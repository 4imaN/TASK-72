# Workforce Learning & Procurement Reconciliation Portal

A fully offline, local-network portal combining employee learning-path management with vendor order governance and financial reconciliation.

---

## What It Does

- **Learners** — search a unified resource library, enroll in learning paths (required + elective rules), track progress, resume learning across devices on the same network, and export personal records as CSV.
- **Procurement Specialists** — manage vendor orders, submit reviews with image attachments, record merchant replies, and initiate dispute appeals.
- **Approvers** — decide arbitration outcomes and approval checkpoints.
- **Finance Analysts** — run reconciliation, manage variance write-offs, generate AR/AP entries, approve settlements, and export to file-drop or LAN webhook targets.
- **Content Moderators** — manage review visibility (hide / show with disclaimer / restore) after arbitration.
- **System Administrators** — manage users, roles, taxonomy, configuration center, feature flags, phased rollouts, and compatibility rules; reveal masked personal data with full audit trail.

---

## Stack

| Layer       | Technology                               |
|-------------|------------------------------------------|
| Frontend    | React 18 + TypeScript + Vite             |
| Routing     | React Router v6                          |
| API State   | TanStack Query v5                        |
| Client State| Zustand                                  |
| Forms       | React Hook Form + Zod                    |
| Styles      | Tailwind CSS + shadcn/ui                 |
| Backend     | Go + Echo                                |
| Database    | PostgreSQL 16                            |
| Search      | PostgreSQL full-text + pg_trgm (local)   |
| Auth        | Opaque session tokens in HttpOnly cookies|
| MFA         | TOTP (local, no cloud dependency)        |
| Container   | Docker Compose                           |

---

## Startup

```bash
./run.sh
```

Or, after the first boot, the canonical command is:

```bash
docker compose up --build
```

No manual `.env` creation is required. Secrets are generated automatically by the `bootstrap` service on first boot and persisted in a Docker-managed named volume (`runtime_secrets`).

---

## Docker Services

| Service     | Purpose                                                      |
|-------------|--------------------------------------------------------------|
| `bootstrap` | One-shot runtime secret generator. Runs once and exits.      |
| `postgres`  | PostgreSQL 16 database (local, no cloud).                    |
| `api`       | Go + Echo REST API server on `:8080`.                        |
| `worker`    | Background job runner (export retries, incremental updates). |
| `scheduler` | Scheduled task runner (nightly search rebuild, cleanup).     |
| `web`       | Nginx serving React SPA; proxies `/api` to the API service.  |

---

## Local Access

| URL                            | Description                  |
|--------------------------------|------------------------------|
| `http://localhost:3000`        | React web UI                 |
| `http://localhost:8080/api/health` | API health check         |

---

## No `.env` Bootstrap Model

Sensitive runtime values are generated on first boot by `infra/bootstrap/bootstrap-runtime.sh`:

- session signing key
- AES-256 encryption master key (for encrypted columns)
- TOTP recovery encryption key
- LAN webhook signing key
- Bootstrap account initial passwords (one per role)
- PostgreSQL superuser and app-user passwords

These are written to the Docker `runtime_secrets` named volume. **No plaintext secrets are ever logged or committed.**

To view a bootstrap account password after first boot:

```bash
docker compose exec bootstrap sh -c "cat /runtime/secrets/bootstrap_pw_admin.txt"
```

---

## Bootstrap Accounts

One seeded account exists per role. All accounts require a **mandatory password rotation on first login**.

| Username              | Role                 | Email                      |
|-----------------------|----------------------|----------------------------|
| `bootstrap_learner`   | Learner              | `learner@portal.local`     |
| `bootstrap_procurement` | Procurement Specialist | `procurement@portal.local` |
| `bootstrap_approver`  | Approver             | `approver@portal.local`    |
| `bootstrap_finance`   | Finance Analyst      | `finance@portal.local`     |
| `bootstrap_moderator` | Content Moderator    | `moderator@portal.local`   |
| `bootstrap_admin`     | System Administrator | `admin@portal.local`       |

---

## Session Timeouts

Enforced server-side (configurable in the Configuration Center):

- **Idle timeout**: 15 minutes of inactivity invalidates the session.
- **Absolute timeout**: 8 hours maximum from session creation, regardless of activity.

---

## Read-Only Compatibility Mode

Every API request carries an `X-Client-Version` header. The backend compares this against `client_version_rules` in the database, using a minimum-supported-version model: a rule applies when the caller's version is **below** `min_version` (optionally bounded from above by `max_version`).

- **Full access** — client is at or above the current `min_version`.
- **Read-only grace** — client is below `min_version`, but `grace_until` is in the future. A `block` action is transparently downgraded to `read_only`: write endpoints return `compatibility.read_only`, the UI shows a banner and disables write actions.
- **Blocked** — grace window has expired (or the rule's action is `block` with no grace). The UI redirects to a blocked screen; all requests return `compatibility.blocked`.
- **Warn** — when the rule's action is `warn`, the UI surfaces a non-blocking banner; writes still succeed.

Admins manage rules through `PUT /admin/config/version-rules`, which persists `min_version`, `max_version`, `action` (`block` / `read_only` / `warn`), and the user-facing `message`.

---

## Search Indexing

Search is entirely local using PostgreSQL:

- **Full-text** — `tsvector`/`tsquery` with English dictionary.
- **Fuzzy** — `pg_trgm` trigram similarity for typo tolerance.
- **Synonym expansion** — normalized alias table; optional toggle per query.
- **Pinyin expansion** — optional toggle per query.
- **Nightly rebuild** — scheduler job at 02:00 UTC rebuilds all `search_documents`.
- **Incremental update** — background worker job rebuilds affected documents on content change.

---

## Storage Layout

```
storage/
  private/
    evidence/    # dispute appeal evidence files
    exports/     # generated CSV and finance export files (file-drop outbox)
    search/      # search rebuild temporary artifacts
    runtime/     # local bootstrap state (Docker-managed volume in production)
  logs/          # application logs
```

All stored files carry checksum, content type, uploader, and ownership record. Every download is authorization-checked against the owning record and actor permissions.

---

## Main Routes

| Path                                       | Permission / Role Gate                   |
|--------------------------------------------|------------------------------------------|
| GET `/catalog/resources`                   | (any authenticated)                      |
| POST `/catalog/resources`                  | catalog:write                            |
| PUT `/catalog/resources/:id`               | catalog:write                            |
| POST `/catalog/resources/:id/archive`      | catalog:publish                          |
| GET `/search`                              | (any authenticated)                      |
| GET `/archive/buckets`                     | (any authenticated)                      |
| POST `/search/rebuild`                     | admin role                               |
| GET `/me/enrollments`                      | (any authenticated)                      |
| GET `/me/progress`                         | (any authenticated)                      |
| POST `/procurement/orders`                 | orders:write                             |
| POST `/procurement/orders/:id/approve`     | orders:approve (not orders:write)        |
| POST `/procurement/orders/:id/reject`      | orders:approve                           |
| POST `/reviews`                            | reviews:write                            |
| POST `/reviews/:id/reply`                  | merchant_replies:write                   |
| POST `/reconciliation/runs`                | reconciliation:write                     |
| POST `/reconciliation/runs/:id/process`    | reconciliation:write                     |
| POST `/reconciliation/variances/:id/approve`| writeoffs:approve                       |
| POST `/reconciliation/batches`             | settlements:write                        |
| POST `/taxonomy/conflicts/:id/resolve`     | taxonomy:write                           |
| GET `/admin/config/*`                      | admin role / config:read                 |
| PUT `/admin/config/*`                      | admin role                               |
| GET `/admin/users`                         | admin role                               |
| GET `/admin/users/:id/reveal-email`        | sensitive_data:reveal                    |
| GET `/admin/audit`                         | admin role                               |
| POST `/admin/webhooks`                     | admin role                               |
| POST `/exports/jobs`                       | per-type gate (exports:write for recon)  |

---

## Backend Module Map

```
cmd/api/          Echo API server entrypoint
cmd/worker/       Background runner — export jobs + incremental search index refresh
cmd/scheduler/    Scheduled task runner — 02:00 UTC search rebuild + hourly archive refresh
internal/app/
  auth/           Login, logout, password hashing
  sessions/       HttpOnly cookie session management
  mfa/            TOTP enrollment and verification
  permissions/    Route and object-level authorization (incl. is_active enforcement)
  users/          User management + admin reveal-email + role mutations
  catalog/        Resource library CRUD (Create / Update / Archive / Restore)
  taxonomy/       Skill tag hierarchy, synonyms, conflict detection + resolve queue
  search/         Local full-text + trigram + pinyin search; archive bucket browsing
  recommendations/ Ranking, near-duplicate dedup, diversity cap, trace factors
  learning/       Paths, enrollment, progress, CSV export
  procurement/    Vendor orders + segregated orders:approve workflow
  reviews/        Ratings, attachments, merchant replies — and appeals/evidence/arbitration
                  (the "disputes" capability lives here as appeals_*.go)
  moderation/     Review visibility management
  reconciliation/ Statement comparison, variances (Finance approval gate),
                  settlement batches, AR/AP generation (the "settlement" capability is in
                  this package — there is no separate settlement/ directory)
  exports/        File-drop CSV exports, scoped reconciliation export, webhook fan-out
  webhooks/       LAN webhook endpoints + delivery (RFC1918/loopback enforcement)
  config/         Feature flags, parameters, version rules (the "configcenter" capability)
  audit/          Structured audit log + reveal log collection
  common/         Shared error helpers
internal/platform/
  postgres/       Connection pool and config
  crypto/         AES-GCM encryption and bcrypt helpers
  logging/        Structured JSON logging
  storage/        On-disk file storage with magic-byte + path-traversal guards
  featureflag/    Generic flag-evaluation gate used by handlers and middleware
  http/           Echo router helpers shared across handlers
  scheduler/      Cron-style helpers
  searchindex/    Index utilities used by search and the worker

Note on package layout: the prompt's role taxonomy mentions "disputes",
"settlement", "files", "configcenter", and "jobs" as conceptual capabilities.
In the shipped code these are merged into the packages they are most coupled
with (reviews, reconciliation, internal/platform/storage, config, cmd/worker)
rather than being separate first-party packages.
```

---

## What Is Intentionally Offline-Local

- Authentication and sessions — no external identity provider
- Search indexing — no Elasticsearch, Algolia, or cloud search
- MFA — TOTP generated locally, no SMS or cloud verification
- Recommendations — local ranking logic, no SaaS recommendation service
- File storage — local disk only
- Exports — local file-drop outbox and LAN webhook delivery
- Webhooks — LAN-only, no public internet endpoints
- Audit logs — local PostgreSQL, no external SIEM

Nothing in this product requires internet access. Cloud services are not used, not supported, and not referenced.

---

## Tests

```bash
./run_tests.sh              # all tests (backend Go + frontend Vitest)
./run_tests.sh --backend    # Go tests only
./run_tests.sh --frontend   # Vitest tests only
./run_tests.sh --e2e        # Playwright E2E (requires running stack)

make test-fast              # Go fakes + source assertions; no external deps
make test-integration       # Real PostgreSQL + real handler chain
make test-e2e               # Playwright smoke suite
make test-all               # all of the above
```

There are three Go test layers:

- **`tests/unit`** — store helpers and pure functions (e.g. URL validators,
  recommendation dedup, scheduler timing). No DB.
- **`tests/api` / `tests/security`** — handler-level tests with in-memory
  fakes plus source-level assertions over `cmd/api/main.go` route
  registrations. Catches handler bugs fast; misses real SQL/middleware
  defects by design.
- **`tests/integration`** — real PostgreSQL + the real Echo middleware chain.
  Each test installs migrations + seeds into a per-test schema so the suite
  can run in parallel. Tests skip cleanly when `INTEGRATION_DATABASE_URL`
  is unset, so `go test ./...` never fails just because no DB is available.
  Run with:

  ```bash
  make test-integration \
      INTEGRATION_DATABASE_URL=postgres://user:pw@localhost/portal_it?sslmode=disable
  ```

  The integration suite is the place to add coverage for RBAC, object
  authorization, state machines, and audit-row presence — defects fakes
  cannot catch.

Coverage target: >90% of prompt-critical behavior surface.
