// Package exports manages export jobs — creation, status tracking, and processing.
package exports

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExportJob represents an export job row in export_jobs. Server-internal
// fields (FilePath, ParamsJSON) are excluded from JSON so internal storage
// paths and raw request parameters never leak to API consumers.
type ExportJob struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	FilePath    string     `json:"-"`                      // server-only; download handler uses it
	ErrorMsg    string     `json:"error_msg,omitempty"`
	ParamsJSON  string     `json:"-"`                      // server-only; internal to ProcessJob
	Downloadable bool     `json:"downloadable,omitempty"` // true when file is ready for download
}

// Store provides database operations for export jobs.
type Store struct {
	pool     *pgxpool.Pool
	webhooks WebhookEmitter // optional; fan-out on job completion when set
}

// NewStore constructs a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// WebhookEmitter is the minimal interface a Store needs to fan out
// completion events to LAN webhook subscribers. Implemented by
// internal/app/webhooks.Store.Deliver — abstracted to avoid an import cycle.
type WebhookEmitter interface {
	Deliver(ctx context.Context, eventType string, payload map[string]any) error
}

// WithWebhooks returns a copy of s with the webhook emitter wired so job
// completion events get fanned out to active subscribers. Pass nil to
// disable webhook fan-out (default).
func (s *Store) WithWebhooks(emitter WebhookEmitter) *Store {
	cp := *s
	cp.webhooks = emitter
	return &cp
}

// CreateJob inserts a new export job with status='queued'.
func (s *Store) CreateJob(ctx context.Context, jobType, createdBy string, params map[string]any) (*ExportJob, error) {
	id := uuid.New().String()
	paramsBytes, _ := json.Marshal(params)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO export_jobs (id, export_type, status, requested_by, job_type, created_by_s9, params_json, schema_version)
		VALUES ($1, $2, 'queued', $3, $2, $4, $5, '1')`,
		id, jobType, createdBy, createdBy, string(paramsBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("create export job: %w", err)
	}

	return &ExportJob{
		ID:         id,
		Type:       jobType,
		Status:     "queued",
		CreatedBy:  createdBy,
		CreatedAt:  time.Now(),
		ParamsJSON: string(paramsBytes),
	}, nil
}

// GetJob returns a single export job by ID.
func (s *Store) GetJob(ctx context.Context, jobID string) (*ExportJob, error) {
	var job ExportJob
	var filePath, errorMsg, paramsJSON *string

	err := s.pool.QueryRow(ctx, `
		SELECT id,
		       coalesce(job_type, export_type),
		       status,
		       coalesce(created_by_s9, requested_by::text),
		       requested_at,
		       completed_at,
		       file_path,
		       error_msg,
		       coalesce(params_json::text, '{}')
		FROM export_jobs WHERE id = $1`, jobID).
		Scan(&job.ID, &job.Type, &job.Status, &job.CreatedBy,
			&job.CreatedAt, &job.CompletedAt,
			&filePath, &errorMsg, &paramsJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("job not found")
		}
		return nil, fmt.Errorf("get export job: %w", err)
	}

	if filePath != nil {
		job.FilePath = *filePath
	}
	if errorMsg != nil {
		job.ErrorMsg = *errorMsg
	}
	if paramsJSON != nil {
		job.ParamsJSON = *paramsJSON
	}
	job.Downloadable = job.Status == "completed" && job.FilePath != ""
	return &job, nil
}

// ListJobs returns the most recent jobs. If createdBy is non-empty it scopes to that user.
func (s *Store) ListJobs(ctx context.Context, createdBy string, limit int) ([]ExportJob, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows pgx.Rows
	var err error
	if createdBy != "" {
		rows, err = s.pool.Query(ctx, `
			SELECT id,
			       coalesce(job_type, export_type),
			       status,
			       coalesce(created_by_s9, requested_by::text),
			       requested_at,
			       completed_at,
			       file_path,
			       error_msg,
			       coalesce(params_json::text, '{}')
			FROM export_jobs
			WHERE coalesce(created_by_s9, requested_by::text) = $1
			ORDER BY requested_at DESC LIMIT $2`, createdBy, limit)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id,
			       coalesce(job_type, export_type),
			       status,
			       coalesce(created_by_s9, requested_by::text),
			       requested_at,
			       completed_at,
			       file_path,
			       error_msg,
			       coalesce(params_json::text, '{}')
			FROM export_jobs
			ORDER BY requested_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list export jobs: %w", err)
	}
	defer rows.Close()

	var jobs []ExportJob
	for rows.Next() {
		var job ExportJob
		var filePath, errorMsg, paramsJSON *string
		if err := rows.Scan(&job.ID, &job.Type, &job.Status, &job.CreatedBy,
			&job.CreatedAt, &job.CompletedAt,
			&filePath, &errorMsg, &paramsJSON); err != nil {
			continue
		}
		if filePath != nil {
			job.FilePath = *filePath
		}
		if errorMsg != nil {
			job.ErrorMsg = *errorMsg
		}
		if paramsJSON != nil {
			job.ParamsJSON = *paramsJSON
		}
		jobs = append(jobs, job)
	}
	if jobs == nil {
		jobs = []ExportJob{}
	}
	return jobs, nil
}

// UpdateJobStatus updates status, file_path, and error_msg for a job.
// On a terminal status (completed / failed) it also enqueues a webhook
// delivery (export.completed or export.failed) when an emitter is wired.
func (s *Store) UpdateJobStatus(ctx context.Context, jobID, status, filePath, errMsg string) error {
	var completedAt *time.Time
	if status == "completed" || status == "failed" {
		now := time.Now()
		completedAt = &now
	}

	_, err := s.pool.Exec(ctx, `
		UPDATE export_jobs
		SET status = $2,
		    file_path = NULLIF($3, ''),
		    error_msg = NULLIF($4, ''),
		    completed_at = $5
		WHERE id = $1`,
		jobID, status, filePath, errMsg, completedAt)
	if err != nil {
		return err
	}

	// Webhook fan-out (best-effort — failure to enqueue does not roll back
	// the status update, but is logged via the returned error chain).
	if s.webhooks != nil && (status == "completed" || status == "failed") {
		eventType := "export.completed"
		if status == "failed" {
			eventType = "export.failed"
		}
		payload := map[string]any{
			"job_id":    jobID,
			"status":    status,
			"file_path": filePath,
			"error":     errMsg,
		}
		_ = s.webhooks.Deliver(ctx, eventType, payload)
	}
	return nil
}

// ProcessJob runs a job synchronously: generates the export file and updates status.
func (s *Store) ProcessJob(ctx context.Context, jobID string, pool *pgxpool.Pool) error {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	// Mark running
	if err := s.UpdateJobStatus(ctx, jobID, "running", "", ""); err != nil {
		return err
	}

	storageDir := os.Getenv("STORAGE_DIR")
	if storageDir == "" {
		storageDir = os.TempDir()
	}
	filePath := filepath.Join(storageDir, "exports", jobID+".csv")
	if err := os.MkdirAll(filepath.Dir(filePath), 0750); err != nil {
		_ = s.UpdateJobStatus(ctx, jobID, "failed", "", err.Error())
		return fmt.Errorf("create export dir: %w", err)
	}

	switch job.Type {
	case "learning_progress_csv":
		// Defense-in-depth: reject if params.user_id differs from the job's created_by.
		var jobParams map[string]any
		if job.ParamsJSON != "" {
			_ = json.Unmarshal([]byte(job.ParamsJSON), &jobParams)
		}
		if jobParams != nil {
			if uid, ok := jobParams["user_id"].(string); ok && uid != "" && uid != job.CreatedBy {
				errMsg := "params.user_id must match the job creator"
				_ = s.UpdateJobStatus(ctx, jobID, "failed", "", errMsg)
				return fmt.Errorf("%s", errMsg)
			}
		}
		if err := generateLearningCSV(ctx, pool, job, filePath); err != nil {
			_ = s.UpdateJobStatus(ctx, jobID, "failed", "", err.Error())
			return err
		}
	case "reconciliation_export":
		// Scope the export to runs created by the job creator unless an admin
		// override (created_by absent or empty) was set at creation time.
		var reconParams map[string]any
		if job.ParamsJSON != "" {
			_ = json.Unmarshal([]byte(job.ParamsJSON), &reconParams)
		}
		scopedBy := ""
		if reconParams != nil {
			if v, ok := reconParams["created_by"].(string); ok {
				scopedBy = v
			}
		}
		if err := generateReconciliationCSV(ctx, pool, filePath, scopedBy); err != nil {
			_ = s.UpdateJobStatus(ctx, jobID, "failed", "", err.Error())
			return err
		}
	default:
		msg := fmt.Sprintf("unknown job type: %s", job.Type)
		_ = s.UpdateJobStatus(ctx, jobID, "failed", "", msg)
		return fmt.Errorf("%s", msg)
	}

	return s.UpdateJobStatus(ctx, jobID, "completed", filePath, "")
}

// generateLearningCSV writes a learning progress CSV to filePath.
func generateLearningCSV(ctx context.Context, pool *pgxpool.Pool, job *ExportJob, filePath string) error {
	// Parse params to find target user_id
	var params map[string]any
	if job.ParamsJSON != "" {
		_ = json.Unmarshal([]byte(job.ParamsJSON), &params)
	}
	if params == nil {
		params = map[string]any{}
	}

	targetUserID := job.CreatedBy
	if uid, ok := params["user_id"].(string); ok && uid != "" {
		targetUserID = uid
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create csv file: %w", err)
	}
	defer f.Close()

	cw := csv.NewWriter(f)
	defer cw.Flush()

	if err := cw.Write([]string{
		"path_id", "path_title", "enrollment_status", "enrolled_at", "completed_at",
		"resource_id", "resource_title", "content_type", "item_type",
		"progress_status", "progress_pct", "resource_completed_at",
	}); err != nil {
		return err
	}

	rows, err := pool.Query(ctx, `
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
		ORDER BY lp.title, lpi.item_type DESC, lpi.sort_order`, targetUserID)
	if err != nil {
		return fmt.Errorf("query learning data: %w", err)
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

		_ = cw.Write([]string{
			pathID, pathTitle, enrollStatus,
			enrolledAt.Format(time.RFC3339),
			nullTimeStr(enrollCompletedAt),
			resourceID, resourceTitle, contentType, itemType,
			progressStatus, fmt.Sprintf("%.1f", progressPct),
			nullTimeStr(progressCompletedAt),
		})
	}
	return cw.Error()
}

// generateReconciliationCSV writes a reconciliation summary CSV to filePath.
func generateReconciliationCSV(ctx context.Context, pool *pgxpool.Pool, filePath, scopedBy string) error {
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create csv file: %w", err)
	}
	defer f.Close()

	return writeReconciliationCSV(ctx, pool, f, scopedBy)
}

// writeReconciliationCSV writes a reconciliation summary CSV to w.
// When scopedBy is non-empty, only runs owned by that user are included;
// an empty string means unscoped (admin export). Uses COALESCE(initiated_by, run_by)
// so both API-created runs (initiated_by) and legacy rows (run_by) are matched.
func writeReconciliationCSV(ctx context.Context, pool *pgxpool.Pool, w io.Writer, scopedBy string) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write([]string{
		"run_id", "batch_id", "initiated_by", "started_at", "completed_at",
		"status", "total_variances", "total_variance_amount",
	}); err != nil {
		return err
	}

	var rows pgx.Rows
	var err error
	if scopedBy != "" {
		rows, err = pool.Query(ctx, `
			SELECT r.id, r.batch_id,
			       COALESCE(r.initiated_by::TEXT, r.run_by::TEXT, ''),
			       r.started_at, r.completed_at,
			       r.status, r.total_variances, r.total_variance_amount
			FROM reconciliation_runs r
			WHERE COALESCE(r.initiated_by::TEXT, r.run_by::TEXT) = $1
			ORDER BY r.started_at DESC
			LIMIT 1000`, scopedBy)
	} else {
		rows, err = pool.Query(ctx, `
			SELECT r.id, r.batch_id,
			       COALESCE(r.initiated_by::TEXT, r.run_by::TEXT, ''),
			       r.started_at, r.completed_at,
			       r.status, r.total_variances, r.total_variance_amount
			FROM reconciliation_runs r
			ORDER BY r.started_at DESC
			LIMIT 1000`)
	}
	if err != nil {
		return fmt.Errorf("query reconciliation data: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var runID, batchID, initiatedBy, status string
		var startedAt time.Time
		var completedAt *time.Time
		var totalVariances int
		var totalVarianceAmount int64

		if err := rows.Scan(&runID, &batchID, &initiatedBy, &startedAt, &completedAt,
			&status, &totalVariances, &totalVarianceAmount); err != nil {
			continue
		}

		_ = cw.Write([]string{
			runID, batchID, initiatedBy,
			startedAt.Format(time.RFC3339),
			nullTimeStr(completedAt),
			status,
			fmt.Sprintf("%d", totalVariances),
			fmt.Sprintf("%d", totalVarianceAmount),
		})
	}
	return cw.Error()
}

// WriteReconciliationCSVForTest exposes writeReconciliationCSV for integration
// tests that need to verify the generated CSV content against a real database.
func WriteReconciliationCSVForTest(ctx context.Context, pool *pgxpool.Pool, w io.Writer, scopedBy string) error {
	return writeReconciliationCSV(ctx, pool, w, scopedBy)
}

func nullTimeStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
