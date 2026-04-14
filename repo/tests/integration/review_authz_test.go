// tests/integration/review_authz_test.go — real-stack integration tests for
// review and appeal object-authorization.
//
// These tests verify:
//   - Cross-user GET /reviews/:id is restricted to author, orders:read, or moderation:write.
//   - Cross-user GET /appeals/:id is restricted to appellant or appeals:decide.
//   - Hidden review returns 404 to non-moderator.
//   - Unauthenticated access returns 401.
//   - Non-existent review returns 404.
package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"portal/internal/app/audit"
	appconfig "portal/internal/app/config"
	"portal/internal/app/mfa"
	"portal/internal/app/permissions"
	"portal/internal/app/reviews"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
	platformstorage "portal/internal/platform/storage"
)

// TestReviewAppealAuthorization_RealStack verifies review and appeal
// object-authorization using the real middleware chain against a live DB.
func TestReviewAppealAuthorization_RealStack(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	userStore := users.NewStore(h.Pool)
	sessStore := sessions.NewStore(h.Pool)
	mfaStore := mfa.NewStore(h.Pool, nil)
	cfgStore := appconfig.NewStore(h.Pool)
	auditStore := audit.NewStore(h.Pool)

	storageDir := t.TempDir()
	fileStore, err := platformstorage.NewStore(storageDir)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	reviewStore := reviews.NewStore(h.Pool, fileStore)
	reviewHandler := reviews.NewHandlerWithAudit(reviewStore, userStore, fileStore, auditStore)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)

	// Review routes
	g.POST("/reviews", reviewHandler.CreateReview, mw.RequirePermission("reviews:write"))
	g.GET("/reviews/:id", reviewHandler.GetReview)

	// Appeal routes
	g.POST("/appeals", reviewHandler.CreateAppeal, mw.RequirePermission("appeals:write"))
	g.GET("/appeals/:id", reviewHandler.GetAppeal)

	// ── Users ────────────────────────────────────────────────────────────────
	reviewer := h.MakeUser(ctx, "reviewer-user", "pw", "procurement")    // has reviews:write, orders:read
	otherUser := h.MakeUser(ctx, "other-user", "pw", "learner")          // has catalog:read only
	moderator := h.MakeUser(ctx, "moderator-user", "pw", "moderator")    // has moderation:write
	appellant := h.MakeUser(ctx, "appellant-user", "pw", "procurement")  // has appeals:write
	arbiter := h.MakeUser(ctx, "arbiter-user", "pw", "approver")         // has appeals:decide

	reviewerToken := h.SeedSession(ctx, reviewer, true)
	otherToken := h.SeedSession(ctx, otherUser, true)
	moderatorToken := h.SeedSession(ctx, moderator, true)
	appellantToken := h.SeedSession(ctx, appellant, true)
	arbiterToken := h.SeedSession(ctx, arbiter, true)

	// Seed a vendor order for the review.
	orderID := "33333333-3333-3333-3333-333333333333"
	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO vendor_orders (id, vendor_name, order_number, order_date, status, total_amount, currency)
		VALUES ($1, 'Delta Corp', 'ORD-REV', '2026-06-01', 'received', 10000, 'USD')`, orderID)

	// ── Create a review ──────────────────────────────────────────────────────
	createReviewBody := map[string]any{
		"order_id": orderID,
		"rating":   2, // low rating so it qualifies for appeal
		"body":     "Poor quality service",
	}
	rec := h.do(t, "POST", "/api/v1/reviews", reviewerToken, createReviewBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create review: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var review map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &review)
	reviewID, _ := review["id"].(string)

	// ── Test 1: Author can read own review ──────────────────────────────────
	t.Run("author_reads_own_review", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/%s", reviewID), reviewerToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 2: User without orders:read or moderation:write is forbidden ───
	t.Run("other_user_forbidden_from_review", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/%s", reviewID), otherToken, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 3: Moderator can read any review ───────────────────────────────
	t.Run("moderator_reads_any_review", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/%s", reviewID), moderatorToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 4: Non-existent review returns 404 ─────────────────────────────
	t.Run("nonexistent_review_returns_404", func(t *testing.T) {
		rec := h.do(t, "GET", "/api/v1/reviews/00000000-0000-0000-0000-000000000000", reviewerToken, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})

	// ── Test 5: Unauthenticated request returns 401 ─────────────────────────
	t.Run("unauthenticated_returns_401", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/%s", reviewID), "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	// ── Create an appeal (from a different user with appeals:write) ──────────
	appealBody := map[string]any{
		"review_id": reviewID,
		"reason":    "The review is unfair and based on wrong information",
	}
	rec = h.do(t, "POST", "/api/v1/appeals", appellantToken, appealBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create appeal: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var appeal map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &appeal)
	appealID, _ := appeal["id"].(string)

	// ── Test 6: Appellant can read own appeal ───────────────────────────────
	t.Run("appellant_reads_own_appeal", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/appeals/%s", appealID), appellantToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 7: Other user cannot read someone else's appeal ────────────────
	t.Run("cross_user_appeal_forbidden", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/appeals/%s", appealID), otherToken, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 8: Arbiter (appeals:decide) can read any appeal ────────────────
	t.Run("arbiter_reads_any_appeal", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/appeals/%s", appealID), arbiterToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 9: Non-existent appeal returns 404 ─────────────────────────────
	t.Run("nonexistent_appeal_returns_404", func(t *testing.T) {
		rec := h.do(t, "GET", "/api/v1/appeals/00000000-0000-0000-0000-000000000000", appellantToken, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})

	// ── Test 10: Hidden review returns 404 to non-moderator ─────────────────
	t.Run("hidden_review_returns_404_to_non_moderator", func(t *testing.T) {
		// Manually hide the review.
		_, err := h.Pool.Exec(ctx, `UPDATE reviews SET visibility = 'hidden' WHERE id = $1`, reviewID)
		if err != nil {
			t.Fatalf("hide review: %v", err)
		}

		// Non-moderator with orders:read gets 404 for hidden review.
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/%s", reviewID), reviewerToken, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for hidden review, got %d — body: %s", rec.Code, rec.Body.String())
		}

		// Moderator can still see it.
		rec = h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/%s", reviewID), moderatorToken, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for moderator on hidden review, got %d", rec.Code)
		}

		// Restore visibility for any subsequent tests.
		_, _ = h.Pool.Exec(ctx, `UPDATE reviews SET visibility = 'visible' WHERE id = $1`, reviewID)
	})
}

// TestAttachmentEvidenceDownload_RealStack verifies download authorization for
// review attachments and appeal evidence using the real middleware chain.
//
// The download handlers check authorization BEFORE attempting to open files,
// so we can seed DB rows with dummy file_path values and test the authz layer
// without needing actual files on disk. Authorized requests will get a "file
// not found on disk" 404 (which proves authz passed); unauthorized requests
// get 403.
func TestAttachmentEvidenceDownload_RealStack(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	userStore := users.NewStore(h.Pool)
	sessStore := sessions.NewStore(h.Pool)
	mfaStore := mfa.NewStore(h.Pool, nil)
	cfgStore := appconfig.NewStore(h.Pool)
	auditStore := audit.NewStore(h.Pool)

	storageDir := t.TempDir()
	fileStore, err := platformstorage.NewStore(storageDir)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	reviewStore := reviews.NewStore(h.Pool, fileStore)
	reviewHandler := reviews.NewHandlerWithAudit(reviewStore, userStore, fileStore, auditStore)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)

	g.POST("/reviews", reviewHandler.CreateReview, mw.RequirePermission("reviews:write"))
	g.GET("/reviews/attachments/:id", reviewHandler.DownloadAttachment)
	g.POST("/appeals", reviewHandler.CreateAppeal, mw.RequirePermission("appeals:write"))
	g.GET("/appeals/evidence/:id", reviewHandler.DownloadEvidence)

	// ── Users ────────────────────────────────────────────────────────────────
	reviewer := h.MakeUser(ctx, "dl-reviewer", "pw", "procurement")
	otherUser := h.MakeUser(ctx, "dl-other", "pw", "learner")
	arbiter := h.MakeUser(ctx, "dl-arbiter", "pw", "approver")

	reviewerToken := h.SeedSession(ctx, reviewer, true)
	otherToken := h.SeedSession(ctx, otherUser, true)
	arbiterToken := h.SeedSession(ctx, arbiter, true)

	// Seed a vendor order + review directly for attachment tests.
	orderID := "44444444-4444-4444-4444-444444444444"
	reviewID := "55555555-5555-5555-5555-555555555555"
	attachmentID := "66666666-6666-6666-6666-666666666666"

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO vendor_orders (id, vendor_name, order_number, order_date, status, total_amount, currency)
		VALUES ($1, 'Echo Corp', 'ORD-DL', '2026-07-01', 'received', 5000, 'USD')`, orderID)

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO reviews (id, order_id, reviewer_id, rating, review_text, visibility, created_at, updated_at)
		VALUES ($1, $2, $3, 1, 'Terrible', 'visible', NOW(), NOW())`, reviewID, orderID, reviewer)

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO review_attachments (id, review_id, file_path, original_name, content_type, size_bytes, checksum, uploaded_by)
		VALUES ($1, $2, '/nonexistent/test-attachment.jpg', 'test.jpg', 'image/jpeg', 1024, 'abc123', $3)`,
		attachmentID, reviewID, reviewer)

	// ── Attachment download tests ────────────────────────────────────────────

	// Test 1: Author can access attachment (will get 404 for file, not 403).
	t.Run("attachment_author_authorized", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/attachments/%s", attachmentID), reviewerToken, nil)
		// File doesn't exist on disk, so we expect 404 "not found on disk" — not 403.
		if rec.Code == http.StatusForbidden {
			t.Fatalf("author should be authorized, got 403 — body: %s", rec.Body.String())
		}
		// Expect 404 (file not on disk) which proves authorization passed.
		if rec.Code != http.StatusNotFound {
			t.Logf("unexpected status %d (expected 404 for missing file after authz pass) — body: %s", rec.Code, rec.Body.String())
		}
	})

	// Test 2: Other user (no orders:read, no moderation:write) is forbidden.
	t.Run("attachment_other_user_forbidden", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/attachments/%s", attachmentID), otherToken, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for unauthorized user, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// Test 3: Arbiter has orders:read (via approver role) — authorized.
	t.Run("attachment_orders_read_authorized", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/attachments/%s", attachmentID), arbiterToken, nil)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("user with orders:read should be authorized, got 403 — body: %s", rec.Body.String())
		}
	})

	// Test 4: Non-existent attachment returns 404.
	t.Run("attachment_nonexistent_returns_404", func(t *testing.T) {
		rec := h.do(t, "GET", "/api/v1/reviews/attachments/00000000-0000-0000-0000-000000000000", reviewerToken, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for nonexistent attachment, got %d", rec.Code)
		}
	})

	// Test 5: Unauthenticated returns 401.
	t.Run("attachment_unauthenticated_returns_401", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/reviews/attachments/%s", attachmentID), "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	// ── Evidence download tests ──────────────────────────────────────────────

	// Create an appeal + evidence row directly.
	appellantUser := h.MakeUser(ctx, "dl-appellant", "pw", "procurement")
	appellantToken := h.SeedSession(ctx, appellantUser, true)

	appealID := "77777777-7777-7777-7777-777777777777"
	evidenceID := "88888888-8888-8888-8888-888888888888"

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO negative_review_appeals (id, review_id, appealed_by, appeal_reason, status, submitted_at)
		VALUES ($1, $2, $3, 'Unfair review', 'pending', NOW())`, appealID, reviewID, appellantUser)

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO appeal_evidence (id, appeal_id, file_path, original_name, content_type, size_bytes, checksum, uploaded_by, uploaded_at)
		VALUES ($1, $2, '/nonexistent/test-evidence.pdf', 'evidence.pdf', 'application/pdf', 2048, 'def456', $3, NOW())`,
		evidenceID, appealID, appellantUser)

	// Test 6: Appellant can access evidence.
	t.Run("evidence_appellant_authorized", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/appeals/evidence/%s", evidenceID), appellantToken, nil)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("appellant should be authorized, got 403 — body: %s", rec.Body.String())
		}
	})

	// Test 7: Other user cannot download evidence.
	t.Run("evidence_other_user_forbidden", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/appeals/evidence/%s", evidenceID), otherToken, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for unauthorized evidence access, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// Test 8: Arbiter (appeals:decide) can access evidence.
	t.Run("evidence_arbiter_authorized", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/appeals/evidence/%s", evidenceID), arbiterToken, nil)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("arbiter should be authorized, got 403 — body: %s", rec.Body.String())
		}
	})

	// Test 9: Non-existent evidence returns 404.
	t.Run("evidence_nonexistent_returns_404", func(t *testing.T) {
		rec := h.do(t, "GET", "/api/v1/appeals/evidence/00000000-0000-0000-0000-000000000000", appellantToken, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for nonexistent evidence, got %d", rec.Code)
		}
	})

	// Test 10: Unauthenticated returns 401.
	t.Run("evidence_unauthenticated_returns_401", func(t *testing.T) {
		rec := h.do(t, "GET", fmt.Sprintf("/api/v1/appeals/evidence/%s", evidenceID), "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}
