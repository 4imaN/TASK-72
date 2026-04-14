// Package audit provides structured audit event recording.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store records audit events into the audit_logs table.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore constructs a Store backed by the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Event describes a single auditable action.
type Event struct {
	ActorID    string
	Action     string
	Category   string
	TargetType string
	TargetID   string
	OldValue   any
	NewValue   any
	IPAddress  string
	TraceID    string
}

// Record writes an audit event. Errors are suppressed to avoid interrupting
// business logic — audit recording is best-effort.
func (s *Store) Record(ctx context.Context, evt Event) {
	oldJSON, _ := json.Marshal(evt.OldValue)
	newJSON, _ := json.Marshal(evt.NewValue)
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO audit_logs (id, actor_id, action, category, target_type, target_id, old_value, new_value, ip_address, trace_id)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4, NULLIF($5,''), NULLIF($6,''), NULLIF($7::text,'null')::jsonb, NULLIF($8::text,'null')::jsonb, NULLIF($9,''), NULLIF($10,''))`,
		uuid.New().String(), evt.ActorID, evt.Action, evt.Category,
		evt.TargetType, evt.TargetID, string(oldJSON), string(newJSON),
		evt.IPAddress, evt.TraceID,
	)
}

// RecordReveal logs an access_reveal_log entry when an admin reveals masked personal data.
func (s *Store) RecordReveal(ctx context.Context, actorID, targetUserID, fieldName, reason, ipAddress string) {
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO access_reveal_logs (id, actor_id, target_user_id, field_name, reason, ip_address)
		VALUES ($1, $2::uuid, NULLIF($3,'')::uuid, $4, $5, $6)`,
		uuid.New().String(), actorID, targetUserID, fieldName, reason, ipAddress,
	)
}

// Recorder is the minimal interface a write handler needs to record an
// audited mutation. Implemented by *Store, but expressed as an interface so
// handlers can accept nil (no audit wired) or a fake during testing.
type Recorder interface {
	Record(ctx context.Context, evt Event)
}

// RecordRoleChange records an admin user-role mutation into the audit log.
// Captures both the previous and new role sets for after-the-fact review.
func (s *Store) RecordRoleChange(ctx context.Context, actorID, targetUserID string, oldRoles, newRoles []string, ipAddress string) {
	s.Record(ctx, Event{
		ActorID:    actorID,
		Action:     "user.roles.update",
		Category:   "user",
		TargetType: "user",
		TargetID:   targetUserID,
		OldValue:   oldRoles,
		NewValue:   newRoles,
		IPAddress:  ipAddress,
	})
}

// RecordConfigChange records a generic config-center mutation. Used by the
// config Handler via an adapter at the wiring site (cmd/api/main.go).
func (s *Store) RecordConfigChange(ctx context.Context, actorID, action, targetType, targetID string, oldValue, newValue any, ipAddress string) {
	s.Record(ctx, Event{
		ActorID:    actorID,
		Action:     action,
		Category:   "config",
		TargetType: targetType,
		TargetID:   targetID,
		OldValue:   oldValue,
		NewValue:   newValue,
		IPAddress:  ipAddress,
	})
}

// AuditEvent represents a row in the audit_logs table.
type AuditEvent struct {
	ID         string          `json:"id"`
	ActorID    *string         `json:"actor_id,omitempty"`
	Action     string          `json:"action"`
	Category   string          `json:"category"`
	TargetType *string         `json:"target_type,omitempty"`
	TargetID   *string         `json:"target_id,omitempty"`
	OldValue   json.RawMessage `json:"old_value,omitempty"`
	NewValue   json.RawMessage `json:"new_value,omitempty"`
	IPAddress  *string         `json:"ip_address,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ListEvents returns paginated audit events filtered by userID and/or action.
// Returns (events, total, error).
func (s *Store) ListEvents(ctx context.Context, userID, action string, limit, offset int) ([]AuditEvent, int, error) {
	if limit <= 0 {
		limit = 50
	}

	args := []any{}
	argN := 1
	where := "WHERE 1=1"

	if userID != "" {
		where += fmt.Sprintf(" AND actor_id = $%d::uuid", argN)
		args = append(args, userID)
		argN++
	}
	if action != "" {
		where += fmt.Sprintf(" AND action = $%d", argN)
		args = append(args, action)
		argN++
	}

	countArgs := make([]any, len(args))
	copy(countArgs, args)

	var total int
	_ = s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs "+where, countArgs...).Scan(&total)

	query := fmt.Sprintf(`
		SELECT id, actor_id, action, category, target_type, target_id,
		       old_value, new_value, ip_address, created_at
		FROM audit_logs
		%s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		where, argN, argN+1,
	)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var oldVal, newVal []byte
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.Category,
			&e.TargetType, &e.TargetID, &oldVal, &newVal,
			&e.IPAddress, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		if len(oldVal) > 0 {
			e.OldValue = json.RawMessage(oldVal)
		}
		if len(newVal) > 0 {
			e.NewValue = json.RawMessage(newVal)
		}
		events = append(events, e)
	}
	if events == nil {
		events = []AuditEvent{}
	}
	return events, total, nil
}
