-- Migration 009 — segregate procurement approval from order creation.
--
-- Background: orders:write (held by procurement specialists) was being used
-- to gate both order creation AND approve/reject. That meant a procurement
-- specialist could approve their own orders, violating segregation of duties.
-- This migration introduces orders:approve, assigns it to approver + admin,
-- and adds the approval-state columns the audit log needs.

-- 1. New permission code.
INSERT INTO permissions (code, description) VALUES
  ('orders:approve', 'Approve or reject vendor orders (segregation of duties)')
ON CONFLICT (code) DO NOTHING;

-- 2. Grant orders:approve to the approver role and to admin.
DO $$
DECLARE
  v_role_id BIGINT;
  v_perm_id BIGINT;
BEGIN
  SELECT id INTO v_perm_id FROM permissions WHERE code = 'orders:approve';

  SELECT id INTO v_role_id FROM roles WHERE name = 'approver';
  IF v_role_id IS NOT NULL THEN
    INSERT INTO role_permissions(role_id, permission_id) VALUES (v_role_id, v_perm_id)
    ON CONFLICT DO NOTHING;
  END IF;

  SELECT id INTO v_role_id FROM roles WHERE name = 'admin';
  IF v_role_id IS NOT NULL THEN
    INSERT INTO role_permissions(role_id, permission_id) VALUES (v_role_id, v_perm_id)
    ON CONFLICT DO NOTHING;
  END IF;
END $$;

-- 3. Approval-state columns on vendor_orders.
--    approved_by/approved_at fire on a successful ApproveOrder transition;
--    rejected_by/rejected_at fire on RejectOrder. created_by already exists
--    and is the self-approval guard.
ALTER TABLE vendor_orders
  ADD COLUMN IF NOT EXISTS approved_by  UUID REFERENCES users(id),
  ADD COLUMN IF NOT EXISTS approved_at  TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS rejected_by  UUID REFERENCES users(id),
  ADD COLUMN IF NOT EXISTS rejected_at  TIMESTAMPTZ;
