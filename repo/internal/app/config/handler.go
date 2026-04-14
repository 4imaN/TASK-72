// Package config — HTTP handlers for the configuration center.
package config

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"portal/internal/app/common"
)

// AuditRecorder is the minimal interface a Handler needs to record privileged
// mutations to the audit log. Implemented by internal/app/audit.Store.
type AuditRecorder interface {
	Record(ctx context.Context, evt AuditEvent)
}

// AuditEvent mirrors audit.Event so callers do not need to import the audit
// package directly (avoids an import cycle if audit ever needs config types).
type AuditEvent struct {
	ActorID    string
	Action     string
	Category   string
	TargetType string
	TargetID   string
	OldValue   any
	NewValue   any
	IPAddress  string
}

// Handler exposes config center HTTP endpoints.
type Handler struct {
	store *Store
	audit AuditRecorder // optional; admin mutations are audited when wired
}

// NewHandler constructs a Handler.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// NewHandlerWithAudit constructs a Handler that records privileged mutations
// (flag/param/version-rule changes) into the audit log via the given recorder.
func NewHandlerWithAudit(store *Store, audit AuditRecorder) *Handler {
	return &Handler{store: store, audit: audit}
}

func (h *Handler) recordAudit(c echo.Context, evt AuditEvent) {
	if h.audit == nil {
		return
	}
	if evt.ActorID == "" {
		if uid, _ := c.Get("user_id").(string); uid != "" {
			evt.ActorID = uid
		}
	}
	if evt.IPAddress == "" {
		evt.IPAddress = c.RealIP()
	}
	h.audit.Record(c.Request().Context(), evt)
}

// ── Config Flags ──────────────────────────────────────────────────────────────

// ListFlags returns all feature flags.
// GET /api/v1/admin/config/flags
func (h *Handler) ListFlags(c echo.Context) error {
	flags, err := h.store.ListFlags(c.Request().Context())
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"flags": flags})
}

// SetFlag upserts a feature flag.
// PUT /api/v1/admin/config/flags/:key
func (h *Handler) SetFlag(c echo.Context) error {
	key := c.Param("key")
	if key == "" {
		return common.BadRequest(c, "validation.required", "key is required")
	}

	var req struct {
		Enabled           bool     `json:"enabled"`
		RolloutPercentage int      `json:"rollout_percentage"`
		TargetRoles       []string `json:"target_roles"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}

	// Default rollout_percentage to 100 if not supplied
	if req.RolloutPercentage == 0 {
		req.RolloutPercentage = 100
	}

	// Capture the prior state for the audit trail before applying the change.
	prev, _ := h.store.GetFlag(c.Request().Context(), key)

	if err := h.store.SetFlag(c.Request().Context(), key, req.Enabled,
		req.RolloutPercentage, req.TargetRoles); err != nil {
		return common.Internal(c)
	}

	flag, err := h.store.GetFlag(c.Request().Context(), key)
	if err != nil {
		return common.Internal(c)
	}

	h.recordAudit(c, AuditEvent{
		Action:     "config.flag.set",
		Category:   "config",
		TargetType: "config_flag",
		TargetID:   key,
		OldValue:   prev,
		NewValue:   flag,
	})
	return c.JSON(http.StatusOK, flag)
}

// ── Config Parameters ─────────────────────────────────────────────────────────

// ListParams returns all configuration parameters.
// GET /api/v1/admin/config/params
func (h *Handler) ListParams(c echo.Context) error {
	params, err := h.store.ListParams(c.Request().Context())
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"params": params})
}

// SetParam upserts a configuration parameter.
// PUT /api/v1/admin/config/params/:key
func (h *Handler) SetParam(c echo.Context) error {
	key := c.Param("key")
	if key == "" {
		return common.BadRequest(c, "validation.required", "key is required")
	}

	var req struct {
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}

	prev, _ := h.store.GetParam(c.Request().Context(), key)

	if err := h.store.SetParam(c.Request().Context(), key, req.Value, req.Description); err != nil {
		return common.Internal(c)
	}

	h.recordAudit(c, AuditEvent{
		Action:     "config.param.set",
		Category:   "config",
		TargetType: "config_param",
		TargetID:   key,
		OldValue:   prev,
		NewValue:   req.Value,
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "ok", "key": key, "value": req.Value})
}

// ── Version Rules ─────────────────────────────────────────────────────────────

// ListVersionRules returns all client version rules.
// GET /api/v1/admin/config/version-rules
func (h *Handler) ListVersionRules(c echo.Context) error {
	rules, err := h.store.ListVersionRules(c.Request().Context())
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"rules": rules})
}

// SetVersionRule upserts a version rule.
// PUT /api/v1/admin/config/version-rules
//
// Body:
//
//	{
//	  "min_version":        "2.0.0",
//	  "max_version":        "",          // optional upper bound
//	  "action":             "block",     // block | warn | read_only
//	  "message":            "Please upgrade",
//	  "grace_until":        "2026-05-01T00:00:00Z",  // optional ISO8601
//	  "grace_period_days":  14           // alternative: server computes grace_until
//	}
//
// Exactly one of grace_until / grace_period_days may be supplied. When both are
// set, grace_until wins. Pass neither to clear the grace window.
func (h *Handler) SetVersionRule(c echo.Context) error {
	var req struct {
		MinVersion      string `json:"min_version"`
		MaxVersion      string `json:"max_version"`
		Action          string `json:"action"`
		Message         string `json:"message"`
		GraceUntil      string `json:"grace_until"`
		GracePeriodDays int    `json:"grace_period_days"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.MinVersion == "" {
		return common.BadRequest(c, "validation.required", "min_version is required")
	}
	if req.Action == "" {
		req.Action = "block"
	}

	switch req.Action {
	case "block", "warn", "read_only":
		// valid
	default:
		return common.BadRequest(c, "validation.invalid_action",
			"action must be block, warn, or read_only")
	}

	// Resolve the grace window. grace_until takes precedence; otherwise compute
	// it from grace_period_days; otherwise leave it unset (zero time → NULL).
	var graceUntil time.Time
	switch {
	case req.GraceUntil != "":
		t, err := time.Parse(time.RFC3339, req.GraceUntil)
		if err != nil {
			return common.BadRequest(c, "validation.invalid_grace_until",
				"grace_until must be an RFC3339 timestamp (e.g. 2026-05-01T00:00:00Z)")
		}
		maxGrace := time.Now().UTC().Add(14 * 24 * time.Hour)
		if t.After(maxGrace) {
			return common.BadRequest(c, "validation.grace_too_long",
				"grace_until cannot exceed 14 days from now (prompt-specified maximum)")
		}
		graceUntil = t
	case req.GracePeriodDays > 0:
		if req.GracePeriodDays > 14 {
			return common.BadRequest(c, "validation.invalid_grace_period",
				"grace_period_days must be between 1 and 14 (prompt-specified maximum)")
		}
		graceUntil = time.Now().UTC().Add(time.Duration(req.GracePeriodDays) * 24 * time.Hour)
	case req.GracePeriodDays < 0:
		return common.BadRequest(c, "validation.invalid_grace_period",
			"grace_period_days must be between 1 and 365")
	}

	if err := h.store.SetVersionRule(c.Request().Context(),
		req.MinVersion, req.MaxVersion, req.Action, req.Message, graceUntil); err != nil {
		return common.Internal(c)
	}

	h.recordAudit(c, AuditEvent{
		Action:     "config.version_rule.set",
		Category:   "config",
		TargetType: "client_version_rule",
		TargetID:   req.MinVersion,
		NewValue: map[string]any{
			"min_version":       req.MinVersion,
			"max_version":       req.MaxVersion,
			"action":            req.Action,
			"message":           req.Message,
			"grace_until":       graceUntil,
			"grace_period_days": req.GracePeriodDays,
		},
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
