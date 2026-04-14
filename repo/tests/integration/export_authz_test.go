// tests/integration/export_authz_test.go — real-stack integration tests for
// export job ownership and download authorization.
//
// These tests verify that:
//   - A finance user can create reconciliation_export jobs.
//   - A non-finance user is forbidden from creating reconciliation_export jobs.
//   - A user can only see their own export jobs (non-admin).
//   - A user cannot access another user's export job by ID.
//   - Admin can see all export jobs.
//   - Download of another user's job is forbidden.
package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	appconfig "portal/internal/app/config"
	"portal/internal/app/exports"
	"portal/internal/app/mfa"
	"portal/internal/app/permissions"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
)

// TestExportJobAuthorization_RealStack verifies export job ownership,
// permission gating, and download authorization using the real middleware
// chain against a live database.
func TestExportJobAuthorization_RealStack(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	userStore := users.NewStore(h.Pool)
	sessStore := sessions.NewStore(h.Pool)
	mfaStore := mfa.NewStore(h.Pool, nil)
	cfgStore := appconfig.NewStore(h.Pool)
	exportStore := exports.NewStore(h.Pool)
	exportHandler := exports.NewHandler(exportStore, h.Pool, userStore)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)

	g.POST("/exports/jobs", exportHandler.CreateJob)
	g.GET("/exports/jobs", exportHandler.ListJobs)
	g.GET("/exports/jobs/:id", exportHandler.GetJob)
	g.GET("/exports/jobs/:id/download", exportHandler.DownloadJob)

	// ── Users ────────────────────────────────────────────────────────────────
	financeUser := h.MakeUser(ctx, "finance-export-user", "pw", "finance")
	learnerUser := h.MakeUser(ctx, "learner-export-user", "pw", "learner")
	adminUser := h.MakeUser(ctx, "admin-export-user", "pw", "admin")

	financeToken := h.SeedSession(ctx, financeUser, true)
	learnerToken := h.SeedSession(ctx, learnerUser, true)
	adminToken := h.SeedSession(ctx, adminUser, true)

	// ── Test 1: Finance user can create reconciliation_export ────────────────
	t.Run("finance_user_creates_recon_export", func(t *testing.T) {
		rec := h.do(t, "POST", "/api/v1/exports/jobs", financeToken,
			map[string]any{"type": "reconciliation_export"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d — body: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["type"] != "reconciliation_export" {
			t.Errorf("expected type=reconciliation_export, got %v", body["type"])
		}
		if body["status"] != "queued" {
			t.Errorf("expected status=queued, got %v", body["status"])
		}
	})

	// ── Test 2: Learner cannot create reconciliation_export (no exports:write) ──
	t.Run("learner_forbidden_from_recon_export", func(t *testing.T) {
		rec := h.do(t, "POST", "/api/v1/exports/jobs", learnerToken,
			map[string]any{"type": "reconciliation_export"})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 3: Learner CAN create learning_progress_csv ────────────────────
	t.Run("learner_creates_learning_export", func(t *testing.T) {
		rec := h.do(t, "POST", "/api/v1/exports/jobs", learnerToken,
			map[string]any{"type": "learning_progress_csv"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 4: User can only see own jobs in list ──────────────────────────
	t.Run("list_jobs_scoped_to_owner", func(t *testing.T) {
		rec := h.do(t, "GET", "/api/v1/exports/jobs", learnerToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		jobs, _ := body["jobs"].([]any)
		for _, j := range jobs {
			jm, _ := j.(map[string]any)
			if jm["created_by"] != learnerUser {
				t.Errorf("learner should only see own jobs, saw created_by=%v", jm["created_by"])
			}
		}
	})

	// ── Test 5: Admin sees all jobs ─────────────────────────────────────────
	t.Run("admin_sees_all_jobs", func(t *testing.T) {
		rec := h.do(t, "GET", "/api/v1/exports/jobs", adminToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		jobs, _ := body["jobs"].([]any)
		// Admin should see at least both the finance and learner jobs created above.
		if len(jobs) < 2 {
			t.Errorf("admin should see all jobs, only saw %d", len(jobs))
		}
	})

	// ── Test 6: User cannot access another user's job by ID ─────────────────
	t.Run("cross_user_get_job_forbidden", func(t *testing.T) {
		// Finance creates a job
		createRec := h.do(t, "POST", "/api/v1/exports/jobs", financeToken,
			map[string]any{"type": "reconciliation_export"})
		if createRec.Code != http.StatusCreated {
			t.Fatalf("create: %d", createRec.Code)
		}
		var created map[string]any
		_ = json.Unmarshal(createRec.Body.Bytes(), &created)
		jobID, _ := created["id"].(string)

		// Learner tries to access it
		getRec := h.do(t, "GET", fmt.Sprintf("/api/v1/exports/jobs/%s", jobID), learnerToken, nil)
		if getRec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for cross-user access, got %d — body: %s", getRec.Code, getRec.Body.String())
		}
	})

	// ── Test 7: User cannot download another user's job ─────────────────────
	t.Run("cross_user_download_forbidden", func(t *testing.T) {
		// Finance creates a job
		createRec := h.do(t, "POST", "/api/v1/exports/jobs", financeToken,
			map[string]any{"type": "reconciliation_export"})
		var created map[string]any
		_ = json.Unmarshal(createRec.Body.Bytes(), &created)
		jobID, _ := created["id"].(string)

		// Learner tries to download it
		dlRec := h.do(t, "GET", fmt.Sprintf("/api/v1/exports/jobs/%s/download", jobID), learnerToken, nil)
		if dlRec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for cross-user download, got %d", dlRec.Code)
		}
	})

	// ── Test 8: Download non-completed job returns 409 Conflict ─────────────
	t.Run("download_queued_job_returns_conflict", func(t *testing.T) {
		createRec := h.do(t, "POST", "/api/v1/exports/jobs", financeToken,
			map[string]any{"type": "reconciliation_export"})
		var created map[string]any
		_ = json.Unmarshal(createRec.Body.Bytes(), &created)
		jobID, _ := created["id"].(string)

		dlRec := h.do(t, "GET", fmt.Sprintf("/api/v1/exports/jobs/%s/download", jobID), financeToken, nil)
		if dlRec.Code != http.StatusConflict {
			t.Errorf("expected 409 for queued job download, got %d — body: %s", dlRec.Code, dlRec.Body.String())
		}
	})
}
