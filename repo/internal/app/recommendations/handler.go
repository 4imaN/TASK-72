package recommendations

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
	"portal/internal/platform/featureflag"
	"portal/internal/platform/logging"
)

// Handler handles recommendation HTTP endpoints.
type Handler struct {
	store *Store
	log   *logging.Logger
	gate  *featureflag.Gate
}

// NewHandler creates a new recommendations handler.
func NewHandler(store *Store, log *logging.Logger) *Handler {
	return &Handler{store: store, log: log}
}

// NewHandlerWithFlags returns a Handler that gates recommendation delivery
// behind the recommendations.enabled flag. When the flag is off for the
// caller's role set, GetRecommendations returns an empty item list (200 OK)
// so the UI can render a graceful empty state without error handling.
func NewHandlerWithFlags(store *Store, log *logging.Logger, flags featureflag.Checker, roles featureflag.RoleLookup) *Handler {
	return &Handler{store: store, log: log, gate: featureflag.New(flags, roles)}
}

// GetRecommendations handles GET /api/v1/recommendations
//
// Query params:
//   - limit: int, default 10, max 20
//   - include_trace: bool
//
// Response: { items: [{ resource, score, factors: [{factor, weight, label}] }] }
func (h *Handler) GetRecommendations(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	// Phased-rollout gate: recommendations.enabled.
	// When disabled (globally or for this caller's roles), return an empty
	// payload instead of 404/503 so the UI can keep rendering without
	// special-case error handling.
	if h.gate != nil && !h.gate.EnabledFor(c, "recommendations.enabled") {
		return c.JSON(http.StatusOK, map[string]any{"items": []any{}})
	}

	// Parse limit
	limit := 10
	if v := c.QueryParam("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 20 {
		limit = 20
	}

	recs, err := h.store.GetRecommendations(c.Request().Context(), userID, limit)
	if err != nil {
		h.log.Error("get recommendations", map[string]any{"err": err.Error(), "user_id": userID})
		return common.Internal(c)
	}

	// Shape items
	type Item struct {
		ResourceID  string       `json:"resource_id"`
		Title       string       `json:"title"`
		ContentType string       `json:"content_type"`
		Category    string       `json:"category"`
		Score       float64      `json:"score"`
		Factors     []TraceFactor `json:"factors"`
	}

	items := make([]Item, 0, len(recs))
	for _, r := range recs {
		items = append(items, Item{
			ResourceID:  r.ResourceID,
			Title:       r.Title,
			ContentType: r.ContentType,
			Category:    r.Category,
			Score:       r.Score,
			Factors:     r.Factors,
		})
	}

	return c.JSON(http.StatusOK, map[string]any{"items": items})
}

// RecordEvent handles POST /api/v1/recommendations/events
//
// Body: { resource_id, event_type: "view"|"complete"|"click" }
// Returns 204 No Content
func (h *Handler) RecordEvent(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	var req struct {
		ResourceID string `json:"resource_id"`
		EventType  string `json:"event_type"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}

	// Validate event_type
	switch req.EventType {
	case "view", "complete", "click":
		// valid
	default:
		return common.BadRequest(c, "validation.invalid_event_type",
			"event_type must be one of: view, complete, click")
	}

	if req.ResourceID == "" {
		return common.BadRequest(c, "validation.missing_resource_id", "resource_id is required")
	}

	// Use the server-side session ID (injected by RequireAuth) — NEVER the
	// raw cookie token, which is a credential. Persisting the token value
	// into analytics would expand credential exposure beyond the session
	// store and violate the security model.
	sessionID, _ := c.Get("session_id").(string)

	if err := h.store.IngestBehavior(c.Request().Context(), userID, req.ResourceID, req.EventType, sessionID); err != nil {
		h.log.Error("ingest behavior", map[string]any{"err": err.Error(), "user_id": userID})
		// Non-fatal — don't break the user experience
		return c.NoContent(http.StatusNoContent)
	}

	return c.NoContent(http.StatusNoContent)
}
