// Package reviews manages vendor order reviews, attachments, and merchant replies.
package reviews

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"portal/internal/platform/crypto"
	platformstorage "portal/internal/platform/storage"
)

// AttachmentInput holds file data for an attachment submitted with a review.
type AttachmentInput struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"` // base64-encoded file content
}

// Attachment is the API-facing attachment record. Server-only fields
// (file_path, checksum) are deliberately omitted from JSON so internal
// storage paths never leak to clients.
type Attachment struct {
	ID           string    `json:"id"`
	ReviewID     string    `json:"review_id"`
	OriginalName string    `json:"original_name"`
	ContentType  string    `json:"content_type"`
	SizeBytes    int64     `json:"size_bytes"`
	UploadedAt   time.Time `json:"uploaded_at"`
}

// AttachmentInternal extends Attachment with server-only fields needed by the
// download endpoint. Must NOT be serialised to JSON in API responses.
type AttachmentInternal struct {
	Attachment
	FilePath string
	Checksum string
}

// MerchantReply holds a merchant's reply to a review.
type MerchantReply struct {
	ID         string    `json:"id"`
	ReviewID   string    `json:"review_id"`
	OrderID    string    `json:"order_id"`
	RecordedBy string    `json:"recorded_by"`
	ReplyText  string    `json:"reply_text"`
	RepliedAt  time.Time `json:"replied_at"`
}

// Review represents a vendor order review.
type Review struct {
	ID             string          `json:"id"`
	OrderID        string          `json:"order_id"`
	ReviewerID     string          `json:"reviewer_id"`
	Rating         int             `json:"rating"`
	ReviewText     string          `json:"review_text,omitempty"`
	Visibility     string          `json:"visibility"`
	DisclaimerText *string         `json:"disclaimer_text,omitempty"`
	Attachments    []Attachment    `json:"attachments"`
	Reply          *MerchantReply  `json:"reply,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Store manages review persistence.
type Store struct {
	pool      *pgxpool.Pool
	storage   *platformstorage.Store
	encryptor *crypto.Encryptor
}

// NewStore constructs a Store backed by the given connection pool and file storage.
func NewStore(pool *pgxpool.Pool, storage *platformstorage.Store) *Store {
	return &Store{pool: pool, storage: storage}
}

// NewStoreWithEncryptor constructs a Store with AES encryption for sensitive fields.
func NewStoreWithEncryptor(pool *pgxpool.Pool, storage *platformstorage.Store, encryptor *crypto.Encryptor) *Store {
	return &Store{pool: pool, storage: storage, encryptor: encryptor}
}

// CreateReview inserts a new review and its attachments.
func (s *Store) CreateReview(
	ctx context.Context,
	orderID, authorID string,
	rating int,
	body string,
	attachments []AttachmentInput,
) (*Review, error) {
	if rating < 1 || rating > 5 {
		return nil, fmt.Errorf("rating must be between 1 and 5")
	}

	id := uuid.New().String()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO reviews (id, order_id, reviewer_id, rating, review_text, visibility)
		VALUES ($1, $2, $3, $4, $5, 'visible')`,
		id, orderID, authorID, rating, body,
	)
	if err != nil {
		return nil, fmt.Errorf("create review: %w", err)
	}

	for _, a := range attachments {
		sf, err := s.storage.Save("attachments", a.Filename, a.ContentType, a.Data,
			[]string{"image/jpeg", "image/png"})
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", a.Filename, err)
		}
		aID := uuid.New().String()
		_, err = s.pool.Exec(ctx, `
			INSERT INTO review_attachments
			  (id, review_id, file_path, original_name, content_type, size_bytes, checksum, uploaded_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			aID, id,
			sf.Path,
			a.Filename, a.ContentType, sf.SizeBytes,
			sf.Checksum,
			authorID,
		)
		if err != nil {
			_ = s.storage.Delete(sf.Path) // remove orphaned file if DB insert fails
			return nil, fmt.Errorf("create attachment: %w", err)
		}
	}

	return s.GetReview(ctx, id)
}

// GetReview fetches a single review by ID, including attachments and reply.
func (s *Store) GetReview(ctx context.Context, reviewID string) (*Review, error) {
	r := &Review{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, order_id, reviewer_id, rating,
		       coalesce(review_text, ''), visibility, disclaimer_text,
		       created_at, updated_at
		FROM reviews
		WHERE id = $1`, reviewID).
		Scan(&r.ID, &r.OrderID, &r.ReviewerID, &r.Rating,
			&r.ReviewText, &r.Visibility, &r.DisclaimerText,
			&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get review: %w", err)
	}

	r.Attachments = s.getAttachments(ctx, reviewID)
	r.Reply = s.getReply(ctx, reviewID)
	return r, nil
}

// ListReviews returns paginated reviews for a vendor order.
// When showHidden is false, reviews with visibility='hidden' are excluded from
// both the total count and the returned rows.
func (s *Store) ListReviews(
	ctx context.Context,
	orderID string,
	limit, offset int,
	showHidden bool,
) ([]Review, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	hiddenClause := ""
	if !showHidden {
		hiddenClause = " AND visibility != 'hidden'"
	}

	var total int
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM reviews WHERE order_id = $1`+hiddenClause, orderID,
	).Scan(&total)

	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, reviewer_id, rating,
		       coalesce(review_text, ''), visibility, disclaimer_text,
		       created_at, updated_at
		FROM reviews
		WHERE order_id = $1`+hiddenClause+`
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`,
		orderID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list reviews: %w", err)
	}
	defer rows.Close()

	var reviews []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.OrderID, &r.ReviewerID, &r.Rating,
			&r.ReviewText, &r.Visibility, &r.DisclaimerText,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		r.Attachments = s.getAttachments(ctx, r.ID)
		r.Reply = s.getReply(ctx, r.ID)
		reviews = append(reviews, r)
	}
	if reviews == nil {
		reviews = []Review{}
	}
	return reviews, total, nil
}

// AddMerchantReply records a merchant reply, failing if one already exists.
func (s *Store) AddMerchantReply(
	ctx context.Context,
	reviewID, merchantUserID, replyText string,
) error {
	// Check for existing reply
	var count int
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM merchant_replies WHERE review_id = $1`, reviewID,
	).Scan(&count)
	if count > 0 {
		return fmt.Errorf("reply already exists")
	}

	// Fetch order_id for the review
	var orderID string
	if err := s.pool.QueryRow(ctx,
		`SELECT order_id FROM reviews WHERE id = $1`, reviewID,
	).Scan(&orderID); err != nil {
		return fmt.Errorf("review not found: %w", err)
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO merchant_replies (id, review_id, order_id, recorded_by, reply_text)
		VALUES ($1, $2, $3, $4, $5)`,
		uuid.New().String(), reviewID, orderID, merchantUserID, replyText,
	)
	return err
}

// FlagForModeration inserts the review into the moderation_queue.
func (s *Store) FlagForModeration(
	ctx context.Context,
	reviewID, reason, flaggedByUserID string,
) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO moderation_queue (id, review_id, reason, flagged_by, status)
		VALUES ($1, $2, $3, $4, 'pending')`,
		uuid.New().String(), reviewID, reason, flaggedByUserID,
	)
	return err
}

// ReviewExists reports whether a review row exists with the given id. Used by
// the flag endpoint to return 404 for bogus ids instead of bubbling a raw
// foreign-key failure up to the client as a 500.
func (s *Store) ReviewExists(ctx context.Context, reviewID string) (bool, error) {
	if reviewID == "" {
		return false, nil
	}
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM reviews WHERE id = $1::uuid)`,
		reviewID,
	).Scan(&exists)
	if err != nil {
		// Invalid uuid syntax → treat as "does not exist" so callers can 404.
		return false, nil
	}
	return exists, nil
}

// getAttachments returns all attachments for a review (API-safe: no file paths).
func (s *Store) getAttachments(ctx context.Context, reviewID string) []Attachment {
	rows, _ := s.pool.Query(ctx, `
		SELECT id, review_id, file_path, original_name, content_type, size_bytes, checksum, uploaded_at
		FROM review_attachments
		WHERE review_id = $1
		ORDER BY uploaded_at ASC`, reviewID)
	if rows == nil {
		return []Attachment{}
	}
	defer rows.Close()

	var out []Attachment
	for rows.Next() {
		var ai AttachmentInternal
		_ = rows.Scan(&ai.ID, &ai.ReviewID, &ai.FilePath, &ai.OriginalName,
			&ai.ContentType, &ai.SizeBytes, &ai.Checksum, &ai.UploadedAt)
		out = append(out, ai.Attachment) // strip internal fields
	}
	if out == nil {
		return []Attachment{}
	}
	return out
}

// GetAttachmentInternal fetches a single attachment record by ID with
// server-only fields (file_path, checksum). Used by the download endpoint.
func (s *Store) GetAttachmentInternal(ctx context.Context, attachmentID string) (*AttachmentInternal, error) {
	ai := &AttachmentInternal{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, review_id, file_path, original_name, content_type, size_bytes, checksum, uploaded_at
		FROM review_attachments
		WHERE id = $1`, attachmentID).
		Scan(&ai.ID, &ai.ReviewID, &ai.FilePath, &ai.OriginalName,
			&ai.ContentType, &ai.SizeBytes, &ai.Checksum, &ai.UploadedAt)
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return ai, nil
}

// getReply fetches the merchant reply for a review, if any.
func (s *Store) getReply(ctx context.Context, reviewID string) *MerchantReply {
	r := &MerchantReply{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, review_id, order_id, recorded_by, reply_text, replied_at
		FROM merchant_replies
		WHERE review_id = $1
		LIMIT 1`, reviewID).
		Scan(&r.ID, &r.ReviewID, &r.OrderID, &r.RecordedBy, &r.ReplyText, &r.RepliedAt)
	if err != nil {
		return nil
	}
	return r
}
