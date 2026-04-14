package procurement

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VendorOrder mirrors the vendor_orders table schema.
type VendorOrder struct {
	ID              string    `json:"id"`
	VendorName      string    `json:"vendor_name"`
	OrderNumber     string    `json:"order_number"`
	OrderDate       string    `json:"order_date"`
	Status          string    `json:"status"` // pending, received, disputed, closed
	TotalAmount     int64     `json:"total_amount"` // integer minor units
	Currency        string    `json:"currency"`
	Description     string    `json:"description,omitempty"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
	CreatedBy       string    `json:"created_by,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// OrderFilter holds filter criteria for listing vendor orders.
type OrderFilter struct {
	Status      string
	RequestedBy string // maps to created_by UUID
}

// Store manages vendor order persistence.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore constructs a Store backed by the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ListOrders returns paginated vendor orders with optional filters.
func (s *Store) ListOrders(ctx context.Context, filter OrderFilter, limit, offset int) ([]VendorOrder, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []any{}
	argN := 1
	where := "WHERE 1=1"

	if filter.Status != "" {
		where += fmt.Sprintf(" AND status = $%d", argN)
		args = append(args, filter.Status)
		argN++
	}
	if filter.RequestedBy != "" {
		where += fmt.Sprintf(" AND created_by = $%d::uuid", argN)
		args = append(args, filter.RequestedBy)
		argN++
	}

	countArgs := make([]any, len(args))
	copy(countArgs, args)

	var total int
	_ = s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM vendor_orders "+where,
		countArgs...,
	).Scan(&total)

	query := fmt.Sprintf(`
		SELECT id, vendor_name, order_number, order_date::text, status,
		       total_amount, currency,
		       coalesce(description,''), coalesce(rejection_reason,''),
		       coalesce(created_by::text,''),
		       created_at, updated_at
		FROM vendor_orders
		%s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		where, argN, argN+1,
	)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	var orders []VendorOrder
	for rows.Next() {
		var o VendorOrder
		if err := rows.Scan(&o.ID, &o.VendorName, &o.OrderNumber, &o.OrderDate,
			&o.Status, &o.TotalAmount, &o.Currency,
			&o.Description, &o.RejectionReason, &o.CreatedBy,
			&o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, 0, err
		}
		orders = append(orders, o)
	}
	if orders == nil {
		orders = []VendorOrder{}
	}
	return orders, total, nil
}

// GetOrder fetches a single vendor order by ID.
func (s *Store) GetOrder(ctx context.Context, id string) (*VendorOrder, error) {
	o := &VendorOrder{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, vendor_name, order_number, order_date::text, status,
		       total_amount, currency,
		       coalesce(description,''), coalesce(rejection_reason,''),
		       coalesce(created_by::text,''),
		       created_at, updated_at
		FROM vendor_orders
		WHERE id = $1`, id).
		Scan(&o.ID, &o.VendorName, &o.OrderNumber, &o.OrderDate,
			&o.Status, &o.TotalAmount, &o.Currency,
			&o.Description, &o.RejectionReason, &o.CreatedBy,
			&o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	return o, nil
}

// CreateOrder inserts a new vendor order with status='pending'.
func (s *Store) CreateOrder(ctx context.Context, vendorName, description, requestedBy string, totalAmount float64) (*VendorOrder, error) {
	id := uuid.New().String()
	orderNumber := "ORD-" + id[:8]
	// totalAmount is in major units (dollars); store as minor units (cents)
	totalMinor := int64(totalAmount * 100)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO vendor_orders
		  (id, vendor_name, order_number, order_date, status, total_amount, currency, description, created_by)
		VALUES ($1, $2, $3, CURRENT_DATE, 'pending', $4, 'USD', NULLIF($5,''), NULLIF($6,'')::uuid)`,
		id, vendorName, orderNumber, totalMinor, description, requestedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	return s.GetOrder(ctx, id)
}

// ApproveOrder transitions a pending order to 'received' and records the
// approver + approval timestamp in the columns added by migration 009.
// Self-approval is blocked at the handler layer (h.ApproveOrder) before this
// store method is reached.
func (s *Store) ApproveOrder(ctx context.Context, orderID, approvedByID string) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE vendor_orders
		SET status      = 'received',
		    approved_by = $2::uuid,
		    approved_at = NOW(),
		    updated_at  = NOW()
		WHERE id = $1 AND status = 'pending'`,
		orderID, approvedByID,
	)
	if err != nil {
		return fmt.Errorf("approve order: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("order not found or not in pending status")
	}
	return nil
}

// RejectOrder transitions a pending order to 'closed' with the rejection
// reason, capturing rejected_by and rejected_at on the row.
func (s *Store) RejectOrder(ctx context.Context, orderID, rejectedByID, reason string) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE vendor_orders
		SET status           = 'closed',
		    rejection_reason = $2,
		    rejected_by      = $3::uuid,
		    rejected_at      = NOW(),
		    updated_at       = NOW()
		WHERE id = $1 AND status = 'pending'`,
		orderID, reason, rejectedByID,
	)
	if err != nil {
		return fmt.Errorf("reject order: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("order not found or not in pending status")
	}
	return nil
}
