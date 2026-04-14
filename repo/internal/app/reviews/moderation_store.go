package reviews

import (
	"context"
	"fmt"
	"time"
)

// ModerationItem represents an entry in the moderation queue.
type ModerationItem struct {
	ID            string     `json:"id"`
	ReviewID      string     `json:"review_id"`
	Reason        string     `json:"reason"`
	FlaggedBy     string     `json:"flagged_by"`
	Status        string     `json:"status"`
	ModeratorID   *string    `json:"moderator_id,omitempty"`
	DecisionNotes *string    `json:"decision_notes,omitempty"`
	FlaggedAt     time.Time  `json:"flagged_at"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
}

// ListQueue returns paginated moderation queue items filtered by status.
func (s *Store) ListQueue(
	ctx context.Context,
	status string,
	limit, offset int,
) ([]ModerationItem, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []any{}
	argN := 1
	where := "WHERE 1=1"

	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", argN)
		args = append(args, status)
		argN++
	}

	countArgs := make([]any, len(args))
	copy(countArgs, args)

	var total int
	_ = s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM moderation_queue "+where,
		countArgs...,
	).Scan(&total)

	query := fmt.Sprintf(`
		SELECT id, review_id, reason, flagged_by, status,
		       moderator_id, decision_notes, flagged_at, decided_at
		FROM moderation_queue
		%s ORDER BY flagged_at DESC LIMIT $%d OFFSET $%d`,
		where, argN, argN+1,
	)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list queue: %w", err)
	}
	defer rows.Close()

	var items []ModerationItem
	for rows.Next() {
		var item ModerationItem
		if err := rows.Scan(
			&item.ID, &item.ReviewID, &item.Reason, &item.FlaggedBy, &item.Status,
			&item.ModeratorID, &item.DecisionNotes, &item.FlaggedAt, &item.DecidedAt,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if items == nil {
		items = []ModerationItem{}
	}
	return items, total, nil
}

// DecideItem records a moderator decision on a queue item.
func (s *Store) DecideItem(
	ctx context.Context,
	itemID, moderatorID, decision, notes string,
) error {
	validDecisions := map[string]bool{
		"approve":  true,
		"reject":   true,
		"escalate": true,
	}
	if !validDecisions[decision] {
		return fmt.Errorf("invalid decision: must be approve, reject, or escalate")
	}

	result, err := s.pool.Exec(ctx, `
		UPDATE moderation_queue
		SET status = $1, moderator_id = $2, decision_notes = $3, decided_at = NOW()
		WHERE id = $4`,
		decision, moderatorID, notes, itemID,
	)
	if err != nil {
		return fmt.Errorf("decide item: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("moderation item not found")
	}
	return nil
}
