// Package webhooks manages LAN webhook endpoints and delivery tracking.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"portal/internal/platform/crypto"
)

// WebhookEndpoint represents a registered webhook receiver.
type WebhookEndpoint struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	IsActive  bool      `json:"is_active"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// WebhookDelivery represents a single delivery attempt record.
type WebhookDelivery struct {
	ID             string     `json:"id"`
	EndpointID     string     `json:"endpoint_id"`
	EventType      string     `json:"event_type"`
	PayloadJSON    string     `json:"payload_json,omitempty"`
	Status         string     `json:"status"` // pending, delivered, failed
	Attempts       int        `json:"attempts"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`
	ResponseStatus *int       `json:"response_status,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// Store provides database operations for webhooks.
type Store struct {
	pool      *pgxpool.Pool
	encryptor *crypto.Encryptor
}

// NewStore constructs a Store.
func NewStore(pool *pgxpool.Pool, encryptor *crypto.Encryptor) *Store {
	return &Store{pool: pool, encryptor: encryptor}
}

// ListEndpoints returns all webhook endpoints.
func (s *Store) ListEndpoints(ctx context.Context) ([]WebhookEndpoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, url, coalesce(events,'{}'), is_active, created_by, created_at
		FROM webhook_endpoints
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", err)
	}
	defer rows.Close()

	var endpoints []WebhookEndpoint
	for rows.Next() {
		var ep WebhookEndpoint
		if err := rows.Scan(&ep.ID, &ep.URL, &ep.Events,
			&ep.IsActive, &ep.CreatedBy, &ep.CreatedAt); err != nil {
			return nil, err
		}
		if ep.Events == nil {
			ep.Events = []string{}
		}
		endpoints = append(endpoints, ep)
	}
	if endpoints == nil {
		endpoints = []WebhookEndpoint{}
	}
	return endpoints, nil
}

// CreateEndpoint creates a new webhook endpoint, storing the secret encrypted.
func (s *Store) CreateEndpoint(ctx context.Context, url string, events []string, createdBy, secret string) (*WebhookEndpoint, error) {
	if events == nil {
		events = []string{}
	}

	secretEnc, err := s.encryptor.Encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt secret: %w", err)
	}

	id := uuid.New().String()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO webhook_endpoints (id, url, events, is_active, created_by, secret_enc)
		VALUES ($1, $2, $3, TRUE, $4, $5)`,
		id, url, events, createdBy, secretEnc)
	if err != nil {
		return nil, fmt.Errorf("create endpoint: %w", err)
	}

	return &WebhookEndpoint{
		ID:        id,
		URL:       url,
		Events:    events,
		IsActive:  true,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	}, nil
}

// Deliver creates pending delivery records for all active endpoints subscribed to eventType.
func (s *Store) Deliver(ctx context.Context, eventType string, payload map[string]any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id FROM webhook_endpoints
		WHERE is_active = TRUE AND $1 = ANY(events)`, eventType)
	if err != nil {
		return fmt.Errorf("find subscribers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var endpointID string
		if err := rows.Scan(&endpointID); err != nil {
			continue
		}
		deliveryID := uuid.New().String()
		_, err = s.pool.Exec(ctx, `
			INSERT INTO webhook_deliveries
			    (id, endpoint_id, event_type, payload_json, status, attempts)
			VALUES ($1, $2, $3, $4, 'pending', 0)`,
			deliveryID, endpointID, eventType, string(payloadBytes))
		if err != nil {
			return fmt.Errorf("create delivery record: %w", err)
		}
	}
	return nil
}

// ProcessPendingDeliveries processes up to 10 pending deliveries synchronously.
func (s *Store) ProcessPendingDeliveries(ctx context.Context, httpClient *http.Client) error {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.endpoint_id, d.event_type, d.payload_json, d.attempts,
		       e.url, e.secret_enc
		FROM webhook_deliveries d
		JOIN webhook_endpoints e ON e.id = d.endpoint_id
		WHERE d.status = 'pending'
		ORDER BY d.created_at ASC
		LIMIT 10`)
	if err != nil {
		return fmt.Errorf("fetch pending deliveries: %w", err)
	}
	defer rows.Close()

	type pendingRow struct {
		id          string
		endpointID  string
		eventType   string
		payloadJSON string
		attempts    int
		url         string
		secretEnc   string
	}

	var pending []pendingRow
	for rows.Next() {
		var p pendingRow
		if err := rows.Scan(&p.id, &p.endpointID, &p.eventType,
			&p.payloadJSON, &p.attempts, &p.url, &p.secretEnc); err != nil {
			continue
		}
		pending = append(pending, p)
	}
	rows.Close()

	for _, p := range pending {
		secret, err := s.encryptor.Decrypt(p.secretEnc)
		if err != nil {
			secret = ""
		}

		// Defense in depth: re-validate the destination at delivery time so
		// rows created via direct SQL or from before this validator existed
		// cannot leak outbound traffic to the public internet.
		if err := validateLANURL(p.url, nil); err != nil {
			s.markAttempt(ctx, p.id, p.attempts+1, 0, false)
			continue
		}

		sig := computeSignature(secret, []byte(p.payloadJSON))

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url,
			bytes.NewBufferString(p.payloadJSON))
		if err != nil {
			s.markAttempt(ctx, p.id, p.attempts+1, 0, false)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", sig)
		req.Header.Set("X-Event-Type", p.eventType)

		resp, err := httpClient.Do(req)
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
			resp.Body.Close()
		}

		success := err == nil && statusCode >= 200 && statusCode < 300
		s.markAttempt(ctx, p.id, p.attempts+1, statusCode, success)
	}

	return nil
}

// markAttempt updates a delivery record after an attempt.
func (s *Store) markAttempt(ctx context.Context, deliveryID string, attempts, statusCode int, success bool) {
	status := "pending"
	if success {
		status = "delivered"
	} else if attempts >= 3 {
		status = "failed"
	}

	var statusCodePtr *int
	if statusCode != 0 {
		statusCodePtr = &statusCode
	}

	_, _ = s.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status          = $2,
		    attempts        = $3,
		    last_attempt_at = NOW(),
		    response_status = $4
		WHERE id = $1`,
		deliveryID, status, attempts, statusCodePtr)
}

// ListDeliveries returns recent webhook deliveries.
func (s *Store) ListDeliveries(ctx context.Context, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, endpoint_id, event_type,
		       coalesce(payload_json::text, '{}'),
		       status, attempts, last_attempt_at, response_status, created_at
		FROM webhook_deliveries
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(&d.ID, &d.EndpointID, &d.EventType,
			&d.PayloadJSON, &d.Status, &d.Attempts,
			&d.LastAttemptAt, &d.ResponseStatus, &d.CreatedAt); err != nil {
			continue
		}
		deliveries = append(deliveries, d)
	}
	if deliveries == nil {
		deliveries = []WebhookDelivery{}
	}
	return deliveries, nil
}

// computeSignature computes HMAC-SHA256 over payload using the secret.
func computeSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// pgx scan helper for nullable int
var _ = pgx.ErrNoRows
