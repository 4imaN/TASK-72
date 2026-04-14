# Architecture & Design

This document describes the system architecture, security model, data flows, and key design decisions for the Workforce Learning & Procurement Reconciliation Portal.

---

## System Overview

The portal is a fully offline, local-network application with no cloud dependencies. It serves six user roles through a single-page React frontend backed by a Go REST API and PostgreSQL 16.

```
                    +-----------+
                    |   Nginx   |  :3000  (SPA + /api proxy)
                    +-----+-----+
                          |
              +-----------+-----------+
              |                       |
       React SPA (Vite)        Go API (:8080)
       - React 18              - Echo framework
       - React Router v6       - Session auth + TOTP MFA
       - TanStack Query v5     - RBAC middleware
       - Zustand               - Structured JSON logging
              |                       |
              |            +----------+----------+
              |            |                     |
              |       PostgreSQL 16         Local Storage
              |       - Full-text search   - exports/
              |       - pg_trgm fuzzy      - evidence/
              |       - Audit logs         - search/
              |                     |
              +---------------------+
                          |
              +-----------+-----------+
              |                       |
         Background Worker       Scheduler
         - Export job processing  - Nightly search rebuild
         - Retry/compensation    - Hourly archive refresh
         - Webhook fan-out
```

---

## Service Architecture

| Service | Entry Point | Purpose |
|---------|-------------|---------|
| **api** | `cmd/api/main.go` | Echo REST server. Handles all HTTP requests, authentication, RBAC, and business logic. |
| **worker** | `cmd/worker/main.go` | Background processor. Polls `export_jobs` every 30s with `FOR UPDATE SKIP LOCKED`, processes CSV generation, handles retries and compensation events, emits webhook fan-out. |
| **scheduler** | `cmd/scheduler/main.go` | Cron-style task runner. Rebuilds `search_documents` nightly at 02:00 UTC, refreshes archive buckets hourly. |
| **bootstrap** | `infra/bootstrap/bootstrap-runtime.sh` | One-shot Alpine container. Generates all runtime secrets on first boot and writes them to a Docker-managed named volume. Exits after completion. |
| **web** | Nginx + `web/dist/` | Serves the built React SPA. Proxies `/api` requests to the Go API service. |
| **postgres** | PostgreSQL 16 Alpine | Single database instance. All data, search indexes, audit logs, and session state. |

---

## Package Layout

### Application Layer (`internal/app/`)

Each package owns its HTTP handlers, data store, and domain types.

| Package | Responsibility |
|---------|---------------|
| `auth` | Login, logout, password hashing, password rotation |
| `sessions` | HttpOnly cookie management, session creation/validation/invalidation, idle + absolute timeouts |
| `mfa` | TOTP enrollment, verification, recovery codes (encrypted at rest) |
| `permissions` | `RequireAuth`, `RequirePermission`, `RequireRole` middleware; MFA gate; client version compatibility gate; disabled account enforcement |
| `users` | User CRUD, admin reveal-email (with audit), role mutations, email masking |
| `catalog` | Resource library CRUD (create, update, archive, restore) |
| `taxonomy` | Skill tag hierarchy, synonyms, conflict detection + resolution queue |
| `search` | Full-text + trigram + pinyin search; archive bucket browsing |
| `recommendations` | Ranking, near-duplicate dedup, diversity cap, trace factors |
| `learning` | Learning paths, enrollment, progress tracking, personal CSV export |
| `procurement` | Vendor orders, segregated approval workflow (orders:approve cannot be held with orders:write) |
| `reviews` | Ratings, image attachments, merchant replies, flagging; appeals with encrypted evidence, arbitration, visibility management |
| `reconciliation` | Statement import, reconciliation runs, variance management, settlement batches (draft/submit/approve/export/settle/void), AR/AP generation, billing rules |
| `exports` | Async export job queue (learning CSV, reconciliation export), per-type permission gates, background processing, webhook fan-out on completion |
| `config` | Feature flags, configuration parameters, client version rules (the Config Center) |
| `webhooks` | LAN webhook endpoint management, delivery queue, RFC1918/loopback enforcement |
| `audit` | Structured audit log, reveal log, admin event recording |
| `common` | Shared error envelope (`AppError`) and HTTP helpers |

### Platform Layer (`internal/platform/`)

| Package | Responsibility |
|---------|---------------|
| `postgres` | Connection pool configuration from environment |
| `crypto` | AES-256-GCM encryption/decryption, key management from environment |
| `logging` | Structured JSON logging with severity levels |
| `storage` | On-disk file storage with magic-byte validation, path-traversal guard, checksum verification |
| `featureflag` | Generic flag evaluation used by handlers and middleware |

---

## Authentication & Security Model

### Session Lifecycle

```
POST /auth/login
  -> Validate credentials (bcrypt)
  -> Check is_active
  -> Check MFA enrollment
  -> Create session (mfa_verified=false if MFA enrolled)
  -> Write HttpOnly cookie (portal_session)
  -> Return requires_mfa flag

POST /mfa/verify  (exempt from MFA gate)
  -> Validate TOTP code against encrypted secret
  -> UPDATE sessions SET mfa_verified=TRUE
  -> Return mfa_verified=true

All subsequent requests:
  -> RequireAuth middleware extracts token from cookie
  -> SHA-256 hash lookup in sessions table
  -> Check is_invalidated, absolute timeout (8h), idle timeout (15min)
  -> Extend idle timer on success
  -> Reject disabled accounts mid-session
  -> MFA gate: if enrolled + !mfa_verified -> 403 mfa_required
  -> Client version compatibility gate
  -> Inject user_id, session_id, mfa_verified into context
```

### RBAC Model

Six seeded roles with a fixed permission matrix:

| Role | Key Permissions |
|------|----------------|
| **learner** | `catalog:read`, `learning:enroll`, `learning:progress` |
| **procurement** | `catalog:read`, `orders:read`, `orders:write`, `reviews:write`, `merchant_replies:write`, `appeals:write` |
| **approver** | `catalog:read`, `orders:read`, `orders:approve`, `reviews:write`, `appeals:decide`, `reconciliation:read` |
| **finance** | `catalog:read`, `orders:read`, `reconciliation:read/write`, `writeoffs:approve`, `settlements:write`, `ar_ap:write`, `exports:write`, `appeals:decide` |
| **moderator** | `catalog:read/write/publish`, `taxonomy:read/write`, `reviews:write`, `moderation:write` |
| **admin** | All permissions via role-based gate (`RequireRole("admin")`) |

Authorization is enforced at three levels:
1. **Route-level** — `RequirePermission`/`RequireRole` middleware on route registration
2. **Handler-level** — Per-type gates (e.g., `exports:write` for reconciliation_export inside `CreateJob`)
3. **Object-level** — Ownership checks (e.g., review author, appeal appellant, export job creator)

### Encryption at Rest

- **MFA secrets** — AES-256-GCM encrypted in `mfa_totp_enrollments.encrypted_secret`
- **Appeal evidence metadata** — AES-256-GCM encrypted in `appeal_evidence.encrypted_metadata`
- **Recovery codes** — bcrypt hashed in `mfa_recovery_codes.code_hash`
- **Session tokens** — SHA-256 hashed; only the hash is stored; the opaque token is never persisted
- **Passwords** — bcrypt hashed in `users.password_hash`

---

## Data Flow: Export Job Processing

```
1. API: POST /exports/jobs {type: "reconciliation_export"}
   -> Validate auth + per-type permission (exports:write)
   -> Scope params (non-admin: created_by = caller)
   -> INSERT export_jobs (status='queued')
   -> Return job immediately

2. Worker (30s poll): SELECT ... FOR UPDATE SKIP LOCKED
   -> UPDATE status='processing'
   -> Call type-specific CSV generator
   -> Write CSV to STORAGE_DIR/exports/
   -> UPDATE status='completed', file_path=...
   -> Emit webhook event (export.completed)
   -> On failure: retry up to max_attempts, then compensation event

3. Download: GET /exports/jobs/:id/download
   -> Validate auth + ownership (creator or admin)
   -> Check status='completed' + file exists on disk
   -> Stream file with Content-Disposition header
```

---

## Data Flow: Reconciliation & Settlement

```
1. Import statements: POST /reconciliation/statements
   -> Parse source_file rows
   -> INSERT statement_rows linked to vendor_orders

2. Create run: POST /reconciliation/runs {period: "2026-04"}
   -> INSERT reconciliation_runs (status='pending')

3. Process run: POST /reconciliation/runs/:id/process
   -> Compare statement_rows against vendor_orders for the period
   -> Generate variances (delta, type, suggestion)
   -> UPDATE run status='completed'

4. Variance approval: POST /reconciliation/variances/:id/approve
   -> Requires writeoffs:approve
   -> State: open -> pending_finance_approval -> finance_approved -> applied

5. Settlement batch lifecycle:
   POST /reconciliation/batches           (draft)
   POST /reconciliation/batches/:id/submit  (under_review)
   POST /reconciliation/batches/:id/approve (approved)
   POST /reconciliation/batches/:id/export  (exported) -> CSV download
   POST /reconciliation/batches/:id/settle  (settled)
   POST /reconciliation/batches/:id/void    (voided) - terminal escape hatch
```

---

## Client Version Compatibility

Every API request carries `X-Client-Version`. The `RequireAuth` middleware evaluates this against `client_version_rules`:

| Mode | Behavior |
|------|----------|
| **full** | Client at or above min_version; unrestricted |
| **read_only** | Client below min_version within grace period; GET/HEAD/OPTIONS allowed, writes return 403 `compatibility.read_only` |
| **blocked** | Grace expired or explicit block action; all requests return 403 `compatibility.blocked` |
| **warn** | Non-blocking banner; writes still succeed |

The `compatibility.check_enabled` feature flag acts as a global kill-switch — when disabled, all version checks are bypassed.

---

## Frontend Architecture

```
web/src/
  app/
    api/client.ts       # Fetch wrapper with auth cookie + version header
    guards/index.tsx    # RequireAuth, useIsReadOnly
    routes/index.tsx    # Route definitions with permission guards
    store/index.ts      # Zustand stores (auth, UI)
    layout/             # AppLayout shell
    providers/          # QueryClient, Router
  features/
    admin/              # Config Center, users, audit, export jobs, webhooks
    finance/            # Reconciliation, settlements, billing rules, export jobs
    moderation/         # Moderation queue with read-only suppression
    procurement/        # Vendor orders, approval workflow
    library/            # Resource catalog browsing
    learning-paths/     # Path enrollment and progress
    progress/           # Personal learning dashboard
    auth/               # Login, password change, MFA setup
    disputes/           # Appeal submission
    approvals/          # Approval dashboard
    archive/            # Archive browsing
```

Key patterns:
- **Route guards** — `<RequireAuth requiredPermission="...">` wraps each route
- **Read-only mode** — `useIsReadOnly()` hook; write-capable components check this and hide/disable action buttons
- **Blocked mode** — `RequireAuth` redirects to `/version-blocked` when `compatibilityMode === 'blocked'`
- **Auto-refetch** — Export job lists poll every 3s while jobs are queued/running

---

## Database Migrations

| Migration | Purpose |
|-----------|---------|
| `001_init.sql` | Core schema: users, roles, permissions, sessions, MFA, catalog, taxonomy, search, learning, procurement, reviews, appeals, evidence, reconciliation, exports, webhooks |
| `002_mfa_session.sql` | Add `mfa_verified` column to sessions |
| `003_recommendations.sql` | Recommendation events and rankings |
| `004_reviews_moderation.sql` | Moderation queue, review disclaimer_text |
| `005_reconciliation.sql` | Settlement batches, cost allocations, billing rules, AR/AP entries |
| `006_exports_config.sql` | Export job Slice-9 columns, config flags/params, version rules, webhook endpoints/deliveries |
| `007_recon_fix.sql` | Reconciliation schema corrections |
| `008_fixes.sql` | Miscellaneous fixes |
| `009_procurement_approval.sql` | Procurement approval columns |
| `010_scrub_session_tokens.sql` | Remove any plaintext session tokens |
| `011_scrub_plaintext_evidence.sql` | Null plaintext columns when encrypted_metadata populated |
| `012_nullable_evidence_metadata.sql` | Allow NULL in evidence metadata columns |

---

## Testing Strategy

Three Go test layers + frontend tests:

| Layer | Directory | Scope | DB Required |
|-------|-----------|-------|-------------|
| **Unit** | `tests/unit/` | Pure functions, validators, dedup logic | No |
| **API** | `tests/api/`, `tests/security/` | Handler-level with in-memory fakes; catches handler bugs fast | No |
| **Integration** | `tests/integration/` | Real PostgreSQL + real middleware chain; catches SQL, RBAC, state-machine defects | Yes (`INTEGRATION_DATABASE_URL`) |
| **Frontend unit** | `web/src/tests/unit/` | Zustand store, API client | No |
| **Frontend component** | `web/src/tests/component/` | Route guards, compatibility mode, write-action suppression | No |
| **E2E** | `web/src/tests/e2e/` | Playwright smoke tests | Full stack |

Integration tests use per-test schema isolation (`CREATE SCHEMA it_<uuid>`) so they can run in parallel against one database without interference. Tests skip cleanly when `INTEGRATION_DATABASE_URL` is unset.

---

## Key Design Decisions

1. **Offline-first** — No cloud services, no external identity provider, no SaaS dependencies. All data stays on the local network.

2. **Async export processing** — Export jobs are queued immediately and processed by the background worker. This prevents API timeouts on large datasets and enables retry/compensation logic.

3. **FOR UPDATE SKIP LOCKED** — The worker safely handles concurrent polls without race conditions or double-processing.

4. **Defense-in-depth authorization** — Route-level permission middleware, handler-level per-type gates, and object-level ownership checks. All three must pass.

5. **Encryption at rest** — Sensitive fields (MFA secrets, evidence metadata) are AES-256-GCM encrypted. Session tokens are SHA-256 hashed. Passwords are bcrypt hashed.

6. **Segregation of duties** — Procurement approval requires `orders:approve` which cannot be combined with `orders:write` on the same user. Self-approval is handler-guarded.

7. **Audit trail** — Every privileged mutation (approval, configuration change, email reveal, moderation decision, settlement transition) records a structured audit event.

8. **Webhook fan-out** — Export completion and settlement transitions emit events to LAN webhook subscribers. Endpoints are restricted to RFC1918/loopback addresses.
