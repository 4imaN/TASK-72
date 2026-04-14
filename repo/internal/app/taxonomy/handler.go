package taxonomy

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"portal/internal/app/audit"
	"portal/internal/app/common"
)

type Handler struct {
	store *Store
	audit audit.Recorder // optional; synonym + conflict-resolve mutations are audited when wired
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// NewHandlerWithAudit wires audit recording for taxonomy mutations.
func NewHandlerWithAudit(store *Store, recorder audit.Recorder) *Handler {
	return &Handler{store: store, audit: recorder}
}

func (h *Handler) recordAudit(c echo.Context, evt audit.Event) {
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

func (h *Handler) ListTags(c echo.Context) error {
	tags, err := h.store.ListTags(c.Request().Context())
	if err != nil {
		return common.Internal(c)
	}
	if tags == nil {
		tags = []Tag{}
	}
	return c.JSON(http.StatusOK, map[string]any{"tags": tags})
}

func (h *Handler) GetTag(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return common.BadRequest(c, "validation.invalid_id", "Invalid tag ID")
	}
	tag, err := h.store.GetTag(c.Request().Context(), id)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "taxonomy.not_found", "Tag not found")
	}
	return c.JSON(http.StatusOK, tag)
}

func (h *Handler) AddSynonym(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return common.BadRequest(c, "validation.invalid_id", "Invalid tag ID")
	}

	var req struct {
		Text string `json:"text"`
		Type string `json:"type"`
	}
	if err := c.Bind(&req); err != nil || req.Text == "" {
		return common.BadRequest(c, "validation.required", "synonym text is required")
	}
	if req.Type == "" {
		req.Type = "alias"
	}

	userID, _ := c.Get("user_id").(string)
	if err := h.store.AddSynonym(c.Request().Context(), id, req.Text, req.Type, userID); err != nil {
		return common.BadRequest(c, "taxonomy.conflict", err.Error())
	}
	h.recordAudit(c, audit.Event{
		Action:     "taxonomy.synonym.add",
		Category:   "taxonomy",
		TargetType: "skill_tag",
		TargetID:   strconv.FormatInt(id, 10),
		NewValue:   map[string]any{"synonym_text": req.Text, "synonym_type": req.Type},
	})
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListConflicts(c echo.Context) error {
	conflicts, err := h.store.ListConflicts(c.Request().Context())
	if err != nil {
		return common.Internal(c)
	}
	if conflicts == nil {
		conflicts = []map[string]any{}
	}
	return c.JSON(http.StatusOK, map[string]any{"conflicts": conflicts})
}

// ResolveConflict closes an open tag_conflicts row.
// POST /api/v1/taxonomy/conflicts/:id/resolve
//
// Body:
//
//	{ "resolution": "deactivated_a" | "deactivated_b" | "merged" }
//
// Requires taxonomy:write (held by admin and content moderator per the seeded
// role matrix). The matching synonym row (when applicable) is deactivated and
// the taxonomy_review_queue entry is marked reviewed.
func (h *Handler) ResolveConflict(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return common.BadRequest(c, "validation.invalid_id", "Invalid conflict ID")
	}

	var req struct {
		Resolution string `json:"resolution"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.Resolution == "" {
		return common.BadRequest(c, "validation.required", "resolution is required")
	}

	userID, _ := c.Get("user_id").(string)
	if err := h.store.ResolveConflict(c.Request().Context(), id, userID, req.Resolution); err != nil {
		return common.BadRequest(c, "taxonomy.resolve_failed", err.Error())
	}
	h.recordAudit(c, audit.Event{
		Action:     "taxonomy.conflict.resolve",
		Category:   "taxonomy",
		TargetType: "tag_conflict",
		TargetID:   strconv.FormatInt(id, 10),
		NewValue:   map[string]any{"resolution": req.Resolution},
	})
	return c.JSON(http.StatusOK, map[string]any{
		"status":     "resolved",
		"id":         id,
		"resolution": req.Resolution,
	})
}
