// cmd/scheduler/main.go — Portal scheduled task runner entrypoint.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"portal/internal/app/search"
	"portal/internal/platform/logging"
	"portal/internal/platform/postgres"
)

func main() {
	log := logging.New(os.Stdout, logging.INFO, "scheduler")

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
	log.Info("scheduler started", map[string]any{"host": dbCfg.Host})

	searchStore := search.NewStore(pool)

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	// Nightly search index rebuild — fires at 02:00 UTC every day, matching
	// the cadence documented in README.md. We sleep until the next 02:00 UTC
	// boundary, rebuild, then sleep 24h. On startup we do NOT do an immediate
	// rebuild — that is the worker's job via incremental updates, and the API
	// exposes POST /search/rebuild for ad-hoc admin rebuilds.
	go func() {
		const targetHourUTC = 2
		for {
			wait := durationUntilNextUTCHour(time.Now().UTC(), targetHourUTC)
			log.Info("scheduler: next search rebuild scheduled",
				map[string]any{"in": wait.String(), "target_utc": "02:00"})

			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
				log.Info("scheduler: rebuilding search index", nil)
				runID := recordRunStart(ctx, pool, "search_rebuild")
				_, rebuildErr := searchStore.RebuildIndex(ctx)
				recordRunEnd(ctx, pool, runID, rebuildErr)
				if rebuildErr != nil {
					log.Error("scheduler: search rebuild failed", map[string]any{"err": rebuildErr.Error()})
				}
			}
		}
	}()

	// Archive bucket refresh (every hour on the hour).
	go func() {
		// Align the first tick to the next top-of-hour so subsequent ticks
		// stay near the boundary rather than drifting with process start time.
		initial := time.Until(time.Now().UTC().Truncate(time.Hour).Add(time.Hour))
		select {
		case <-ctx.Done():
			return
		case <-time.After(initial):
		}

		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		refresh := func() {
			runID := recordRunStart(ctx, pool, "archive_refresh")
			err := searchStore.RefreshArchiveBuckets(ctx)
			recordRunEnd(ctx, pool, runID, err)
			if err != nil {
				log.Error("scheduler: archive refresh failed", map[string]any{"err": err.Error()})
			}
		}
		refresh() // run once at the first aligned tick

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()

	select {
	case <-shutdownCh:
		log.Info("scheduler shutting down")
		cancel()
	case <-ctx.Done():
	}
}

// recordRunStart inserts a scheduled_job_runs row and returns its UUID.
func recordRunStart(ctx context.Context, pool *pgxpool.Pool, jobType string) string {
	id := uuid.New().String()
	_, _ = pool.Exec(ctx, `
		INSERT INTO scheduled_job_runs (id, job_type, trigger_source, status)
		VALUES ($1, $2, 'scheduler', 'running')`, id, jobType)
	return id
}

// recordRunEnd updates the scheduled_job_runs row with the outcome.
func recordRunEnd(ctx context.Context, pool *pgxpool.Pool, runID string, runErr error) {
	if runID == "" {
		return
	}
	status := "completed"
	var errSummary string
	if runErr != nil {
		status = "failed"
		errSummary = runErr.Error()
	}
	_, _ = pool.Exec(ctx, `
		UPDATE scheduled_job_runs
		SET completed_at   = NOW(),
		    status         = $2,
		    duration_ms    = EXTRACT(EPOCH FROM (NOW() - started_at))::bigint * 1000,
		    error_summary  = NULLIF($3, '')
		WHERE id = $1`, runID, status, errSummary)
}

// durationUntilNextUTCHour returns the duration between `now` and the next
// occurrence of the given UTC hour (0–23). If `now` is already past today's
// target, it returns the duration to tomorrow's.
func durationUntilNextUTCHour(now time.Time, hourUTC int) time.Duration {
	target := time.Date(now.Year(), now.Month(), now.Day(), hourUTC, 0, 0, 0, time.UTC)
	if !now.Before(target) {
		target = target.Add(24 * time.Hour)
	}
	return target.Sub(now)
}
