// cmd/worker/main.go — Portal background worker entrypoint.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"portal/internal/app/exports"
	"portal/internal/app/search"
	"portal/internal/platform/logging"
	"portal/internal/platform/postgres"
)

func main() {
	log := logging.New(os.Stdout, logging.INFO, "worker")

	dbCfg, err := postgres.ConfigFromEnv()
	if err != nil {
		log.Error("database config error", map[string]any{"err": err.Error()})
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := postgres.Open(ctx, dbCfg)
	if err != nil {
		log.Error("database connect error", map[string]any{"err": err.Error()})
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("worker started", map[string]any{"host": dbCfg.Host})

	exportStore := exports.NewStore(pool)
	searchStore := search.NewStore(pool)

	// Graceful shutdown
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	// Export job processor (poll every 30s).
	//
	// Lifecycle: queued → processing → completed | failed.
	// Retry: when a job fails and attempt_count < max_attempts it is re-queued
	// after incrementing attempt_count. A job_retry_events row records the fact.
	// After max_attempts a compensation_events row is written and the job stays
	// in 'failed' state permanently.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Fetch one queued/retryable job atomically.
				var jobID string
				var attemptCount, maxAttempts int
				err := pool.QueryRow(ctx, `
					UPDATE export_jobs
					SET status         = 'processing',
					    attempt_count  = attempt_count + 1
					WHERE id = (
						SELECT id FROM export_jobs
						WHERE status IN ('queued', 'retry')
						  AND attempt_count < max_attempts
						LIMIT 1
						FOR UPDATE SKIP LOCKED
					)
					RETURNING id, attempt_count, max_attempts`,
				).Scan(&jobID, &attemptCount, &maxAttempts)
				if err != nil {
					continue // no jobs queued or error
				}

				log.Info("worker: processing export job",
					map[string]any{"job_id": jobID, "attempt": attemptCount, "max": maxAttempts})

				// Create a scheduled_job_runs row that tracks this processing
				// attempt. retry/compensation events reference this run_id, so
				// it must exist BEFORE the ProcessJob call.
				runID := newScheduledRun(ctx, pool, "export_process", jobID)

				if err := exportStore.ProcessJob(ctx, jobID, pool); err != nil {
					log.Error("worker: job failed",
						map[string]any{"job_id": jobID, "err": err.Error(), "attempt": attemptCount})

					if attemptCount < maxAttempts {
						// Re-queue for retry. Write to both error columns so the
						// error is visible regardless of which column the reader
						// queries (error_message from migration 001, error_msg
						// from migration 006).
						_, _ = pool.Exec(ctx,
							`UPDATE export_jobs SET status = 'retry', error_message = $1, error_msg = $1 WHERE id = $2`,
							err.Error(), jobID)

						// Record a retry event linked to this processing run.
						_, _ = pool.Exec(ctx, `
							INSERT INTO job_retry_events (id, run_id, retry_number, scheduled_at, reason)
							VALUES (gen_random_uuid(), $1, $2, NOW(), $3)`,
							runID, attemptCount, err.Error())

						completeScheduledRun(ctx, pool, runID, fmt.Errorf("retry scheduled (attempt %d/%d): %w", attemptCount, maxAttempts, err))
					} else {
						// Permanently failed — write compensation event.
						_, _ = pool.Exec(ctx,
							`UPDATE export_jobs SET status = 'failed', error_message = $1, error_msg = $1 WHERE id = $2`,
							err.Error(), jobID)

						_, _ = pool.Exec(ctx, `
							INSERT INTO compensation_events (id, run_id, action, applied_at, result)
							VALUES (gen_random_uuid(), $1, 'export_permanently_failed', NOW(), $2)`,
							runID, fmt.Sprintf("job %s failed after %d attempts: %s", jobID, attemptCount, err.Error()))

						completeScheduledRun(ctx, pool, runID, err)
						log.Error("worker: export permanently failed",
							map[string]any{"job_id": jobID, "attempts": attemptCount})
					}
				} else {
					completeScheduledRun(ctx, pool, runID, nil)
				}
			}
		}
	}()

	// Incremental search-index updater (poll every 60s).
	//
	// The README promises "incremental update — background worker job rebuilds
	// affected documents on content change." This loop closes the gap between
	// the synchronous index refresh that catalog mutations do at write time
	// and the 02:00 UTC nightly rebuild — it catches resources that were
	// touched (tag re-attached, description edited via direct SQL, etc.)
	// without going through the catalog Store.
	//
	// Strategy: a resource is "stale" when it is published, not archived, and
	// its updated_at is newer than its search_documents.last_rebuilt_at (or
	// it has no search_documents row at all). We refresh up to 50 stale rows
	// per tick to bound the cost. Resources whose last refresh was less than
	// 5 seconds ago are skipped to avoid thrash from in-flight writes.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rows, err := pool.Query(ctx, `
					SELECT r.id
					FROM resources r
					LEFT JOIN search_documents sd ON sd.resource_id = r.id
					WHERE r.is_published = TRUE
					  AND r.is_archived  = FALSE
					  AND (sd.resource_id IS NULL
					       OR (r.updated_at > sd.last_rebuilt_at
					           AND sd.last_rebuilt_at < NOW() - INTERVAL '5 seconds'))
					ORDER BY r.updated_at ASC
					LIMIT 50`)
				if err != nil {
					log.Error("worker: search staleness query failed", map[string]any{"err": err.Error()})
					continue
				}

				var stale []string
				for rows.Next() {
					var id string
					if scanErr := rows.Scan(&id); scanErr == nil {
						stale = append(stale, id)
					}
				}
				rows.Close()

				if len(stale) == 0 {
					continue
				}
				log.Info("worker: refreshing search index for stale resources",
					map[string]any{"count": len(stale)})

				for _, id := range stale {
					if rerr := searchStore.UpdateResourceIndex(ctx, id); rerr != nil {
						log.Error("worker: incremental index refresh failed",
							map[string]any{"resource_id": id, "err": rerr.Error()})
					}
				}
			}
		}
	}()

	select {
	case <-shutdownCh:
		log.Info("worker shutting down")
		cancel()
	case <-ctx.Done():
	}
}

// newScheduledRun creates a scheduled_job_runs row that retry/compensation
// events can reference. Returns the run UUID.
func newScheduledRun(ctx context.Context, pool *pgxpool.Pool, jobType, detail string) string {
	id := uuid.New().String()
	_, _ = pool.Exec(ctx, `
		INSERT INTO scheduled_job_runs (id, job_type, trigger_source, status, error_summary)
		VALUES ($1, $2, 'worker', 'running', NULLIF($3, ''))`, id, jobType, detail)
	return id
}

// completeScheduledRun marks a scheduled_job_runs row as completed or failed.
func completeScheduledRun(ctx context.Context, pool *pgxpool.Pool, runID string, runErr error) {
	status := "completed"
	var errSummary string
	if runErr != nil {
		status = "failed"
		errSummary = runErr.Error()
	}
	_, _ = pool.Exec(ctx, `
		UPDATE scheduled_job_runs
		SET completed_at  = NOW(),
		    status        = $2,
		    duration_ms   = EXTRACT(EPOCH FROM (NOW() - started_at))::bigint * 1000,
		    error_summary = NULLIF($3, '')
		WHERE id = $1`, runID, status, errSummary)
}
