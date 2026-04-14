package reviews

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"portal/internal/app/audit"
	"portal/internal/app/common"
	"portal/internal/app/users"
	platformstorage "portal/internal/platform/storage"
)

// Handler handles all review, appeal, and moderation HTTP requests.
type Handler struct {
	store     *Store
	userStore *users.Store
	storage   *platformstorage.Store
	audit     audit.Recorder // optional; arbitration/moderation/reply actions are audited when wired
}

// NewHandler constructs a Handler.
func NewHandler(store *Store, userStore *users.Store, storage *platformstorage.Store) *Handler {
	return &Handler{store: store, userStore: userStore, storage: storage}
}

// NewHandlerWithAudit returns a Handler that records merchant replies, appeal
// arbitrations, and moderation decisions into the audit log.
func NewHandlerWithAudit(store *Store, userStore *users.Store, storage *platformstorage.Store, recorder audit.Recorder) *Handler {
	return &Handler{store: store, userStore: userStore, storage: storage, audit: recorder}
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

// ── Review endpoints ──────────────────────────────────────────────────────────

// CreateReview handles POST /api/v1/reviews
func (h *Handler) CreateReview(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	var req struct {
		OrderID     string            `json:"order_id"`
		Rating      int               `json:"rating"`
		Body        string            `json:"body"`
		Attachments []AttachmentInput `json:"attachments"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}

	if req.OrderID == "" {
		return common.BadRequest(c, "validation.required", "order_id is required")
	}
	if req.Rating < 1 || req.Rating > 5 {
		return common.BadRequest(c, "validation.invalid_rating", "Rating must be between 1 and 5")
	}
	if len(req.Body) > 2000 {
		return common.BadRequest(c, "validation.body_too_long", "Review body must be 2000 characters or fewer")
	}
	if len(req.Attachments) > 5 {
		return common.BadRequest(c, "validation.too_many_attachments", "Maximum 5 attachments allowed")
	}

	allowedTypes := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
	}
	for _, a := range req.Attachments {
		if !allowedTypes[a.ContentType] {
			return common.BadRequest(c, "validation.invalid_attachment_type",
				"Attachment content_type must be image/jpeg or image/png")
		}
		if a.Data == "" {
			return common.BadRequest(c, "validation.required", "attachment data is required")
		}
	}

	review, err := h.store.CreateReview(
		c.Request().Context(),
		req.OrderID, userID, req.Rating, req.Body, req.Attachments,
	)
	if err != nil {
		if containsStr(err.Error(), "storage:") {
			return common.BadRequest(c, "validation.invalid_attachment", err.Error())
		}
		return common.Internal(c)
	}

	return c.JSON(http.StatusCreated, review)
}

// GetReview handles GET /api/v1/reviews/:id
func (h *Handler) GetReview(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)

	id := c.Param("id")
	review, err := h.store.GetReview(c.Request().Context(), id)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "reviews.not_found", "Review not found")
	}

	// Resolve caller permissions once (needed for both the hidden gate and scope gate).
	canModerate := false
	canReadOrders := false
	if callerID != "" && h.userStore != nil {
		if uw, err := h.userStore.GetWithRoles(c.Request().Context(), callerID); err == nil {
			for _, p := range uw.Permissions {
				switch p {
				case "moderation:write":
					canModerate = true
				case "orders:read":
					canReadOrders = true
				}
			}
		}
	}

	// Step 1: Hidden reviews — return 404 to avoid enumeration unless caller can moderate.
	if review.Visibility == "hidden" && !canModerate {
		return common.ErrorResponse(c, http.StatusNotFound, "reviews.not_found", "Review not found")
	}

	// Step 2: Business-scope gate — author, orders:read, or moderation:write may read.
	isAuthor := callerID != "" && review.ReviewerID == callerID
	if !isAuthor && !canReadOrders && !canModerate {
		return common.Forbidden(c, "Access denied")
	}

	return c.JSON(http.StatusOK, review)
}

// ListOrderReviews handles GET /api/v1/orders/:order_id/reviews
func (h *Handler) ListOrderReviews(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)

	// Resolve caller permissions — same logic used by GetReview.
	canModerate := false
	canReadOrders := false
	if callerID != "" && h.userStore != nil {
		if uw, err := h.userStore.GetWithRoles(c.Request().Context(), callerID); err == nil {
			for _, p := range uw.Permissions {
				switch p {
				case "moderation:write":
					canModerate = true
				case "orders:read":
					canReadOrders = true
				}
			}
		}
	}

	// Scope gate: only procurement participants (orders:read) or moderators may
	// enumerate reviews for an order. Authors can read their own individual review
	// via GET /reviews/:id but list access requires a broader business role.
	if !canReadOrders && !canModerate {
		return common.Forbidden(c, "Access denied")
	}

	orderID := c.Param("order_id")
	limit := parseIntParam(c.QueryParam("limit"), 20)
	offset := parseIntParam(c.QueryParam("offset"), 0)

	// Hidden reviews are only visible to moderators, matching the single-review gate.
	reviews, total, err := h.store.ListReviews(c.Request().Context(), orderID, limit, offset, canModerate)
	if err != nil {
		return common.Internal(c)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"reviews": reviews,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// AddMerchantReply handles POST /api/v1/reviews/:id/reply
func (h *Handler) AddMerchantReply(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	reviewID := c.Param("id")

	var req struct {
		ReplyText string `json:"reply_text"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.ReplyText == "" {
		return common.BadRequest(c, "validation.required", "reply_text is required")
	}

	if err := h.store.AddMerchantReply(
		c.Request().Context(), reviewID, userID, req.ReplyText,
	); err != nil {
		if err.Error() == "reply already exists" {
			return common.ErrorResponse(c, http.StatusConflict, "reviews.reply_exists", "A reply already exists for this review")
		}
		if containsStr(err.Error(), "review not found") {
			return common.ErrorResponse(c, http.StatusNotFound, "reviews.not_found", "Review not found")
		}
		return common.Internal(c)
	}

	h.recordAudit(c, audit.Event{
		Action:     "reviews.merchant_reply.add",
		Category:   "reviews",
		TargetType: "review",
		TargetID:   reviewID,
		NewValue:   map[string]any{"reply_text": req.ReplyText},
	})

	return c.NoContent(http.StatusNoContent)
}

// FlagReview handles POST /api/v1/reviews/:id/flag
func (h *Handler) FlagReview(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	reviewID := c.Param("id")

	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.Reason == "" {
		return common.BadRequest(c, "validation.required", "reason is required")
	}

	if err := h.store.FlagForModeration(
		c.Request().Context(), reviewID, req.Reason, userID,
	); err != nil {
		return common.Internal(c)
	}

	return c.NoContent(http.StatusNoContent)
}

// ── Appeal endpoints ──────────────────────────────────────────────────────────

// CreateAppeal handles POST /api/v1/appeals
func (h *Handler) CreateAppeal(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	var req struct {
		ReviewID string          `json:"review_id"`
		Reason   string          `json:"reason"`
		Evidence []EvidenceInput `json:"evidence"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.ReviewID == "" {
		return common.BadRequest(c, "validation.required", "review_id is required")
	}
	if req.Reason == "" {
		return common.BadRequest(c, "validation.required", "reason is required")
	}

	allowedTypes := map[string]bool{
		"application/pdf": true,
		"image/jpeg":      true,
		"image/png":       true,
	}
	for _, e := range req.Evidence {
		if !allowedTypes[e.ContentType] {
			return common.BadRequest(c, "validation.invalid_evidence_type",
				"Evidence content_type must be application/pdf, image/jpeg, or image/png")
		}
		if e.Data == "" {
			return common.BadRequest(c, "validation.required", "evidence data is required")
		}
	}

	appeal, err := h.store.CreateAppeal(
		c.Request().Context(),
		req.ReviewID, userID, req.Reason, req.Evidence,
	)
	if err != nil {
		if containsStr(err.Error(), "can only appeal reviews with rating") {
			return common.BadRequest(c, "appeals.rating_too_high",
				"Can only appeal reviews with a rating of 1 or 2")
		}
		if containsStr(err.Error(), "review not found") {
			return common.ErrorResponse(c, http.StatusNotFound, "reviews.not_found", "Review not found")
		}
		if containsStr(err.Error(), "storage:") {
			return common.BadRequest(c, "validation.invalid_evidence", err.Error())
		}
		return common.Internal(c)
	}

	return c.JSON(http.StatusCreated, appeal)
}

// GetAppeal handles GET /api/v1/appeals/:id
// Only the appellant or a user with appeals:decide permission may read an appeal.
func (h *Handler) GetAppeal(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)
	if callerID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	id := c.Param("id")
	appeal, err := h.store.GetAppeal(c.Request().Context(), id)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "appeals.not_found", "Appeal not found")
	}

	// Check caller is the appellant or has appeals:decide permission.
	if appeal.AppealedBy != callerID {
		canDecide := false
		if h.userStore != nil {
			if uw, err := h.userStore.GetWithRoles(c.Request().Context(), callerID); err == nil {
				for _, p := range uw.Permissions {
					if p == "appeals:decide" {
						canDecide = true
						break
					}
				}
			}
		}
		if !canDecide {
			return common.Forbidden(c, "Access denied")
		}
	}

	return c.JSON(http.StatusOK, appeal)
}

// ListAppeals handles GET /api/v1/appeals
// Callers with appeals:decide see all; callers with only appeals:write see their own.
func (h *Handler) ListAppeals(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	// Check whether caller has appeals:decide to show all vs own-only
	canDecide := false
	if h.userStore != nil {
		uw, err := h.userStore.GetWithRoles(c.Request().Context(), userID)
		if err == nil {
			for _, p := range uw.Permissions {
				if p == "appeals:decide" {
					canDecide = true
					break
				}
			}
		}
		// If user has neither appeals:decide nor appeals:write, deny
		hasWrite := false
		if uw != nil {
			for _, p := range uw.Permissions {
				if p == "appeals:write" || p == "appeals:decide" {
					hasWrite = true
					break
				}
			}
		}
		if !hasWrite {
			return common.Forbidden(c, "Insufficient permissions")
		}
	}

	filter := AppealFilter{
		Status:        c.QueryParam("status"),
		VendorOrderID: c.QueryParam("order_id"),
	}
	if !canDecide {
		filter.AppellantID = userID
	}

	limit := parseIntParam(c.QueryParam("limit"), 20)
	offset := parseIntParam(c.QueryParam("offset"), 0)

	appeals, total, err := h.store.ListAppeals(
		c.Request().Context(), filter, limit, offset,
	)
	if err != nil {
		return common.Internal(c)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"appeals": appeals,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// Arbitrate handles POST /api/v1/appeals/:id/arbitrate
func (h *Handler) Arbitrate(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	appealID := c.Param("id")

	var req struct {
		Outcome        string `json:"outcome"`
		Notes          string `json:"notes"`
		DisclaimerText string `json:"disclaimer_text"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.Outcome == "" {
		return common.BadRequest(c, "validation.required", "outcome is required")
	}

	err := h.store.RecordArbitration(
		c.Request().Context(),
		appealID, userID, req.Outcome, req.Notes, req.DisclaimerText,
	)
	if err != nil {
		if containsStr(err.Error(), "invalid outcome") {
			return common.BadRequest(c, "appeals.invalid_outcome",
				"Outcome must be hide, show_with_disclaimer, or restore")
		}
		if containsStr(err.Error(), "appeal not found") {
			return common.ErrorResponse(c, http.StatusNotFound, "appeals.not_found", "Appeal not found")
		}
		return common.Internal(c)
	}

	h.recordAudit(c, audit.Event{
		Action:     "appeals.arbitrate",
		Category:   "disputes",
		TargetType: "appeal",
		TargetID:   appealID,
		NewValue: map[string]any{
			"outcome":         req.Outcome,
			"notes":           req.Notes,
			"disclaimer_text": req.DisclaimerText,
		},
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "decided"})
}

// ── Moderation endpoints ──────────────────────────────────────────────────────

// ListModerationQueue handles GET /api/v1/moderation/queue
func (h *Handler) ListModerationQueue(c echo.Context) error {
	status := c.QueryParam("status")
	limit := parseIntParam(c.QueryParam("limit"), 20)
	offset := parseIntParam(c.QueryParam("offset"), 0)

	items, total, err := h.store.ListQueue(c.Request().Context(), status, limit, offset)
	if err != nil {
		return common.Internal(c)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// DecideModerationItem handles POST /api/v1/moderation/queue/:id/decide
func (h *Handler) DecideModerationItem(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	itemID := c.Param("id")

	var req struct {
		Decision string `json:"decision"`
		Notes    string `json:"notes"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.Decision == "" {
		return common.BadRequest(c, "validation.required", "decision is required")
	}

	if err := h.store.DecideItem(
		c.Request().Context(), itemID, userID, req.Decision, req.Notes,
	); err != nil {
		if containsStr(err.Error(), "invalid decision") {
			return common.BadRequest(c, "moderation.invalid_decision",
				"Decision must be approve, reject, or escalate")
		}
		if containsStr(err.Error(), "not found") {
			return common.ErrorResponse(c, http.StatusNotFound, "moderation.not_found", "Queue item not found")
		}
		return common.Internal(c)
	}

	h.recordAudit(c, audit.Event{
		Action:     "moderation.decide",
		Category:   "moderation",
		TargetType: "moderation_queue_item",
		TargetID:   itemID,
		NewValue:   map[string]any{"decision": req.Decision, "notes": req.Notes},
	})

	return c.NoContent(http.StatusNoContent)
}

// ── Download endpoints ────────────────────────────────────────────────────────

// DownloadAttachment handles GET /api/v1/reviews/attachments/:id
// Requires orders:read OR moderation:write OR caller is the review author.
func (h *Handler) DownloadAttachment(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)
	if callerID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	attachmentID := c.Param("id")
	att, err := h.store.GetAttachmentInternal(c.Request().Context(), attachmentID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "attachments.not_found", "Attachment not found")
	}

	// Resolve the review to check authorship
	review, err := h.store.GetReview(c.Request().Context(), att.ReviewID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "reviews.not_found", "Review not found")
	}

	// Permission check
	canModerate := false
	canReadOrders := false
	if h.userStore != nil {
		if uw, err := h.userStore.GetWithRoles(c.Request().Context(), callerID); err == nil {
			for _, p := range uw.Permissions {
				switch p {
				case "moderation:write":
					canModerate = true
				case "orders:read":
					canReadOrders = true
				}
			}
		}
	}
	isAuthor := review.ReviewerID == callerID
	if !isAuthor && !canReadOrders && !canModerate {
		return common.Forbidden(c, "Access denied")
	}

	if att.FilePath == "" {
		return common.ErrorResponse(c, http.StatusNotFound, "attachments.no_file", "No file stored for this attachment")
	}

	f, err := h.storage.Open(att.FilePath)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "attachments.not_found", "File not found on disk")
	}
	defer f.Close()

	c.Response().Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"`, att.OriginalName))
	return c.Stream(http.StatusOK, att.ContentType, f)
}

// DownloadEvidence handles GET /api/v1/appeals/evidence/:id
// Requires caller is the appellant OR has appeals:decide.
func (h *Handler) DownloadEvidence(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)
	if callerID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	evidenceID := c.Param("id")
	// Use the internal reader so the download path has the server-side
	// file_path — the API-safe Evidence DTO deliberately strips it.
	ev, err := h.store.GetEvidenceInternal(c.Request().Context(), evidenceID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "evidence.not_found", "Evidence not found")
	}

	// Resolve the appeal to check appellant identity
	appeal, err := h.store.GetAppeal(c.Request().Context(), ev.AppealID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "appeals.not_found", "Appeal not found")
	}

	// Permission check
	canDecide := false
	if h.userStore != nil {
		if uw, err := h.userStore.GetWithRoles(c.Request().Context(), callerID); err == nil {
			for _, p := range uw.Permissions {
				if p == "appeals:decide" {
					canDecide = true
					break
				}
			}
		}
	}
	isAppellant := appeal.AppealedBy == callerID
	if !isAppellant && !canDecide {
		return common.Forbidden(c, "Access denied")
	}

	if ev.FilePath == "" {
		return common.ErrorResponse(c, http.StatusNotFound, "evidence.no_file", "No file stored for this evidence")
	}

	f, err := h.storage.Open(ev.FilePath)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "evidence.not_found", "File not found on disk")
	}
	defer f.Close()

	c.Response().Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"`, ev.OriginalName))
	return c.Stream(http.StatusOK, ev.ContentType, f)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func parseIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return def
	}
	return v
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
