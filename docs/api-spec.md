# API Reference

Base URL: `/api/v1`

All endpoints return JSON. Error responses use the standard envelope:

```json
{
  "code": "error.code",
  "message": "Human-readable message",
  "fields": { "field_name": ["validation error"] },
  "trace_id": "uuid"
}
```

Every request should include `X-Client-Version` for compatibility enforcement.
Authenticated endpoints require the `portal_session` HttpOnly cookie.

---

## Health & Version

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/health` | No | Health check. Returns `{"status": "ok"}`. |
| GET | `/api/version` | No | API version. Returns `{"version": "...", "service": "portal-api"}`. |

---

## Authentication

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/auth/login` | No | Authenticate with username + password. |
| POST | `/auth/logout` | No | Invalidate session. |
| GET | `/session` | Session | Return current session user data. |
| POST | `/auth/password/change` | Session | Change password (mandatory rotation on first login). |

### POST /auth/login

**Request:**
```json
{ "username": "string", "password": "string" }
```

**Response (200):**
```json
{
  "requires_mfa": true,
  "compatibility_mode": "full",
  "user": {
    "id": "uuid",
    "username": "string",
    "display_name": "string",
    "roles": ["learner"],
    "permissions": ["catalog:read"],
    "force_password_reset": false,
    "mfa_enrolled": true,
    "mfa_verified": false
  }
}
```

When `requires_mfa` is `true`, `user` is `null` until MFA verification completes.
Sets `portal_session` HttpOnly cookie.

---

## MFA

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/mfa/enroll/start` | Session | Begin TOTP enrollment. Returns QR URI + secret. |
| POST | `/mfa/enroll/confirm` | Session | Confirm enrollment with a valid TOTP code. |
| POST | `/mfa/verify` | Session | Verify TOTP code. Upgrades session to MFA-verified. |
| POST | `/mfa/recovery` | Session | Verify recovery code (one-time use). |

### POST /mfa/verify

**Request:**
```json
{ "code": "123456" }
```

**Response (200):**
```json
{ "mfa_verified": true }
```

**Error (403):** MFA gate blocks access to all other protected endpoints until verification succeeds. `/mfa/verify` and `/mfa/recovery` are exempt from the gate.

---

## Catalog

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/catalog/resources` | Session | (any) | List resources. Supports `?q=`, `?type=`, `?category=`, `?limit=`, `?offset=`. |
| GET | `/catalog/resources/:id` | Session | (any) | Get single resource. |
| POST | `/catalog/resources` | Session | `catalog:write` | Create resource. |
| PUT | `/catalog/resources/:id` | Session | `catalog:write` | Update resource. |
| POST | `/catalog/resources/:id/archive` | Session | `catalog:publish` | Archive resource. |
| POST | `/catalog/resources/:id/restore` | Session | `catalog:publish` | Restore archived resource. |

---

## Search

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/search` | Session | (any) | Full-text + fuzzy search. Params: `?q=`, `?type=`, `?category=`, `?synonyms=true`, `?pinyin=true`, `?limit=`, `?offset=`. |
| GET | `/archive/buckets` | Session | (any) | List archive buckets by type/category. |
| GET | `/archive/buckets/:type/:key/resources` | Session | (any) | List resources in a specific archive bucket. |
| POST | `/search/rebuild` | Session | `admin` role | Trigger full search index rebuild. |

---

## Taxonomy

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/taxonomy/tags` | Session | (any) | List all tags. |
| GET | `/taxonomy/tags/:id` | Session | (any) | Get tag with synonyms. |
| POST | `/taxonomy/tags/:id/synonyms` | Session | `taxonomy:write` | Add synonym to tag. |
| GET | `/taxonomy/conflicts` | Session | `taxonomy:write` | List unresolved conflicts. |
| POST | `/taxonomy/conflicts/:id/resolve` | Session | `taxonomy:write` | Resolve a conflict. |

---

## Learning Paths

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/paths` | Session | (any) | List published learning paths. |
| GET | `/paths/:id` | Session | (any) | Get path detail with resources. |
| POST | `/paths/:id/enroll` | Session | (any) | Enroll in a learning path. |
| GET | `/paths/:id/progress` | Session | (any) | Get progress on a path. |

---

## My Progress (Learner)

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/me/enrollments` | Session | (any) | List own enrollments. |
| GET | `/me/progress` | Session | (any) | Get resume state (last resource, position). |
| POST | `/me/progress/:resource_id` | Session | (any) | Record progress event. |
| GET | `/me/exports/csv` | Session | (any) | Download own learning progress as CSV. |

---

## Recommendations

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/recommendations` | Session | (any) | Get personalized recommendations. Gated by `recommendations.enabled` feature flag. |
| POST | `/recommendations/events` | Session | (any) | Record interaction event for ranking. |

---

## Reviews

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| POST | `/reviews` | Session | `reviews:write` | Create review (rating 1-5, body, optional attachments). |
| GET | `/reviews/:id` | Session | (any) | Get review. Hidden reviews return 404 unless caller has `moderation:write`. Business scope: author, `orders:read`, or `moderation:write`. |
| GET | `/orders/:order_id/reviews` | Session | (any) | List reviews for an order. Requires `orders:read` or `moderation:write`. |
| POST | `/reviews/:id/reply` | Session | `merchant_replies:write` | Add merchant reply (one per review). |
| POST | `/reviews/:id/flag` | Session | `reviews:write` | Flag review for moderation. |
| GET | `/reviews/attachments/:id` | Session | (any) | Download attachment. Authorized: author, `orders:read`, or `moderation:write`. |

### POST /reviews

**Request:**
```json
{
  "order_id": "uuid",
  "rating": 4,
  "body": "Great service",
  "attachments": [
    {
      "filename": "receipt.jpg",
      "content_type": "image/jpeg",
      "data": "base64..."
    }
  ]
}
```

---

## Appeals

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| POST | `/appeals` | Session | `appeals:write` | Create appeal on a review with rating <= 2. |
| GET | `/appeals/:id` | Session | (any) | Get appeal. Authorized: appellant or `appeals:decide`. |
| GET | `/appeals` | Session | (any) | List appeals. `appeals:decide` sees all; `appeals:write` sees own only. |
| POST | `/appeals/:id/arbitrate` | Session | `appeals:decide` | Decide appeal: outcome is `hide`, `show_with_disclaimer`, or `restore`. |
| GET | `/appeals/evidence/:id` | Session | (any) | Download evidence file. Authorized: appellant or `appeals:decide`. |

### POST /appeals/:id/arbitrate

**Request:**
```json
{
  "outcome": "hide",
  "notes": "Offensive content confirmed"
}
```

Valid outcomes: `hide`, `show_with_disclaimer`, `restore`.

---

## Moderation Queue

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/moderation/queue` | Session | `moderation:write` | List flagged items. Params: `?status=pending`. |
| POST | `/moderation/queue/:id/decide` | Session | `moderation:write` | Decide item: `approve`, `reject`, or `escalate`. |

---

## Procurement

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/procurement/orders` | Session | `orders:read` | List vendor orders. |
| POST | `/procurement/orders` | Session | `orders:write` | Create vendor order. |
| GET | `/procurement/orders/:id` | Session | `orders:read` | Get order detail. |
| POST | `/procurement/orders/:id/approve` | Session | `orders:approve` | Approve order. Self-approval is blocked. |
| POST | `/procurement/orders/:id/reject` | Session | `orders:approve` | Reject order. |

---

## Reconciliation

### Statement Imports

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| POST | `/reconciliation/statements` | Session | `reconciliation:write` | Import vendor statement batch. |
| GET | `/reconciliation/statements` | Session | `reconciliation:read` | List import batches. |

### POST /reconciliation/statements

**Request:**
```json
{
  "source_file": "vendor_april.csv",
  "checksum": "sha256hex",
  "rows": [
    {
      "order_id": "uuid",
      "line_description": "April invoice",
      "statement_amount": 52000,
      "currency": "USD",
      "transaction_date": "2026-04-10"
    }
  ]
}
```

### Billing Rules

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/reconciliation/rules` | Session | `reconciliation:read` | List versioned billing rules. |

### Reconciliation Runs

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/reconciliation/runs` | Session | `reconciliation:read` | List runs. |
| POST | `/reconciliation/runs` | Session | `reconciliation:write` | Create run for a period. |
| GET | `/reconciliation/runs/:id` | Session | `reconciliation:read` | Get run status + summary. |
| POST | `/reconciliation/runs/:id/process` | Session | `reconciliation:write` | Process run (compare statements vs orders, generate variances). |

### Variances

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/reconciliation/runs/:id/variances` | Session | `reconciliation:read` | List variances for a run. |
| POST | `/reconciliation/variances/:id/submit-approval` | Session | `reconciliation:write` | Submit variance for finance approval. |
| POST | `/reconciliation/variances/:id/approve` | Session | `writeoffs:approve` | Approve variance write-off. |
| POST | `/reconciliation/variances/:id/apply` | Session | `reconciliation:write` | Apply suggestion (adjust amount). |

Variance state machine: `open` -> `pending_finance_approval` -> `finance_approved` -> `applied` (also: `ignored` terminal)

### Settlement Batches

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/reconciliation/batches` | Session | `reconciliation:read` | List batches. |
| POST | `/reconciliation/batches` | Session | `settlements:write` | Create batch with lines and allocations. |
| GET | `/reconciliation/batches/:id` | Session | `reconciliation:read` | Get batch detail with lines. |
| POST | `/reconciliation/batches/:id/submit` | Session | `settlements:write` | Submit for review. |
| POST | `/reconciliation/batches/:id/approve` | Session | `settlements:write` | Approve batch (generates AR/AP entries). |
| POST | `/reconciliation/batches/:id/export` | Session | `settlements:write` | Export batch as CSV. Returns `text/csv`. |
| POST | `/reconciliation/batches/:id/settle` | Session | `settlements:write` | Mark as settled. |
| POST | `/reconciliation/batches/:id/void` | Session | `settlements:write` | Void batch (terminal). |

Batch state machine: `draft` -> `under_review` -> `approved` -> `exported` -> `settled` (also: `voided`, `exception` terminal)

### POST /reconciliation/batches

**Request:**
```json
{
  "run_id": "uuid",
  "lines": [
    {
      "vendor_order_id": "uuid",
      "amount": 3000,
      "direction": "AP",
      "cost_center_id": "CC-001",
      "allocations": [
        {
          "department_code": "FIN",
          "cost_center": "CC-FIN",
          "percentage": 100.0
        }
      ]
    }
  ]
}
```

### Settlement Export CSV Columns

```
batch_id, line_id, vendor_order_id, amount, direction, cost_center_id,
alloc_department_code, alloc_cost_center, alloc_amount, alloc_pct, exported_at
```

One row per allocation. Lines without allocations emit one row with empty allocation fields.

---

## Export Jobs

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| POST | `/exports/jobs` | Session | per-type | Create export job. |
| GET | `/exports/jobs` | Session | (any) | List own jobs (admin sees all). |
| GET | `/exports/jobs/:id` | Session | (any) | Get job status. Owner or admin only. |
| GET | `/exports/jobs/:id/download` | Session | (any) | Download completed export CSV. Owner or admin only. |

### POST /exports/jobs

**Request:**
```json
{
  "type": "reconciliation_export",
  "params": {}
}
```

**Job types:**

| Type | Permission | Scope |
|------|------------|-------|
| `learning_progress_csv` | (any authenticated) | Automatically scoped to caller's own data |
| `reconciliation_export` | `exports:write` or admin | Non-admin scoped to caller's runs |

**Response (201):**
```json
{
  "id": "uuid",
  "type": "reconciliation_export",
  "status": "queued",
  "created_by": "uuid",
  "created_at": "2026-04-14T10:00:00Z"
}
```

Job status lifecycle: `queued` -> `processing` -> `completed` | `failed` (with retry -> `retry`)

**Download (409 Conflict):** Returns conflict if job is not yet completed.

---

## Admin: Config Center

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/admin/config/flags` | Session | `admin` role | List feature flags. |
| PUT | `/admin/config/flags/:key` | Session | `admin` role | Set flag (enabled, rollout_percentage, target_roles). |
| GET | `/admin/config/params` | Session | `config:read` | List configuration parameters. |
| PUT | `/admin/config/params/:key` | Session | `admin` role | Set parameter value. |
| GET | `/admin/config/version-rules` | Session | `config:read` | List client version rules. |
| PUT | `/admin/config/version-rules` | Session | `admin` role | Create/update version rule. |

### PUT /admin/config/flags/:key

**Request:**
```json
{
  "enabled": true,
  "rollout_percentage": 100,
  "target_roles": ["finance", "admin"]
}
```

### PUT /admin/config/version-rules

**Request:**
```json
{
  "min_version": "2.0.0",
  "max_version": "",
  "action": "block",
  "message": "Please upgrade your client",
  "grace_until": "2026-05-01T00:00:00Z"
}
```

Valid actions: `block`, `read_only`, `warn`.

---

## Admin: Webhooks

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/admin/webhooks` | Session | `admin` role | List webhook endpoints. |
| POST | `/admin/webhooks` | Session | `admin` role | Create endpoint. URL must be RFC1918 or loopback. |
| GET | `/admin/webhooks/deliveries` | Session | `admin` role | List delivery attempts. |
| POST | `/admin/webhooks/process` | Session | `admin` role | Process pending deliveries. |

Gated by `exports.webhook_enabled` feature flag.

---

## Admin: Users

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/admin/users` | Session | `admin` role | List users (emails masked for non-admin). |
| GET | `/admin/users/:id` | Session | `admin` role | Get user detail. |
| PUT | `/admin/users/:id/roles` | Session | `admin` role | Update user roles. |
| GET | `/admin/users/:id/reveal-email` | Session | `sensitive_data:reveal` | Reveal masked email (audited). |

---

## Admin: Audit Log

| Method | Path | Auth | Permission | Description |
|--------|------|------|------------|-------------|
| GET | `/admin/audit` | Session | `admin` role | List audit events. Params: `?action=`, `?actor_id=`, `?limit=`, `?offset=`. |

---

## Error Codes

| HTTP Status | Code | Meaning |
|-------------|------|---------|
| 400 | `validation.required` | Required field missing |
| 400 | `validation.invalid_body` | Malformed request body |
| 400 | `validation.invalid_type` | Unknown job type or enum value |
| 401 | `auth.unauthenticated` | No session or expired session |
| 401 | `auth.account_disabled` | Account deactivated mid-session |
| 403 | `auth.forbidden` | Insufficient permissions or role |
| 403 | `mfa_required` | MFA enrolled but session not verified |
| 403 | `compatibility.blocked` | Client version blocked |
| 403 | `compatibility.read_only` | Client version in read-only mode (write attempt) |
| 404 | `*.not_found` | Resource not found (or hidden review for non-moderator) |
| 409 | `exports.not_ready` | Export job not yet completed |
| 409 | `reconciliation.invalid_state` | Invalid state transition |
| 500 | `internal` | Server error |
