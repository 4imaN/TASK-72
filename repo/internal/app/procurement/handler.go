package procurement

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"portal/internal/app/audit"
	"portal/internal/app/common"
)

// Handler handles procurement HTTP requests.
type Handler struct {
	store *Store
	audit audit.Recorder // optional; approve/reject mutations are audited when wired
}

// NewHandler constructs a Handler.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// NewHandlerWithAudit returns a Handler that records approve/reject decisions
// into the audit log. Pass nil to disable audit recording.
func NewHandlerWithAudit(store *Store, recorder audit.Recorder) *Handler {
	return &Handler{store: store, audit: recorder}
}

// recordAudit is a fire-and-forget helper that fills in actor + IP from the
// Echo context. No-op when no recorder is wired.
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

// ListOrders handles GET /api/v1/procurement/orders
// Query params: status, requested_by, limit, offset
func (h *Handler) ListOrders(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)
	if callerID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	filter := OrderFilter{
		Status:      c.QueryParam("status"),
		RequestedBy: c.QueryParam("requested_by"),
	}
	// "me" alias: resolve to caller's own ID
	if filter.RequestedBy == "me" {
		filter.RequestedBy = callerID
	}

	limit := parseIntParam(c.QueryParam("limit"), 20)
	offset := parseIntParam(c.QueryParam("offset"), 0)

	orders, total, err := h.store.ListOrders(c.Request().Context(), filter, limit, offset)
	if err != nil {
		return common.Internal(c)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"orders": orders,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// CreateOrder handles POST /api/v1/procurement/orders
// Body: { vendor_name, description, total_amount }
func (h *Handler) CreateOrder(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)
	if callerID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	var req struct {
		VendorName  string  `json:"vendor_name"`
		Description string  `json:"description"`
		TotalAmount float64 `json:"total_amount"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.VendorName == "" {
		return common.BadRequest(c, "validation.required", "vendor_name is required")
	}

	order, err := h.store.CreateOrder(c.Request().Context(), req.VendorName, req.Description, callerID, req.TotalAmount)
	if err != nil {
		return common.Internal(c)
	}

	return c.JSON(http.StatusCreated, order)
}

// GetOrder handles GET /api/v1/procurement/orders/:id
func (h *Handler) GetOrder(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)
	if callerID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	orderID := c.Param("id")
	order, err := h.store.GetOrder(c.Request().Context(), orderID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "procurement.not_found", "Order not found")
	}

	return c.JSON(http.StatusOK, order)
}

// ApproveOrder handles POST /api/v1/procurement/orders/:id/approve.
//
// Routed under orders:approve (held by the approver role per the seed matrix).
// Procurement specialists who created the order cannot self-approve — that
// check happens here regardless of the route permission, in case orders:approve
// is later granted to a role that can also create orders.
func (h *Handler) ApproveOrder(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)
	if callerID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	orderID := c.Param("id")

	// Self-approval guard: load the order and reject if the caller is the creator.
	order, err := h.store.GetOrder(c.Request().Context(), orderID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "procurement.not_found", "Order not found")
	}
	if order.CreatedBy != "" && order.CreatedBy == callerID {
		return common.ErrorResponse(c, http.StatusForbidden, "procurement.self_approval_forbidden",
			"You cannot approve an order you created — segregation of duties requires a different approver")
	}

	if err := h.store.ApproveOrder(c.Request().Context(), orderID, callerID); err != nil {
		return common.ErrorResponse(c, http.StatusBadRequest, "procurement.approve_failed", err.Error())
	}

	h.recordAudit(c, audit.Event{
		Action:     "procurement.order.approve",
		Category:   "procurement",
		TargetType: "vendor_order",
		TargetID:   orderID,
		OldValue:   map[string]any{"status": order.Status},
		NewValue:   map[string]any{"status": "received", "approved_by": callerID},
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "approved"})
}

// RejectOrder handles POST /api/v1/procurement/orders/:id/reject.
// Body: { reason }
//
// Same orders:approve gate and self-rejection ban as ApproveOrder.
func (h *Handler) RejectOrder(c echo.Context) error {
	callerID, _ := c.Get("user_id").(string)
	if callerID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	orderID := c.Param("id")

	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.Reason == "" {
		return common.BadRequest(c, "validation.required", "reason is required")
	}

	order, err := h.store.GetOrder(c.Request().Context(), orderID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "procurement.not_found", "Order not found")
	}
	if order.CreatedBy != "" && order.CreatedBy == callerID {
		return common.ErrorResponse(c, http.StatusForbidden, "procurement.self_rejection_forbidden",
			"You cannot reject an order you created — a different reviewer must take this action")
	}

	if err := h.store.RejectOrder(c.Request().Context(), orderID, callerID, req.Reason); err != nil {
		return common.ErrorResponse(c, http.StatusBadRequest, "procurement.reject_failed", err.Error())
	}

	h.recordAudit(c, audit.Event{
		Action:     "procurement.order.reject",
		Category:   "procurement",
		TargetType: "vendor_order",
		TargetID:   orderID,
		OldValue:   map[string]any{"status": order.Status},
		NewValue:   map[string]any{"status": "closed", "rejected_by": callerID, "reason": req.Reason},
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "rejected"})
}

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
