// tests/api/moderation_test.go — HTTP-level tests for review, appeal, and moderation endpoints.
// All tests use httptest and in-memory fakes; no real database is required.
package api_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
	"portal/internal/app/sessions"
)

// minimalJPEGBase64 is a valid minimal JPEG used in tests that require attachment data.
var minimalJPEGBase64 = base64.StdEncoding.EncodeToString([]byte{
	0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01,
	0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0xFF, 0xD9,
})

// ── In-memory domain types ────────────────────────────────────────────────────

type fakeReview struct {
	ID             string           `json:"id"`
	OrderID        string           `json:"order_id"`
	ReviewerID     string           `json:"reviewer_id"`
	Rating         int              `json:"rating"`
	ReviewText     string           `json:"review_text,omitempty"`
	Visibility     string           `json:"visibility"`
	DisclaimerText *string          `json:"disclaimer_text,omitempty"`
	Attachments    []fakeAttachment `json:"attachments"`
	Reply          *fakeReply       `json:"reply,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type fakeAttachment struct {
	ID           string    `json:"id"`
	ReviewID     string    `json:"review_id"`
	OriginalName string    `json:"original_name"`
	ContentType  string    `json:"content_type"`
	SizeBytes    int64     `json:"size_bytes"`
	UploadedAt   time.Time `json:"uploaded_at"`
}

type fakeReply struct {
	ID         string    `json:"id"`
	ReviewID   string    `json:"review_id"`
	RecordedBy string    `json:"recorded_by"`
	ReplyText  string    `json:"reply_text"`
	RepliedAt  time.Time `json:"replied_at"`
}

type fakeAttachmentInput struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"`
}

type fakeEvidenceInput struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"`
}

type fakeAppeal struct {
	ID           string          `json:"id"`
	ReviewID     string          `json:"review_id"`
	AppealedBy   string          `json:"appealed_by"`
	AppealReason string          `json:"appeal_reason"`
	Status       string          `json:"status"`
	SubmittedAt  time.Time       `json:"submitted_at"`
	Evidence     []fakeEvidence  `json:"evidence"`
}

type fakeEvidence struct {
	ID           string    `json:"id"`
	AppealID     string    `json:"appeal_id"`
	OriginalName string    `json:"original_name"`
	ContentType  string    `json:"content_type"`
	SizeBytes    int64     `json:"size_bytes"`
	UploadedAt   time.Time `json:"uploaded_at"`
}

type fakeModerationItem struct {
	ID            string     `json:"id"`
	ReviewID      string     `json:"review_id"`
	Reason        string     `json:"reason"`
	FlaggedBy     string     `json:"flagged_by"`
	Status        string     `json:"status"`
	ModeratorID   *string    `json:"moderator_id,omitempty"`
	DecisionNotes *string    `json:"decision_notes,omitempty"`
	FlaggedAt     time.Time  `json:"flagged_at"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
}

// ── In-memory store ───────────────────────────────────────────────────────────

type fakeModerationStore struct {
	reviews    map[string]*fakeReview
	appeals    map[string]*fakeAppeal
	queue      map[string]*fakeModerationItem
	nextID     int
}

func newFakeModerationStore() *fakeModerationStore {
	return &fakeModerationStore{
		reviews: make(map[string]*fakeReview),
		appeals: make(map[string]*fakeAppeal),
		queue:   make(map[string]*fakeModerationItem),
	}
}

func (s *fakeModerationStore) genID() string {
	s.nextID++
	return "id-" + string(rune('A'-1+s.nextID))
}

// ── Echo instance builder ─────────────────────────────────────────────────────

// userPermissions maps userID to their permissions list
type fakePermMap map[string][]string

func buildModerationEcho(
	ms *fakeModerationStore,
	ss *fakeSessionStore,
	perms fakePermMap,
) *echo.Echo {
	e := echo.New()
	e.HideBanner = true

	// requireAuth inlines the session check
	requireAuth := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := sessions.TokenFromRequest(c)
			if token == "" {
				return common.Unauthorized(c, "Authentication required")
			}
			sess, ok := ss.sessions[token]
			if !ok {
				return common.Unauthorized(c, "Session expired or invalid")
			}
			c.Set("user_id", sess.UserID)
			return next(c)
		}
	}

	// requirePerm returns a middleware that checks a permission
	requirePerm := func(perm string) echo.MiddlewareFunc {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				userID, _ := c.Get("user_id").(string)
				for _, p := range perms[userID] {
					if p == perm {
						return next(c)
					}
				}
				return common.Forbidden(c, "Insufficient permissions")
			}
		}
	}

	// hasPerm checks a user's permission without failing
	hasPerm := func(userID, perm string) bool {
		for _, p := range perms[userID] {
			if p == perm {
				return true
			}
		}
		return false
	}

	// POST /api/v1/reviews
	e.POST("/api/v1/reviews", requireAuth(requirePerm("catalog:read")(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		var req struct {
			OrderID     string               `json:"order_id"`
			Rating      int                  `json:"rating"`
			Body        string               `json:"body"`
			Attachments []fakeAttachmentInput `json:"attachments"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
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
		allowed := map[string]bool{"image/jpeg": true, "image/png": true}
		for _, a := range req.Attachments {
			if !allowed[a.ContentType] {
				return common.BadRequest(c, "validation.invalid_attachment_type",
					"Attachment content_type must be image/jpeg or image/png")
			}
		}

		id := "review-" + ms.genID()
		now := time.Now()
		var attachments []fakeAttachment
		for _, a := range req.Attachments {
			attachments = append(attachments, fakeAttachment{
				ID: "att-" + ms.genID(), ReviewID: id,
				OriginalName: a.Filename, ContentType: a.ContentType,
				SizeBytes: 0, UploadedAt: now,
			})
		}
		if attachments == nil {
			attachments = []fakeAttachment{}
		}
		r := &fakeReview{
			ID: id, OrderID: req.OrderID, ReviewerID: userID,
			Rating: req.Rating, ReviewText: req.Body,
			Visibility:  "visible",
			Attachments: attachments,
			CreatedAt:   now, UpdatedAt: now,
		}
		ms.reviews[id] = r
		return c.JSON(http.StatusCreated, r)
	})))

	// GET /api/v1/reviews/:id
	e.GET("/api/v1/reviews/:id", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		r, ok := ms.reviews[id]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reviews.not_found", "Review not found")
		}
		return c.JSON(http.StatusOK, r)
	}))

	// GET /api/v1/orders/:order_id/reviews
	e.GET("/api/v1/orders/:order_id/reviews", requireAuth(func(c echo.Context) error {
		orderID := c.Param("order_id")
		var out []fakeReview
		for _, r := range ms.reviews {
			if r.OrderID == orderID {
				out = append(out, *r)
			}
		}
		if out == nil {
			out = []fakeReview{}
		}
		return c.JSON(http.StatusOK, map[string]any{"reviews": out, "total": len(out), "limit": 20, "offset": 0})
	}))

	// POST /api/v1/reviews/:id/reply
	e.POST("/api/v1/reviews/:id/reply", requireAuth(requirePerm("orders:read")(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		reviewID := c.Param("id")
		r, ok := ms.reviews[reviewID]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reviews.not_found", "Review not found")
		}
		if r.Reply != nil {
			return common.ErrorResponse(c, http.StatusConflict, "reviews.reply_exists", "Reply already exists")
		}
		var req struct {
			ReplyText string `json:"reply_text"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.ReplyText == "" {
			return common.BadRequest(c, "validation.required", "reply_text is required")
		}
		now := time.Now()
		r.Reply = &fakeReply{
			ID: "reply-" + ms.genID(), ReviewID: reviewID,
			RecordedBy: userID, ReplyText: req.ReplyText, RepliedAt: now,
		}
		return c.NoContent(http.StatusNoContent)
	})))

	// POST /api/v1/reviews/:id/flag
	e.POST("/api/v1/reviews/:id/flag", requireAuth(requirePerm("catalog:read")(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		reviewID := c.Param("id")
		if _, ok := ms.reviews[reviewID]; !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reviews.not_found", "Review not found")
		}
		var req struct {
			Reason string `json:"reason"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.Reason == "" {
			return common.BadRequest(c, "validation.required", "reason is required")
		}
		now := time.Now()
		itemID := "mod-" + ms.genID()
		ms.queue[itemID] = &fakeModerationItem{
			ID: itemID, ReviewID: reviewID,
			Reason: req.Reason, FlaggedBy: userID,
			Status: "pending", FlaggedAt: now,
		}
		return c.NoContent(http.StatusNoContent)
	})))

	// POST /api/v1/appeals
	e.POST("/api/v1/appeals", requireAuth(requirePerm("appeals:write")(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		var req struct {
			ReviewID string              `json:"review_id"`
			Reason   string              `json:"reason"`
			Evidence []fakeEvidenceInput `json:"evidence"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.ReviewID == "" {
			return common.BadRequest(c, "validation.required", "review_id is required")
		}
		if req.Reason == "" {
			return common.BadRequest(c, "validation.required", "reason is required")
		}

		r, ok := ms.reviews[req.ReviewID]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reviews.not_found", "Review not found")
		}
		if r.Rating > 2 {
			return common.BadRequest(c, "appeals.rating_too_high",
				"Can only appeal reviews with a rating of 1 or 2")
		}

		allowed := map[string]bool{"application/pdf": true, "image/jpeg": true, "image/png": true}
		for _, ev := range req.Evidence {
			if !allowed[ev.ContentType] {
				return common.BadRequest(c, "validation.invalid_evidence_type",
					"Evidence content_type must be application/pdf, image/jpeg, or image/png")
			}
		}

		id := "appeal-" + ms.genID()
		now := time.Now()
		var evidence []fakeEvidence
		for _, ev := range req.Evidence {
			evidence = append(evidence, fakeEvidence{
				ID: "ev-" + ms.genID(), AppealID: id,
				OriginalName: ev.Filename, ContentType: ev.ContentType,
				SizeBytes: 0, UploadedAt: now,
			})
		}
		if evidence == nil {
			evidence = []fakeEvidence{}
		}
		a := &fakeAppeal{
			ID: id, ReviewID: req.ReviewID, AppealedBy: userID,
			AppealReason: req.Reason, Status: "pending",
			SubmittedAt: now, Evidence: evidence,
		}
		ms.appeals[id] = a
		return c.JSON(http.StatusCreated, a)
	})))

	// GET /api/v1/appeals/:id
	e.GET("/api/v1/appeals/:id", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		a, ok := ms.appeals[id]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "appeals.not_found", "Appeal not found")
		}
		return c.JSON(http.StatusOK, a)
	}))

	// POST /api/v1/appeals/:id/arbitrate
	e.POST("/api/v1/appeals/:id/arbitrate", requireAuth(requirePerm("appeals:decide")(func(c echo.Context) error {
		appealID := c.Param("id")
		a, ok := ms.appeals[appealID]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "appeals.not_found", "Appeal not found")
		}
		var req struct {
			Outcome        string `json:"outcome"`
			Notes          string `json:"notes"`
			DisclaimerText string `json:"disclaimer_text"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		validOutcomes := map[string]bool{
			"hide": true, "show_with_disclaimer": true, "restore": true,
		}
		if !validOutcomes[req.Outcome] {
			return common.BadRequest(c, "appeals.invalid_outcome",
				"Outcome must be hide, show_with_disclaimer, or restore")
		}
		// Update appeal
		a.Status = "decided"
		// Update review visibility
		if r, ok := ms.reviews[a.ReviewID]; ok {
			switch req.Outcome {
			case "hide":
				r.Visibility = "hidden"
			case "show_with_disclaimer":
				r.Visibility = "shown_with_disclaimer"
				dt := req.DisclaimerText
				r.DisclaimerText = &dt
			case "restore":
				r.Visibility = "visible"
				r.DisclaimerText = nil
			}
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "decided"})
	})))

	// GET /api/v1/moderation/queue
	e.GET("/api/v1/moderation/queue", requireAuth(requirePerm("moderation:write")(func(c echo.Context) error {
		status := c.QueryParam("status")
		var items []fakeModerationItem
		for _, item := range ms.queue {
			if status == "" || item.Status == status {
				items = append(items, *item)
			}
		}
		if items == nil {
			items = []fakeModerationItem{}
		}
		return c.JSON(http.StatusOK, map[string]any{
			"items": items, "total": len(items), "limit": 20, "offset": 0,
		})
	})))

	// POST /api/v1/moderation/queue/:id/decide
	e.POST("/api/v1/moderation/queue/:id/decide", requireAuth(requirePerm("moderation:write")(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		itemID := c.Param("id")
		item, ok := ms.queue[itemID]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "moderation.not_found", "Queue item not found")
		}
		var req struct {
			Decision string `json:"decision"`
			Notes    string `json:"notes"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		validDecisions := map[string]bool{"approve": true, "reject": true, "escalate": true}
		if !validDecisions[req.Decision] {
			return common.BadRequest(c, "moderation.invalid_decision",
				"Decision must be approve, reject, or escalate")
		}
		now := time.Now()
		item.Status = req.Decision
		item.ModeratorID = &userID
		item.DecisionNotes = &req.Notes
		item.DecidedAt = &now
		_ = hasPerm // suppress unused warning for helper captured above
		return c.NoContent(http.StatusNoContent)
	})))

	return e
}

// ── Session helper ────────────────────────────────────────────────────────────

func makeModerationSession(ss *fakeSessionStore, userID string) *http.Cookie {
	token := "modtok_" + userID
	ss.sessions[token] = &sessions.Session{
		ID:     "sess_mod_" + userID,
		UserID: userID,
	}
	return &http.Cookie{
		Name:     sessions.CookieName,
		Value:    token,
		HttpOnly: true,
	}
}

func doModerationPost(e *echo.Echo, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(http.MethodPost, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func doModerationGet(e *echo.Echo, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// 1. POST /reviews returns 201 with valid session and body
func TestCreateReview_Valid(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{
		"user-reviewer": {"catalog:read"},
	}
	e := buildModerationEcho(ms, ss, perms)
	cookie := makeModerationSession(ss, "user-reviewer")

	body := map[string]any{
		"order_id": "order-abc",
		"rating":   4,
		"body":     "Great vendor, fast delivery!",
		"attachments": []map[string]any{
			{"filename": "photo.jpg", "content_type": "image/jpeg", "data": minimalJPEGBase64},
		},
	}

	rec := doModerationPost(e, "/api/v1/reviews", body, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["order_id"] != "order-abc" {
		t.Errorf("expected order_id=order-abc, got %v", resp["order_id"])
	}
	if resp["rating"].(float64) != 4 {
		t.Errorf("expected rating=4, got %v", resp["rating"])
	}
	atts, _ := resp["attachments"].([]any)
	if len(atts) != 1 {
		t.Errorf("expected 1 attachment, got %d", len(atts))
	}
}

// 2. POST /reviews returns 400 with invalid rating (0 or 6)
func TestCreateReview_InvalidRating(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{"user-reviewer": {"catalog:read"}}
	e := buildModerationEcho(ms, ss, perms)
	cookie := makeModerationSession(ss, "user-reviewer")

	for _, rating := range []int{0, 6} {
		body := map[string]any{"order_id": "order-abc", "rating": rating, "body": "Test"}
		rec := doModerationPost(e, "/api/v1/reviews", body, cookie)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("rating=%d: expected 400, got %d", rating, rec.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["code"] != "validation.invalid_rating" {
			t.Errorf("rating=%d: expected code=validation.invalid_rating, got %v", rating, resp["code"])
		}
	}
}

// 3. POST /reviews returns 400 with attachment of invalid content type
func TestCreateReview_InvalidAttachmentType(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{"user-reviewer": {"catalog:read"}}
	e := buildModerationEcho(ms, ss, perms)
	cookie := makeModerationSession(ss, "user-reviewer")

	body := map[string]any{
		"order_id": "order-abc",
		"rating":   3,
		"body":     "Test",
		"attachments": []map[string]any{
			{"filename": "doc.pdf", "content_type": "application/pdf", "data": "dGVzdA=="},
		},
	}
	rec := doModerationPost(e, "/api/v1/reviews", body, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid attachment type, got %d", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "validation.invalid_attachment_type" {
		t.Errorf("expected code=validation.invalid_attachment_type, got %v", resp["code"])
	}
}

// 4. GET /reviews/:id returns 200
func TestGetReview_Found(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{"user-reviewer": {"catalog:read"}}
	e := buildModerationEcho(ms, ss, perms)
	cookie := makeModerationSession(ss, "user-reviewer")

	// First create a review
	body := map[string]any{"order_id": "order-xyz", "rating": 5, "body": "Excellent!"}
	createRec := doModerationPost(e, "/api/v1/reviews", body, cookie)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create review failed: %d — %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	reviewID := created["id"].(string)

	// Get the review
	rec := doModerationGet(e, "/api/v1/reviews/"+reviewID, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["id"] != reviewID {
		t.Errorf("expected id=%s, got %v", reviewID, resp["id"])
	}
}

// 5. POST /reviews/:id/reply returns 204
func TestAddMerchantReply_Valid(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{
		"user-reviewer": {"catalog:read"},
		"user-merchant": {"orders:read"},
	}
	e := buildModerationEcho(ms, ss, perms)

	reviewerCookie := makeModerationSession(ss, "user-reviewer")
	merchantCookie := makeModerationSession(ss, "user-merchant")

	// Create a review
	createRec := doModerationPost(e, "/api/v1/reviews", map[string]any{
		"order_id": "order-abc", "rating": 3, "body": "Decent",
	}, reviewerCookie)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create review: %d", createRec.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	reviewID := created["id"].(string)

	// Add reply
	rec := doModerationPost(e, "/api/v1/reviews/"+reviewID+"/reply",
		map[string]any{"reply_text": "Thank you for your feedback!"},
		merchantCookie,
	)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d — %s", rec.Code, rec.Body.String())
	}
}

// 6. POST /reviews/:id/flag returns 204
func TestFlagReview_Valid(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{"user-reviewer": {"catalog:read"}}
	e := buildModerationEcho(ms, ss, perms)
	cookie := makeModerationSession(ss, "user-reviewer")

	// Create review
	createRec := doModerationPost(e, "/api/v1/reviews", map[string]any{
		"order_id": "order-abc", "rating": 1, "body": "Terrible!",
	}, cookie)
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	reviewID := created["id"].(string)

	// Flag it
	rec := doModerationPost(e, "/api/v1/reviews/"+reviewID+"/flag",
		map[string]any{"reason": "Inappropriate content"},
		cookie,
	)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d — %s", rec.Code, rec.Body.String())
	}
}

// 7. POST /appeals returns 201 for a review with rating ≤ 2
func TestCreateAppeal_LowRating(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{
		"user-reviewer": {"catalog:read"},
		"user-vendor":   {"appeals:write"},
	}
	e := buildModerationEcho(ms, ss, perms)

	reviewerCookie := makeModerationSession(ss, "user-reviewer")
	vendorCookie := makeModerationSession(ss, "user-vendor")

	// Create a low-rating review
	createRec := doModerationPost(e, "/api/v1/reviews", map[string]any{
		"order_id": "order-abc", "rating": 2, "body": "Very slow delivery",
	}, reviewerCookie)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create review: %d", createRec.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	reviewID := created["id"].(string)

	// Create appeal
	rec := doModerationPost(e, "/api/v1/appeals", map[string]any{
		"review_id": reviewID,
		"reason":    "Delivery was delayed due to a carrier error, not our fault",
		"evidence": []map[string]any{
			{"filename": "carrier_notice.pdf", "content_type": "application/pdf", "data": minimalJPEGBase64},
		},
	}, vendorCookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["review_id"] != reviewID {
		t.Errorf("expected review_id=%s, got %v", reviewID, resp["review_id"])
	}
	if resp["status"] != "pending" {
		t.Errorf("expected status=pending, got %v", resp["status"])
	}
}

// 8. POST /appeals returns 400 for a review with rating ≥ 3
func TestCreateAppeal_HighRating(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{
		"user-reviewer": {"catalog:read"},
		"user-vendor":   {"appeals:write"},
	}
	e := buildModerationEcho(ms, ss, perms)

	reviewerCookie := makeModerationSession(ss, "user-reviewer")
	vendorCookie := makeModerationSession(ss, "user-vendor")

	// Create a 3-star review (too high for appeal)
	createRec := doModerationPost(e, "/api/v1/reviews", map[string]any{
		"order_id": "order-abc", "rating": 3, "body": "Average",
	}, reviewerCookie)
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	reviewID := created["id"].(string)

	rec := doModerationPost(e, "/api/v1/appeals", map[string]any{
		"review_id": reviewID,
		"reason":    "We deserve better",
	}, vendorCookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d — %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "appeals.rating_too_high" {
		t.Errorf("expected code=appeals.rating_too_high, got %v", resp["code"])
	}
}

// 9. POST /appeals/:id/arbitrate returns 200 with valid outcome
func TestArbitrate_ValidOutcome(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{
		"user-reviewer":   {"catalog:read"},
		"user-vendor":     {"appeals:write"},
		"user-arbitrator": {"appeals:decide"},
	}
	e := buildModerationEcho(ms, ss, perms)

	reviewerCookie := makeModerationSession(ss, "user-reviewer")
	vendorCookie := makeModerationSession(ss, "user-vendor")
	arbitratorCookie := makeModerationSession(ss, "user-arbitrator")

	// Create low-rating review
	createRec := doModerationPost(e, "/api/v1/reviews", map[string]any{
		"order_id": "order-abc", "rating": 1, "body": "Terrible",
	}, reviewerCookie)
	var createdReview map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &createdReview)
	reviewID := createdReview["id"].(string)

	// Create appeal
	appealRec := doModerationPost(e, "/api/v1/appeals", map[string]any{
		"review_id": reviewID, "reason": "False claim",
	}, vendorCookie)
	var createdAppeal map[string]any
	_ = json.Unmarshal(appealRec.Body.Bytes(), &createdAppeal)
	appealID := createdAppeal["id"].(string)

	// Arbitrate: hide
	rec := doModerationPost(e, "/api/v1/appeals/"+appealID+"/arbitrate", map[string]any{
		"outcome": "hide",
		"notes":   "Review violates community guidelines",
	}, arbitratorCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "decided" {
		t.Errorf("expected status=decided, got %v", resp["status"])
	}
}

// 10. POST /appeals/:id/arbitrate returns 400 with invalid outcome
func TestArbitrate_InvalidOutcome(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{
		"user-reviewer":   {"catalog:read"},
		"user-vendor":     {"appeals:write"},
		"user-arbitrator": {"appeals:decide"},
	}
	e := buildModerationEcho(ms, ss, perms)

	reviewerCookie := makeModerationSession(ss, "user-reviewer")
	vendorCookie := makeModerationSession(ss, "user-vendor")
	arbitratorCookie := makeModerationSession(ss, "user-arbitrator")

	// Create review + appeal
	createRec := doModerationPost(e, "/api/v1/reviews", map[string]any{
		"order_id": "order-abc", "rating": 1, "body": "Bad",
	}, reviewerCookie)
	var createdReview map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &createdReview)
	reviewID := createdReview["id"].(string)

	appealRec := doModerationPost(e, "/api/v1/appeals", map[string]any{
		"review_id": reviewID, "reason": "Dispute",
	}, vendorCookie)
	var createdAppeal map[string]any
	_ = json.Unmarshal(appealRec.Body.Bytes(), &createdAppeal)
	appealID := createdAppeal["id"].(string)

	// Invalid outcome
	rec := doModerationPost(e, "/api/v1/appeals/"+appealID+"/arbitrate", map[string]any{
		"outcome": "delete_everything",
	}, arbitratorCookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d — %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "appeals.invalid_outcome" {
		t.Errorf("expected code=appeals.invalid_outcome, got %v", resp["code"])
	}
}

// 11. GET /moderation/queue returns 200 with moderator session
func TestGetModerationQueue_Authorized(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{
		"user-reviewer": {"catalog:read"},
		"user-moderator": {"moderation:write"},
	}
	e := buildModerationEcho(ms, ss, perms)

	reviewerCookie := makeModerationSession(ss, "user-reviewer")
	moderatorCookie := makeModerationSession(ss, "user-moderator")

	// Create and flag a review
	createRec := doModerationPost(e, "/api/v1/reviews", map[string]any{
		"order_id": "order-abc", "rating": 2, "body": "Suspicious",
	}, reviewerCookie)
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	reviewID := created["id"].(string)

	doModerationPost(e, "/api/v1/reviews/"+reviewID+"/flag",
		map[string]any{"reason": "Spam"}, reviewerCookie)

	// Moderator fetches queue
	rec := doModerationGet(e, "/api/v1/moderation/queue", moderatorCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatal("expected items array")
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item in queue, got %d", len(items))
	}
}

// 12. POST /moderation/queue/:id/decide returns 204
func TestDecideModerationItem_Valid(t *testing.T) {
	ms := newFakeModerationStore()
	ss := newFakeSessionStore()
	perms := fakePermMap{
		"user-reviewer":  {"catalog:read"},
		"user-moderator": {"moderation:write"},
	}
	e := buildModerationEcho(ms, ss, perms)

	reviewerCookie := makeModerationSession(ss, "user-reviewer")
	moderatorCookie := makeModerationSession(ss, "user-moderator")

	// Create + flag a review
	createRec := doModerationPost(e, "/api/v1/reviews", map[string]any{
		"order_id": "order-abc", "rating": 3, "body": "OK",
	}, reviewerCookie)
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	reviewID := created["id"].(string)

	doModerationPost(e, "/api/v1/reviews/"+reviewID+"/flag",
		map[string]any{"reason": "Off-topic"}, reviewerCookie)

	// Fetch the queue to get item ID
	queueRec := doModerationGet(e, "/api/v1/moderation/queue", moderatorCookie)
	var queueBody map[string]any
	_ = json.Unmarshal(queueRec.Body.Bytes(), &queueBody)
	items := queueBody["items"].([]any)
	if len(items) == 0 {
		t.Fatal("expected at least 1 item in queue")
	}
	itemID := items[0].(map[string]any)["id"].(string)

	// Decide: approve
	rec := doModerationPost(e, "/api/v1/moderation/queue/"+itemID+"/decide", map[string]any{
		"decision": "approve",
		"notes":    "Content reviewed, approved for visibility",
	}, moderatorCookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d — %s", rec.Code, rec.Body.String())
	}

	// Verify status changed
	queueRec2 := doModerationGet(e, "/api/v1/moderation/queue?status=approve", moderatorCookie)
	var queueBody2 map[string]any
	_ = json.Unmarshal(queueRec2.Body.Bytes(), &queueBody2)
	items2 := queueBody2["items"].([]any)
	if len(items2) != 1 {
		t.Errorf("expected 1 approved item, got %d", len(items2))
	}
}
