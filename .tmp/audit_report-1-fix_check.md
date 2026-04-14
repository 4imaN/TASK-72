# Prior Audit Issue Recheck

## Verdict
- Overall status: `Fixed`
- Summary: the prior product gap and the previously open coverage gaps are now materially addressed by the current static evidence.

## Recheck Results

### 1. High

#### Finance export workflow was not available in the finance-facing portal
- Status: `Fixed`
- Prior concern: finance users could create `reconciliation_export` jobs in the backend, but the only UI lived in the admin-only area.
- Current evidence:
  - `web/src/features/finance/FinancePage.tsx:174` adds an `exports` tab to the finance console.
  - `web/src/features/finance/FinancePage.tsx:177` shows the tab for users with `exports:write`.
  - `web/src/features/finance/FinancePage.tsx:1132` defines `ExportJobsTab`.
  - `web/src/features/finance/FinancePage.tsx:1147` creates reconciliation export jobs from the finance page.
  - `web/src/features/finance/FinancePage.tsx:1225` exposes download links for completed export jobs.
  - `web/src/features/finance/FinancePage.tsx:1302` renders the export tab in the finance page.
- Conclusion: finance-role users now have a finance-facing export workflow without going through the admin page.
- Potential remaining follow-up: add a dedicated UI test for the finance export tab if stronger frontend regression protection is needed.

### 2. Medium

#### MFA/session gate lacked real-stack verification
- Status: `Fixed`
- Current evidence:
  - `tests/integration/mfa_session_test.go:1` adds real-DB integration coverage for MFA/session gating.
  - `tests/integration/mfa_session_test.go:67` verifies MFA-enrolled but unverified users get `403`.
  - `tests/integration/mfa_session_test.go:81` verifies MFA-enrolled and verified users pass.
  - `tests/integration/mfa_session_test.go:98` verifies `/mfa/verify` is exempt from the MFA gate.
- Additional end-to-end evidence:
  - `tests/integration/login_mfa_e2e_test.go:1` adds a real login → MFA verify → protected route integration test.
  - `tests/integration/login_mfa_e2e_test.go:118` exercises `POST /api/v1/auth/login`.
  - `tests/integration/login_mfa_e2e_test.go:156` exercises `POST /api/v1/mfa/verify` with a real TOTP code.
  - `tests/integration/login_mfa_e2e_test.go:170` verifies the protected route returns `200` after MFA verification.
- Conclusion: the original MFA/session coverage gap is now materially fixed.

#### Export job ownership and download authorization were under-covered
- Status: `Fixed`
- Current evidence:
  - `tests/integration/export_authz_test.go:31` adds real-stack export authorization coverage.
  - `tests/integration/export_authz_test.go:62` verifies finance users can create reconciliation exports.
  - `tests/integration/export_authz_test.go:79` verifies non-finance users are forbidden from creating reconciliation exports.
  - `tests/integration/export_authz_test.go:97` verifies list scoping to the owner.
  - `tests/integration/export_authz_test.go:129` verifies cross-user `GET /exports/jobs/:id` is forbidden.
  - `tests/integration/export_authz_test.go:148` verifies cross-user download is forbidden.
  - `internal/app/exports/handler.go:177` enforces owner/admin checks on job reads.
  - `internal/app/exports/handler.go:199` enforces owner/admin checks on downloads.
- Conclusion: this previously reported gap is materially fixed.
- Potential follow-up: add a completed-job download success case if you want full happy-path coverage in the same suite.

#### Compatibility read-only / blocked behavior had no meaningful enforcement coverage
- Status: `Fixed`
- Current evidence:
  - `tests/integration/compatibility_test.go:1` adds real-stack backend compatibility-mode tests.
  - `tests/integration/compatibility_test.go:80` verifies blocked clients get `403`.
  - `tests/integration/compatibility_test.go:96` verifies read-only clients can still `GET`.
  - `tests/integration/compatibility_test.go:104` verifies read-only clients cannot `POST`.
  - `web/src/app/guards/index.tsx:29` redirects blocked users to `/version-blocked`.
  - `web/src/app/guards/index.tsx:52` exposes read-only mode to the UI via `useIsReadOnly`.
- Additional frontend evidence:
  - `web/src/tests/component/guards.test.tsx:57` verifies blocked users are redirected to `/version-blocked`.
  - `web/src/tests/component/guards.test.tsx:116` verifies read-only mode does not block navigation.
  - `web/src/tests/component/guards.test.tsx:182` verifies `useIsReadOnly`.
  - `web/src/features/moderation/ModerationPage.tsx:326` consumes `useIsReadOnly`.
  - `web/src/features/moderation/ModerationPage.tsx:238` suppresses moderation action buttons when read-only.
- Additional write-suppression evidence:
  - `web/src/tests/component/guards.test.tsx:233` adds a dedicated read-only write-action suppression test section.
  - `web/src/tests/component/guards.test.tsx:272` verifies write actions are hidden in `read_only` mode.
- Conclusion: backend enforcement, blocked routing, and frontend read-only action suppression are now all statically covered.

#### Settlement/export integration coverage did not prove export output and allocation details
- Status: `Fixed`
- Current evidence:
  - `tests/integration/finance_flow_test.go:143` adds settlement/export integration coverage.
  - `tests/integration/finance_flow_test.go:228` creates a settlement batch with allocation data.
  - `tests/integration/finance_flow_test.go:275` exports the settlement batch.
  - `tests/integration/finance_flow_test.go:279` asserts CSV content is returned.
  - `tests/integration/finance_flow_test.go:290` parses the exported CSV.
  - `tests/integration/finance_flow_test.go:308` asserts allocation columns exist.
  - `tests/integration/finance_flow_test.go:316` and `tests/integration/finance_flow_test.go:319` assert `FIN` and `CC-FIN` allocation values.
  - `tests/integration/finance_flow_test.go:325` asserts `cost_center_id=CC-001`.
  - `internal/app/reconciliation/store.go:1022` shows the export includes allocation breakdown columns.
- Conclusion: the allocation-aware export artifact is now statically verified by integration tests and implementation.

#### Review/evidence object authorization lacked real-stack coverage
- Status: `Fixed`
- Current evidence:
  - `tests/integration/review_authz_test.go:1` adds real-stack review and appeal authorization coverage.
  - `tests/integration/review_authz_test.go:97` verifies the author can read their own review.
  - `tests/integration/review_authz_test.go:105` verifies an unrelated user gets `403` for another user’s review.
  - `tests/integration/review_authz_test.go:113` verifies moderators can read hidden/other reviews.
  - `tests/integration/review_authz_test.go:150` verifies the appellant can read their own appeal.
  - `tests/integration/review_authz_test.go:158` verifies another user gets `403` for the appeal.
  - `tests/integration/review_authz_test.go:166` verifies an arbiter can read any appeal.
  - `internal/app/reviews/handler.go:539` and `internal/app/reviews/handler.go:594` define protected attachment/evidence download endpoints.
- Additional download-authorization evidence:
  - `tests/integration/review_authz_test.go:239` registers `GET /reviews/attachments/:id` against `DownloadAttachment`.
  - `tests/integration/review_authz_test.go:241` registers `GET /appeals/evidence/:id` against `DownloadEvidence`.
  - `tests/integration/review_authz_test.go:274` verifies the review author is authorized for attachment download.
  - `tests/integration/review_authz_test.go:287` verifies an unrelated user gets `403` for attachment download.
  - `tests/integration/review_authz_test.go:303` and `tests/integration/review_authz_test.go:311` cover attachment `404` and `401`.
  - `tests/integration/review_authz_test.go:337` verifies the appellant is authorized for evidence download.
  - `tests/integration/review_authz_test.go:345` verifies an unrelated user gets `403` for evidence download.
  - `tests/integration/review_authz_test.go:361` and `tests/integration/review_authz_test.go:369` cover evidence `404` and `401`.
- Conclusion: review, appeal, attachment, and evidence object-authorization are now materially covered under the real integration stack.

## Final Recheck Summary
- Fixed:
  - finance-facing reconciliation export UI
  - MFA/session end-to-end coverage
  - export job ownership/download authorization coverage
  - compatibility-mode coverage
  - settlement/export artifact allocation coverage
  - review/evidence object-authorization integration coverage
- Remaining:
  - no previously tracked material issue remains open in this recheck file
