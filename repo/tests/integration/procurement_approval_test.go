// tests/integration/procurement_approval_test.go — exercises the procurement
// approval workflow end-to-end against a real PostgreSQL database with the
// full middleware chain (session validation → permission gate → handler).
//
// This is the kind of test that catches the defects the auditor flagged in
// earlier rounds: write routes guarded by read permissions, self-approval
// loopholes, missing audit rows. Fakes can't catch any of these because they
// do not run the real RBAC SQL or the real route registrations.
package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"portal/internal/app/audit"
	"portal/internal/app/mfa"
	"portal/internal/app/permissions"
	"portal/internal/app/procurement"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
	appconfig "portal/internal/app/config"
)

// TestProcurementApproval_FullStack drives the real Echo router + middleware
// + procurement handler + procurement store against a fresh DB. It asserts:
//
//  1. A procurement specialist creates an order successfully.
//  2. The same procurement user is forbidden from approving their own order
//     (segregation-of-duties guard in handler).
//  3. A user who only has orders:write but not orders:approve is forbidden
//     from approving anyone's order (route-level permission gate).
//  4. An approver successfully approves the order.
//  5. The approval is recorded as an audit row.
//  6. The order's approved_by/approved_at columns are populated.
func TestProcurementApproval_FullStack(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	// ── Real stores wired exactly like cmd/api/main.go does ──────────────────
	auditStore := audit.NewStore(h.Pool)
	userStore  := users.NewStore(h.Pool)
	sessStore  := sessions.NewStore(h.Pool)
	mfaStore   := mfa.NewStore(h.Pool, nil)
	cfgStore   := appconfig.NewStore(h.Pool)
	procStore  := procurement.NewStore(h.Pool)
	procHandler := procurement.NewHandlerWithAudit(procStore, auditStore)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)

	// Mount the routes under test (mirrors the real registration in main.go).
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)
	g.POST("/procurement/orders",                procHandler.CreateOrder,  mw.RequirePermission("orders:write"))
	g.POST("/procurement/orders/:id/approve",    procHandler.ApproveOrder, mw.RequirePermission("orders:approve"))
	g.POST("/procurement/orders/:id/reject",     procHandler.RejectOrder,  mw.RequirePermission("orders:approve"))

	// ── Fixture users — three personas exercising distinct permission paths ──
	procUserID     := h.MakeUser(ctx, "alice-proc",     "x", "procurement")
	approverUserID := h.MakeUser(ctx, "bob-approver",   "x", "approver")
	otherWriter    := h.MakeUser(ctx, "carol-writeonly","x", "procurement") // has orders:write, NOT orders:approve

	procToken     := h.SeedSession(ctx, procUserID,     true)
	approverToken := h.SeedSession(ctx, approverUserID, true)
	otherToken    := h.SeedSession(ctx, otherWriter,    true)

	// ── 1. Procurement creates an order ──────────────────────────────────────
	createBody := map[string]any{
		"vendor_name":  "Acme LAN Switches",
		"description":  "10 x 48-port switches",
		"total_amount": 1234.56,
	}
	rec := h.do(t, "POST", "/api/v1/procurement/orders", procToken, createBody)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create order: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var order map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &order); err != nil {
		t.Fatalf("unmarshal order: %v (body=%s)", err, rec.Body.String())
	}
	orderID, _ := order["id"].(string)
	if orderID == "" {
		t.Fatalf("created order is missing id: %v", order)
	}

	// ── 2. Self-approval is forbidden ────────────────────────────────────────
	rec = h.do(t, "POST", "/api/v1/procurement/orders/"+orderID+"/approve", procToken, nil)
	// Procurement specialist does NOT hold orders:approve, so this is rejected
	// at the route middleware (403) BEFORE the self-approval guard runs. Either
	// 403 outcome is acceptable — the security property we care about is "the
	// creator cannot approve their own order".
	if rec.Code != http.StatusForbidden {
		t.Errorf("self-approval should be forbidden (route gate): got %d, body=%s",
			rec.Code, rec.Body.String())
	}

	// ── 3. orders:write without orders:approve is forbidden ──────────────────
	rec = h.do(t, "POST", "/api/v1/procurement/orders/"+orderID+"/approve", otherToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("orders:write without orders:approve should be forbidden: got %d, body=%s",
			rec.Code, rec.Body.String())
	}

	// ── 4. Approver successfully approves ────────────────────────────────────
	rec = h.do(t, "POST", "/api/v1/procurement/orders/"+orderID+"/approve", approverToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("approver should succeed: got %d, body=%s", rec.Code, rec.Body.String())
	}

	// ── 5. Audit row exists ──────────────────────────────────────────────────
	if n := h.AuditCount(ctx, "procurement.order.approve", orderID); n != 1 {
		t.Errorf("expected 1 audit row for procurement.order.approve, got %d", n)
	}

	// ── 6. Approval-state columns populated ──────────────────────────────────
	var approvedBy *string
	var status string
	if err := h.Pool.QueryRow(ctx,
		`SELECT status, approved_by::text FROM vendor_orders WHERE id = $1`, orderID,
	).Scan(&status, &approvedBy); err != nil {
		t.Fatalf("read order: %v", err)
	}
	if status != "received" {
		t.Errorf("expected status=received after approval, got %q", status)
	}
	if approvedBy == nil || *approvedBy != approverUserID {
		t.Errorf("expected approved_by=%s, got %v", approverUserID, approvedBy)
	}
}

// TestProcurementSelfApprovalGuard isolates the self-approval guard by giving
// the same user BOTH orders:write and orders:approve. This is the case the
// route-level RBAC alone would not catch — only the handler's
// procurement.self_approval_forbidden check rejects it.
func TestProcurementSelfApprovalGuard(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	auditStore := audit.NewStore(h.Pool)
	userStore  := users.NewStore(h.Pool)
	sessStore  := sessions.NewStore(h.Pool)
	mfaStore   := mfa.NewStore(h.Pool, nil)
	cfgStore   := appconfig.NewStore(h.Pool)
	procStore  := procurement.NewStore(h.Pool)
	procHandler := procurement.NewHandlerWithAudit(procStore, auditStore)
	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)

	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)
	g.POST("/procurement/orders",             procHandler.CreateOrder,  mw.RequirePermission("orders:write"))
	g.POST("/procurement/orders/:id/approve", procHandler.ApproveOrder, mw.RequirePermission("orders:approve"))

	// Grant BOTH permissions to one user (procurement + approver roles).
	dualUserID := h.MakeUser(ctx, "dave-dual", "x", "procurement", "approver")
	token      := h.SeedSession(ctx, dualUserID, true)

	rec := h.do(t, "POST", "/api/v1/procurement/orders", token,
		map[string]any{"vendor_name": "Self Vendor", "description": "n/a", "total_amount": 99.0})
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create order: %d %s", rec.Code, rec.Body.String())
	}
	var order map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &order)
	orderID, _ := order["id"].(string)

	// Same user attempts to approve their own order. Even though they hold
	// orders:approve (so the route gate lets the request through), the handler
	// must reject it because created_by == caller.
	rec = h.do(t, "POST", "/api/v1/procurement/orders/"+orderID+"/approve", token, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("handler self-approval guard must return 403, got %d body=%s",
			rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if code, _ := errBody["code"].(string); code != "procurement.self_approval_forbidden" {
		t.Errorf("expected code=procurement.self_approval_forbidden, got %q (body=%s)",
			code, rec.Body.String())
	}

	// And no audit row should exist for the rejected attempt.
	if n := h.AuditCount(ctx, "procurement.order.approve", orderID); n != 0 {
		t.Errorf("rejected approval must not write an audit row, got %d", n)
	}
}

// do issues a request against h.Echo with the session cookie set, returning the
// recorded response. JSON-encodes a non-nil body.
func (h *Harness) do(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessions.CookieName, Value: token})

	rec := httptest.NewRecorder()
	h.Echo.ServeHTTP(rec, req)
	return rec
}

// guard against unused imports until more tests come online
var _ = fmt.Sprintf
