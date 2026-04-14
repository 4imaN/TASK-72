package audit

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
)

// Handler exposes admin audit log endpoints.
type Handler struct {
	store *Store
}

// NewHandler constructs an audit Handler.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// ListEvents handles GET /api/v1/admin/audit
// Query params: user_id, action, limit, offset
// Requires admin role.
func (h *Handler) ListEvents(c echo.Context) error {
	userID := c.QueryParam("user_id")
	action := c.QueryParam("action")
	limit := parseAuditIntParam(c.QueryParam("limit"), 50)
	offset := parseAuditIntParam(c.QueryParam("offset"), 0)

	events, total, err := h.store.ListEvents(c.Request().Context(), userID, action, limit, offset)
	if err != nil {
		return common.Internal(c)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"events": events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func parseAuditIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return def
	}
	return v
}
