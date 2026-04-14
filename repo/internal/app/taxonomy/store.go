package taxonomy

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Tag struct {
	ID            int64     `json:"id"`
	Code          string    `json:"code"`
	CanonicalName string    `json:"canonical_name"`
	ParentID      *int64    `json:"parent_id,omitempty"`
	IsActive      bool      `json:"is_active"`
	Synonyms      []Synonym `json:"synonyms,omitempty"`
}

type Synonym struct {
	ID       int64  `json:"id"`
	Text     string `json:"text"`
	Type     string `json:"type"` // alias, pinyin, abbreviation
	IsActive bool   `json:"is_active"`
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, canonical_name, parent_id, is_active
		FROM skill_tags ORDER BY canonical_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []Tag
	for rows.Next() {
		var t Tag
		_ = rows.Scan(&t.ID, &t.Code, &t.CanonicalName, &t.ParentID, &t.IsActive)
		tags = append(tags, t)
	}
	return tags, nil
}

func (s *Store) GetTag(ctx context.Context, id int64) (*Tag, error) {
	var t Tag
	err := s.pool.QueryRow(ctx, `
		SELECT id, code, canonical_name, parent_id, is_active FROM skill_tags WHERE id = $1`, id).
		Scan(&t.ID, &t.Code, &t.CanonicalName, &t.ParentID, &t.IsActive)
	if err != nil {
		return nil, fmt.Errorf("tag not found")
	}
	t.Synonyms = s.getSynonyms(ctx, t.ID)
	return &t, nil
}

// AddSynonym adds a synonym and checks for conflicts.
// Returns a conflict error if the synonym_text is already an active synonym pointing to a different canonical tag.
func (s *Store) AddSynonym(ctx context.Context, tagID int64, text, synonymType string, createdBy string) error {
	// Conflict detection: does this synonym_text already point to a DIFFERENT active tag?
	var existingTagID int64
	err := s.pool.QueryRow(ctx, `
		SELECT ts.tag_id FROM tag_synonyms ts
		WHERE ts.synonym_text = $1 AND ts.is_active = TRUE AND ts.tag_id <> $2
		LIMIT 1`, text, tagID).Scan(&existingTagID)
	if err == nil {
		// Conflict found — insert into tag_conflicts queue and return error
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO tag_conflicts (synonym_text, tag_id_a, tag_id_b)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, text, tagID, existingTagID)
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO taxonomy_review_queue (conflict_id, status)
			SELECT id, 'pending' FROM tag_conflicts
			WHERE synonym_text = $1 AND tag_id_a = $2 AND tag_id_b = $3
			LIMIT 1`, text, tagID, existingTagID)
		return fmt.Errorf("conflict: synonym %q already points to a different active tag (id=%d)", text, existingTagID)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO tag_synonyms (tag_id, synonym_text, synonym_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (synonym_text, synonym_type) DO NOTHING`, tagID, text, synonymType)
	return err
}

// ResolveConflict closes an open tag_conflicts row by recording the resolver,
// timestamp, and chosen resolution. Side effects on tag_synonyms:
//
//   - "deactivated_a": set is_active=FALSE on the synonym pointing at tag_id_a
//   - "deactivated_b": set is_active=FALSE on the synonym pointing at tag_id_b
//   - "merged":        no synonym change here (merge is handled separately by
//                      a Tag merge operation; this just closes the queue item)
//
// Idempotent: re-resolving an already-resolved conflict returns an error.
func (s *Store) ResolveConflict(ctx context.Context, conflictID int64, resolverID, resolution string) error {
	switch resolution {
	case "deactivated_a", "deactivated_b", "merged":
	default:
		return fmt.Errorf("invalid resolution %q (expected deactivated_a, deactivated_b, or merged)", resolution)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the conflict row and confirm it's still open.
	var (
		synonymText string
		tagA, tagB  int64
		resolvedAt  *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT synonym_text, tag_id_a, tag_id_b, resolved_at
		FROM tag_conflicts WHERE id = $1
		FOR UPDATE`, conflictID).Scan(&synonymText, &tagA, &tagB, &resolvedAt)
	if err != nil {
		return fmt.Errorf("conflict not found: %w", err)
	}
	if resolvedAt != nil {
		return fmt.Errorf("conflict %d is already resolved", conflictID)
	}

	// Apply the side effect on tag_synonyms before closing the conflict.
	switch resolution {
	case "deactivated_a":
		if _, err := tx.Exec(ctx, `
			UPDATE tag_synonyms SET is_active = FALSE
			WHERE synonym_text = $1 AND tag_id = $2`, synonymText, tagA); err != nil {
			return fmt.Errorf("deactivate side a: %w", err)
		}
	case "deactivated_b":
		if _, err := tx.Exec(ctx, `
			UPDATE tag_synonyms SET is_active = FALSE
			WHERE synonym_text = $1 AND tag_id = $2`, synonymText, tagB); err != nil {
			return fmt.Errorf("deactivate side b: %w", err)
		}
	}

	// Close the conflict.
	if _, err := tx.Exec(ctx, `
		UPDATE tag_conflicts
		SET resolved_at = NOW(),
		    resolved_by = NULLIF($2,'')::uuid,
		    resolution  = $3
		WHERE id = $1`, conflictID, resolverID, resolution); err != nil {
		return fmt.Errorf("close conflict: %w", err)
	}

	// Mark the matching review queue entry as reviewed.
	if _, err := tx.Exec(ctx, `
		UPDATE taxonomy_review_queue
		SET status = 'reviewed', updated_at = NOW()
		WHERE conflict_id = $1`, conflictID); err != nil {
		return fmt.Errorf("close review queue: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *Store) ListConflicts(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tc.id, tc.synonym_text, tc.tag_id_a, tc.tag_id_b, tc.detected_at, tc.resolved_at
		FROM tag_conflicts tc
		WHERE tc.resolved_at IS NULL
		ORDER BY tc.detected_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]any
	for rows.Next() {
		var id int64
		var text string
		var tagA, tagB int64
		var detected *string
		var resolved *string
		_ = rows.Scan(&id, &text, &tagA, &tagB, &detected, &resolved)
		results = append(results, map[string]any{
			"id":          id,
			"synonym_text": text,
			"tag_id_a":    tagA,
			"tag_id_b":    tagB,
			"detected_at": detected,
		})
	}
	return results, nil
}

func (s *Store) getSynonyms(ctx context.Context, tagID int64) []Synonym {
	rows, _ := s.pool.Query(ctx, `
		SELECT id, synonym_text, synonym_type, is_active
		FROM tag_synonyms WHERE tag_id = $1`, tagID)
	if rows == nil {
		return nil
	}
	defer rows.Close()
	var syns []Synonym
	for rows.Next() {
		var syn Synonym
		_ = rows.Scan(&syn.ID, &syn.Text, &syn.Type, &syn.IsActive)
		syns = append(syns, syn)
	}
	return syns
}
