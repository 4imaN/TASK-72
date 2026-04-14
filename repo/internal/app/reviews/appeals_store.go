package reviews

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EvidenceInput holds file data for appeal evidence.
type EvidenceInput struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"` // base64-encoded file content
}

// Evidence is the API-facing evidence record. Internal fields (file_path,
// checksum) are never serialised to JSON — clients see only the safe
// identifiers and the display-friendly metadata decrypted from the
// encrypted_metadata column (or the plaintext fallback when the
// encryptor is nil).
type Evidence struct {
	ID           string    `json:"id"`
	AppealID     string    `json:"appeal_id"`
	OriginalName string    `json:"original_name"`
	ContentType  string    `json:"content_type"`
	SizeBytes    int64     `json:"size_bytes"`
	UploadedAt   time.Time `json:"uploaded_at"`
}

// evidenceInternal holds the full DB row including server-only fields that
// the download endpoint needs but the API must not expose.
type evidenceInternal struct {
	Evidence
	FilePath         string
	Checksum         string
	EncryptedMeta    string // raw encrypted_metadata column value
}

// Appeal represents a negative review appeal.
type Appeal struct {
	ID             string     `json:"id"`
	ReviewID       string     `json:"review_id"`
	AppealedBy     string     `json:"appealed_by"`
	AppealReason   string     `json:"appeal_reason"`
	Status         string     `json:"status"`
	SubmittedAt    time.Time  `json:"submitted_at"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
	DecidedBy      *string    `json:"decided_by,omitempty"`
	Evidence       []Evidence `json:"evidence"`
}

// AppealFilter holds filter criteria for listing appeals.
type AppealFilter struct {
	Status       string
	VendorOrderID string
	AppellantID  string // for own-only filtering
}

// CreateAppeal creates a new appeal for a low-rated review.
func (s *Store) CreateAppeal(
	ctx context.Context,
	reviewID, appellantUserID, reason string,
	evidence []EvidenceInput,
) (*Appeal, error) {
	// Fetch the review rating to confirm ≤ 2
	var rating int
	if err := s.pool.QueryRow(ctx,
		`SELECT rating FROM reviews WHERE id = $1`, reviewID,
	).Scan(&rating); err != nil {
		return nil, fmt.Errorf("review not found: %w", err)
	}
	if rating > 2 {
		return nil, fmt.Errorf("can only appeal reviews with rating 1 or 2")
	}

	id := uuid.New().String()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO negative_review_appeals
		  (id, review_id, appealed_by, appeal_reason, status)
		VALUES ($1, $2, $3, $4, 'pending')`,
		id, reviewID, appellantUserID, reason,
	)
	if err != nil {
		return nil, fmt.Errorf("create appeal: %w", err)
	}

	for _, e := range evidence {
		sf, err := s.storage.Save("evidence", e.Filename, e.ContentType, e.Data,
			[]string{"application/pdf", "image/jpeg", "image/png"})
		if err != nil {
			return nil, fmt.Errorf("evidence %s: %w", e.Filename, err)
		}

		// Encrypt sensitive file metadata before persisting. When encryption
		// is available the plaintext metadata columns (original_name,
		// content_type, size_bytes) are NULLed so the ciphertext in
		// encrypted_metadata is the sole source of truth at rest. The
		// download and API-read paths decrypt from encrypted_metadata via
		// toSafeEvidence. When encryption is NOT available (dev/test) the
		// plaintext columns serve as the fallback.
		var encMeta string
		var ptOrigName, ptContentType *string
		var ptSizeBytes *int64
		if s.encryptor != nil {
			metaObj := map[string]any{
				"filename":     e.Filename,
				"content_type": e.ContentType,
				"size_bytes":   sf.SizeBytes,
			}
			metaJSON, jsonErr := json.Marshal(metaObj)
			if jsonErr == nil {
				encMeta, _ = s.encryptor.Encrypt(string(metaJSON))
			}
			// Plaintext columns stay NULL when we have the ciphertext.
		} else {
			// No encryptor — fall back to plaintext columns.
			fn := e.Filename
			ct := e.ContentType
			sb := sf.SizeBytes
			ptOrigName = &fn
			ptContentType = &ct
			ptSizeBytes = &sb
		}

		eID := uuid.New().String()
		_, err = s.pool.Exec(ctx, `
			INSERT INTO appeal_evidence
			  (id, appeal_id, file_path, original_name, content_type, size_bytes, checksum, uploaded_by, encrypted_metadata)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9,''))`,
			eID, id,
			sf.Path,
			ptOrigName, ptContentType, ptSizeBytes,
			sf.Checksum,
			appellantUserID,
			encMeta,
		)
		if err != nil {
			_ = s.storage.Delete(sf.Path) // remove orphaned file if DB insert fails
			return nil, fmt.Errorf("create evidence: %w", err)
		}
	}

	return s.GetAppeal(ctx, id)
}

// GetAppeal fetches a single appeal by ID, including evidence.
func (s *Store) GetAppeal(ctx context.Context, appealID string) (*Appeal, error) {
	a := &Appeal{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, review_id, appealed_by, appeal_reason, status,
		       submitted_at, decided_at, decided_by
		FROM negative_review_appeals
		WHERE id = $1`, appealID).
		Scan(&a.ID, &a.ReviewID, &a.AppealedBy, &a.AppealReason, &a.Status,
			&a.SubmittedAt, &a.DecidedAt, &a.DecidedBy)
	if err != nil {
		return nil, fmt.Errorf("get appeal: %w", err)
	}
	a.Evidence = s.getEvidence(ctx, appealID)
	return a, nil
}

// ListAppeals returns paginated appeals with optional filters.
func (s *Store) ListAppeals(
	ctx context.Context,
	filter AppealFilter,
	limit, offset int,
) ([]Appeal, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []any{}
	argN := 1
	where := "WHERE 1=1"

	if filter.Status != "" {
		where += fmt.Sprintf(" AND a.status = $%d", argN)
		args = append(args, filter.Status)
		argN++
	}
	if filter.VendorOrderID != "" {
		where += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM reviews r WHERE r.id = a.review_id AND r.order_id = $%d)`, argN)
		args = append(args, filter.VendorOrderID)
		argN++
	}
	if filter.AppellantID != "" {
		where += fmt.Sprintf(" AND a.appealed_by = $%d", argN)
		args = append(args, filter.AppellantID)
		argN++
	}

	countArgs := make([]any, len(args))
	copy(countArgs, args)

	var total int
	_ = s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM negative_review_appeals a "+where,
		countArgs...,
	).Scan(&total)

	query := fmt.Sprintf(`
		SELECT a.id, a.review_id, a.appealed_by, a.appeal_reason, a.status,
		       a.submitted_at, a.decided_at, a.decided_by
		FROM negative_review_appeals a
		%s ORDER BY a.submitted_at DESC LIMIT $%d OFFSET $%d`,
		where, argN, argN+1,
	)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list appeals: %w", err)
	}
	defer rows.Close()

	var appeals []Appeal
	for rows.Next() {
		var a Appeal
		if err := rows.Scan(&a.ID, &a.ReviewID, &a.AppealedBy, &a.AppealReason, &a.Status,
			&a.SubmittedAt, &a.DecidedAt, &a.DecidedBy); err != nil {
			return nil, 0, err
		}
		a.Evidence = s.getEvidence(ctx, a.ID)
		appeals = append(appeals, a)
	}
	if appeals == nil {
		appeals = []Appeal{}
	}
	return appeals, total, nil
}

// RecordArbitration records an arbitration decision and updates review visibility atomically.
func (s *Store) RecordArbitration(
	ctx context.Context,
	appealID, arbitratorID, outcome, notes, disclaimerText string,
) error {
	validOutcomes := map[string]bool{
		"hide":                 true,
		"show_with_disclaimer": true,
		"restore":              true,
	}
	if !validOutcomes[outcome] {
		return fmt.Errorf("invalid outcome: must be hide, show_with_disclaimer, or restore")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Fetch the review ID from the appeal
	var reviewID string
	if err := tx.QueryRow(ctx,
		`SELECT review_id FROM negative_review_appeals WHERE id = $1`, appealID,
	).Scan(&reviewID); err != nil {
		return fmt.Errorf("appeal not found: %w", err)
	}

	// Update appeal status to decided
	_, err = tx.Exec(ctx, `
		UPDATE negative_review_appeals
		SET status = 'decided', decided_at = NOW(), decided_by = $1
		WHERE id = $2`,
		arbitratorID, appealID,
	)
	if err != nil {
		return fmt.Errorf("update appeal: %w", err)
	}

	// Insert arbitration outcome
	_, err = tx.Exec(ctx, `
		INSERT INTO arbitration_outcomes (id, appeal_id, decided_by, outcome, rationale)
		VALUES ($1, $2, $3, $4, $5)`,
		uuid.New().String(), appealID, arbitratorID, outcome, notes,
	)
	if err != nil {
		return fmt.Errorf("insert arbitration: %w", err)
	}

	// Update review visibility based on outcome
	switch outcome {
	case "hide":
		_, err = tx.Exec(ctx, `
			UPDATE reviews SET visibility = 'hidden', updated_at = NOW()
			WHERE id = $1`, reviewID)
	case "show_with_disclaimer":
		_, err = tx.Exec(ctx, `
			UPDATE reviews SET visibility = 'shown_with_disclaimer',
			                   disclaimer_text = $1, updated_at = NOW()
			WHERE id = $2`, disclaimerText, reviewID)
	case "restore":
		_, err = tx.Exec(ctx, `
			UPDATE reviews SET visibility = 'visible',
			                   disclaimer_text = NULL, updated_at = NOW()
			WHERE id = $1`, reviewID)
	}
	if err != nil {
		return fmt.Errorf("update review visibility: %w", err)
	}

	return tx.Commit(ctx)
}

// getEvidence returns all evidence records for an appeal.
func (s *Store) getEvidence(ctx context.Context, appealID string) []Evidence {
	rows, _ := s.pool.Query(ctx, `
		SELECT id, appeal_id, file_path, original_name, content_type,
		       size_bytes, checksum, uploaded_at, coalesce(encrypted_metadata,'')
		FROM appeal_evidence
		WHERE appeal_id = $1
		ORDER BY uploaded_at ASC`, appealID)
	if rows == nil {
		return []Evidence{}
	}
	defer rows.Close()

	var out []Evidence
	for rows.Next() {
		var ei evidenceInternal
		_ = rows.Scan(&ei.ID, &ei.AppealID, &ei.FilePath, &ei.OriginalName,
			&ei.ContentType, &ei.SizeBytes, &ei.Checksum, &ei.UploadedAt, &ei.EncryptedMeta)
		out = append(out, s.toSafeEvidence(ei))
	}
	if out == nil {
		return []Evidence{}
	}
	return out
}

// GetEvidenceInternal fetches a single evidence record by ID with server-only
// fields (file_path, checksum) included. Used by the download endpoint —
// callers must NOT serialize this to JSON.
func (s *Store) GetEvidenceInternal(ctx context.Context, evidenceID string) (*evidenceInternal, error) {
	ei := &evidenceInternal{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, appeal_id, file_path, original_name, content_type,
		       size_bytes, checksum, uploaded_at, coalesce(encrypted_metadata,'')
		FROM appeal_evidence
		WHERE id = $1`, evidenceID).
		Scan(&ei.ID, &ei.AppealID, &ei.FilePath, &ei.OriginalName,
			&ei.ContentType, &ei.SizeBytes, &ei.Checksum, &ei.UploadedAt, &ei.EncryptedMeta)
	if err != nil {
		return nil, fmt.Errorf("get evidence: %w", err)
	}
	return ei, nil
}

// GetEvidence fetches a single evidence record by ID for API consumption.
// Internal fields are stripped; metadata is decrypted when the encryptor is
// available.
func (s *Store) GetEvidence(ctx context.Context, evidenceID string) (*Evidence, error) {
	ei, err := s.GetEvidenceInternal(ctx, evidenceID)
	if err != nil {
		return nil, err
	}
	safe := s.toSafeEvidence(*ei)
	return &safe, nil
}

// toSafeEvidence converts an internal row to the API-safe Evidence DTO.
// When encrypted_metadata is populated and the encryptor is wired, the
// display-friendly metadata (original_name, content_type, size_bytes) is
// sourced from the ciphertext — the plaintext DB columns are only fallback.
func (s *Store) toSafeEvidence(ei evidenceInternal) Evidence {
	e := ei.Evidence // start from the plaintext fallback

	if s.encryptor != nil && ei.EncryptedMeta != "" {
		if plain, err := s.encryptor.Decrypt(ei.EncryptedMeta); err == nil {
			var meta map[string]any
			if json.Unmarshal([]byte(plain), &meta) == nil {
				if v, ok := meta["filename"].(string); ok && v != "" {
					e.OriginalName = v
				}
				if v, ok := meta["content_type"].(string); ok && v != "" {
					e.ContentType = v
				}
				if v, ok := meta["size_bytes"].(float64); ok {
					e.SizeBytes = int64(v)
				}
			}
		}
	}
	return e
}
