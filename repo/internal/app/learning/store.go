package learning

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- domain types ---

type LearningPath struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	JobFamilyID *int64     `json:"job_family_id,omitempty"`
	IsPublished bool       `json:"is_published"`
	CreatedAt   time.Time  `json:"created_at"`
	Items       []PathItem `json:"items,omitempty"`
	Rules       *PathRules `json:"rules,omitempty"`
}

type PathItem struct {
	ResourceID  string `json:"resource_id"`
	Title       string `json:"title"`
	ContentType string `json:"content_type"`
	ItemType    string `json:"item_type"` // "required" or "elective"
	SortOrder   int    `json:"sort_order"`
}

type PathRules struct {
	RequiredCount   int    `json:"required_count"`
	ElectiveMinimum int    `json:"elective_minimum"`
	Description     string `json:"completion_description,omitempty"`
}

type Enrollment struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	PathID      string     `json:"path_id"`
	EnrolledAt  time.Time  `json:"enrolled_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Status      string     `json:"status"` // active, completed, withdrawn
}

type ProgressSnapshot struct {
	UserID       string     `json:"user_id"`
	ResourceID   string     `json:"resource_id"`
	Status       string     `json:"status"` // not_started, in_progress, completed
	ProgressPct  float64    `json:"progress_pct"`
	LastPosition int        `json:"last_position_seconds"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

type PathProgress struct {
	Path            LearningPath   `json:"path"`
	Enrollment      Enrollment     `json:"enrollment"`
	RequiredItems   []ItemProgress `json:"required_items"`
	ElectiveItems   []ItemProgress `json:"elective_items"`
	CompletionReady bool           `json:"completion_ready"`
	RequiredDone    int            `json:"required_done"`
	ElectiveDone    int            `json:"elective_done"`
}

type ItemProgress struct {
	PathItem
	Progress *ProgressSnapshot `json:"progress,omitempty"`
}

// --- store ---

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ListPaths returns all published learning paths.
func (s *Store) ListPaths(ctx context.Context) ([]LearningPath, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, coalesce(description,''), job_family_id, is_published, created_at
		FROM learning_paths WHERE is_published = TRUE ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []LearningPath
	for rows.Next() {
		var p LearningPath
		if err := rows.Scan(&p.ID, &p.Title, &p.Description, &p.JobFamilyID, &p.IsPublished, &p.CreatedAt); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// GetPath returns a path with items and rules.
func (s *Store) GetPath(ctx context.Context, pathID string) (*LearningPath, error) {
	var p LearningPath
	err := s.pool.QueryRow(ctx, `
		SELECT id, title, coalesce(description,''), job_family_id, is_published, created_at
		FROM learning_paths WHERE id = $1`, pathID).
		Scan(&p.ID, &p.Title, &p.Description, &p.JobFamilyID, &p.IsPublished, &p.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("path not found")
	}

	// Items
	rows, err := s.pool.Query(ctx, `
		SELECT lpi.resource_id, r.title, r.content_type, lpi.item_type, lpi.sort_order
		FROM learning_path_items lpi
		JOIN resources r ON r.id = lpi.resource_id
		WHERE lpi.path_id = $1
		ORDER BY lpi.item_type DESC, lpi.sort_order`, pathID) // required first
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var item PathItem
			_ = rows.Scan(&item.ResourceID, &item.Title, &item.ContentType, &item.ItemType, &item.SortOrder)
			p.Items = append(p.Items, item)
		}
	}

	// Rules
	var rules PathRules
	err2 := s.pool.QueryRow(ctx, `
		SELECT required_count, elective_minimum, coalesce(completion_description,'')
		FROM learning_path_rules WHERE path_id = $1`, pathID).
		Scan(&rules.RequiredCount, &rules.ElectiveMinimum, &rules.Description)
	if err2 == nil {
		p.Rules = &rules
	}

	return &p, nil
}

// Enroll enrolls a user in a path. Idempotent if already enrolled.
func (s *Store) Enroll(ctx context.Context, userID, pathID string) (*Enrollment, error) {
	// Check if already enrolled
	var existing Enrollment
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, path_id, enrolled_at, completed_at, status
		FROM learning_enrollments WHERE user_id = $1 AND path_id = $2`, userID, pathID).
		Scan(&existing.ID, &existing.UserID, &existing.PathID,
			&existing.EnrolledAt, &existing.CompletedAt, &existing.Status)
	if err == nil {
		return &existing, nil // already enrolled
	}

	// New enrollment
	id := uuid.New().String()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO learning_enrollments (id, user_id, path_id, status)
		VALUES ($1, $2, $3, 'active')`, id, userID, pathID)
	if err != nil {
		return nil, fmt.Errorf("enroll: %w", err)
	}
	return &Enrollment{
		ID: id, UserID: userID, PathID: pathID,
		EnrolledAt: time.Now(), Status: "active",
	}, nil
}

// GetPathProgress returns full progress breakdown for a user on a path.
func (s *Store) GetPathProgress(ctx context.Context, userID, pathID string) (*PathProgress, error) {
	path, err := s.GetPath(ctx, pathID)
	if err != nil {
		return nil, err
	}

	var enrollment Enrollment
	err = s.pool.QueryRow(ctx, `
		SELECT id, user_id, path_id, enrolled_at, completed_at, status
		FROM learning_enrollments WHERE user_id = $1 AND path_id = $2`, userID, pathID).
		Scan(&enrollment.ID, &enrollment.UserID, &enrollment.PathID,
			&enrollment.EnrolledAt, &enrollment.CompletedAt, &enrollment.Status)
	if err != nil {
		return nil, fmt.Errorf("not enrolled")
	}

	pp := &PathProgress{Path: *path, Enrollment: enrollment}

	for _, item := range path.Items {
		ip := ItemProgress{PathItem: item}
		snap := s.getSnapshot(ctx, userID, item.ResourceID)
		ip.Progress = snap

		if item.ItemType == "required" {
			pp.RequiredItems = append(pp.RequiredItems, ip)
			if snap != nil && snap.Status == "completed" {
				pp.RequiredDone++
			}
		} else {
			pp.ElectiveItems = append(pp.ElectiveItems, ip)
			if snap != nil && snap.Status == "completed" {
				pp.ElectiveDone++
			}
		}
	}

	// Ensure slices are non-nil for JSON serialization
	if pp.RequiredItems == nil {
		pp.RequiredItems = []ItemProgress{}
	}
	if pp.ElectiveItems == nil {
		pp.ElectiveItems = []ItemProgress{}
	}

	// Check completion rule
	if path.Rules != nil {
		allRequired := pp.RequiredDone >= path.Rules.RequiredCount
		enoughElectives := pp.ElectiveDone >= path.Rules.ElectiveMinimum
		pp.CompletionReady = allRequired && enoughElectives
	}

	return pp, nil
}

// RecordProgress saves a progress event and updates the snapshot.
// The resource must belong to at least one learning path the user is enrolled in.
func (s *Store) RecordProgress(ctx context.Context, userID, resourceID, eventType string, positionSeconds int, progressPct float64, deviceHint string) error {
	// Validate that the resource belongs to at least one path the user is enrolled in.
	var enrolledCount int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM learning_enrollments le
		JOIN learning_path_items lpi ON lpi.path_id = le.path_id
		WHERE le.user_id = $1
		  AND lpi.resource_id = $2
		  AND le.status = 'active'`, userID, resourceID).Scan(&enrolledCount)
	if err != nil || enrolledCount == 0 {
		return fmt.Errorf("resource not in any enrolled path")
	}

	// Insert event
	_, err = s.pool.Exec(ctx, `
		INSERT INTO learning_progress_events (id, user_id, resource_id, event_type, position_seconds, progress_pct, device_hint)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7,''))`,
		uuid.New().String(), userID, resourceID, eventType,
		positionSeconds, progressPct, deviceHint,
	)
	if err != nil {
		return fmt.Errorf("record progress event: %w", err)
	}

	// Determine new status
	status := "in_progress"
	var completedAt *time.Time
	if eventType == "completed" || progressPct >= 100 {
		status = "completed"
		now := time.Now()
		completedAt = &now
	}

	// Upsert snapshot
	if completedAt != nil {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO learning_progress_snapshots (id, user_id, resource_id, status, progress_pct, last_position_seconds, last_active_at, completed_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7)
			ON CONFLICT (user_id, resource_id) DO UPDATE SET
				status = CASE WHEN EXCLUDED.status = 'completed' THEN 'completed' ELSE learning_progress_snapshots.status END,
				progress_pct = GREATEST(learning_progress_snapshots.progress_pct, EXCLUDED.progress_pct),
				last_position_seconds = EXCLUDED.last_position_seconds,
				last_active_at = NOW(),
				completed_at = COALESCE(learning_progress_snapshots.completed_at, EXCLUDED.completed_at)`,
			uuid.New().String(), userID, resourceID, status, progressPct, positionSeconds, *completedAt,
		)
	} else {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO learning_progress_snapshots (id, user_id, resource_id, status, progress_pct, last_position_seconds, last_active_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (user_id, resource_id) DO UPDATE SET
				status = CASE WHEN learning_progress_snapshots.status = 'completed' THEN 'completed' ELSE EXCLUDED.status END,
				progress_pct = GREATEST(learning_progress_snapshots.progress_pct, EXCLUDED.progress_pct),
				last_position_seconds = EXCLUDED.last_position_seconds,
				last_active_at = NOW()`,
			uuid.New().String(), userID, resourceID, status, progressPct, positionSeconds,
		)
	}
	return err
}

// ListEnrollments returns the caller's enrolled paths with per-path progress
// summary. This is the data source the "My Progress" page should bind to —
// enrolled paths only, not all published paths.
func (s *Store) ListEnrollments(ctx context.Context, userID string) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT le.id, le.path_id, lp.title, le.status, le.enrolled_at, le.completed_at,
		       coalesce(
		           (SELECT avg(lps.progress_pct)
		            FROM learning_path_items lpi
		            JOIN learning_progress_snapshots lps
		              ON lps.resource_id = lpi.resource_id AND lps.user_id = le.user_id
		            WHERE lpi.path_id = lp.id),
		       0) AS progress_pct,
		       (SELECT count(*) FROM learning_path_items lpi WHERE lpi.path_id = lp.id) AS item_count
		FROM learning_enrollments le
		JOIN learning_paths lp ON lp.id = le.path_id
		WHERE le.user_id = $1
		ORDER BY le.enrolled_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var enrollID, pathID, title, status string
		var enrolledAt time.Time
		var completedAt *time.Time
		var pct float64
		var itemCount int
		if err := rows.Scan(&enrollID, &pathID, &title, &status,
			&enrolledAt, &completedAt, &pct, &itemCount); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"enrollment_id": enrollID,
			"path_id":       pathID,
			"title":         title,
			"status":        status,
			"enrolled_at":   enrolledAt,
			"completed_at":  completedAt,
			"progress_pct":  pct,
			"item_count":    itemCount,
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

// GetResumeState returns all in-progress snapshots for a user (cross-device resume).
func (s *Store) GetResumeState(ctx context.Context, userID string) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT lps.resource_id, r.title, r.content_type,
		       lps.status, lps.progress_pct, lps.last_position_seconds, lps.last_active_at
		FROM learning_progress_snapshots lps
		JOIN resources r ON r.id = lps.resource_id
		WHERE lps.user_id = $1 AND lps.status = 'in_progress'
		ORDER BY lps.last_active_at DESC NULLS LAST
		LIMIT 20`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]any
	for rows.Next() {
		var rid, title, ct, status string
		var pct float64
		var pos int
		var lat *time.Time
		if err := rows.Scan(&rid, &title, &ct, &status, &pct, &pos, &lat); err != nil {
			continue
		}
		results = append(results, map[string]any{
			"resource_id": rid, "title": title, "content_type": ct,
			"status": status, "progress_pct": pct,
			"last_position_seconds": pos, "last_active_at": lat,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return results, nil
}

// getSnapshot returns the current progress snapshot for a user/resource pair.
func (s *Store) getSnapshot(ctx context.Context, userID, resourceID string) *ProgressSnapshot {
	var snap ProgressSnapshot
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, resource_id, status, progress_pct, last_position_seconds, last_active_at, completed_at
		FROM learning_progress_snapshots WHERE user_id = $1 AND resource_id = $2`,
		userID, resourceID).
		Scan(&snap.UserID, &snap.ResourceID, &snap.Status, &snap.ProgressPct,
			&snap.LastPosition, &snap.LastActiveAt, &snap.CompletedAt)
	if err != nil {
		return nil
	}
	return &snap
}

// GenerateCSV writes a learner-scoped CSV to w. Only the requesting userID is exported.
func (s *Store) GenerateCSV(ctx context.Context, userID string, w io.Writer) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header
	if err := cw.Write([]string{
		"path_id", "path_title", "enrollment_status", "enrolled_at", "completed_at",
		"resource_id", "resource_title", "content_type", "item_type",
		"progress_status", "progress_pct", "resource_completed_at",
	}); err != nil {
		return err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			lp.id, lp.title, le.status, le.enrolled_at, le.completed_at,
			r.id, r.title, r.content_type, lpi.item_type,
			coalesce(lps.status, 'not_started'),
			coalesce(lps.progress_pct, 0),
			lps.completed_at
		FROM learning_enrollments le
		JOIN learning_paths lp ON lp.id = le.path_id
		JOIN learning_path_items lpi ON lpi.path_id = lp.id
		JOIN resources r ON r.id = lpi.resource_id
		LEFT JOIN learning_progress_snapshots lps
			ON lps.user_id = le.user_id AND lps.resource_id = r.id
		WHERE le.user_id = $1
		ORDER BY lp.title, lpi.item_type DESC, lpi.sort_order`, userID)
	if err != nil {
		return fmt.Errorf("generate csv: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var pathID, pathTitle, enrollStatus, resourceID, resourceTitle, contentType, itemType, progressStatus string
		var enrolledAt time.Time
		var enrollCompletedAt, progressCompletedAt *time.Time
		var progressPct float64

		if err := rows.Scan(
			&pathID, &pathTitle, &enrollStatus, &enrolledAt, &enrollCompletedAt,
			&resourceID, &resourceTitle, &contentType, &itemType,
			&progressStatus, &progressPct, &progressCompletedAt,
		); err != nil {
			continue
		}

		row := []string{
			pathID, pathTitle, enrollStatus, enrolledAt.Format(time.RFC3339), nullTime(enrollCompletedAt),
			resourceID, resourceTitle, contentType, itemType,
			progressStatus, fmt.Sprintf("%.1f", progressPct), nullTime(progressCompletedAt),
		}
		_ = cw.Write(row)
	}
	return cw.Error()
}

func nullTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
