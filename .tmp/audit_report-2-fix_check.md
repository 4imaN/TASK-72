# Previous Audit Recheck

Source reviewed: [.tmp/delivery_acceptance_audit.md](.tmp/delivery_acceptance_audit.md:1)

This recheck compares the issues recorded in the earlier audit against the current repository state.

## Summary

- Fixed: 8
- Partially fixed / needs stronger proof: 0
- Still open: 0 implementation defects from the prior issue list
- Remaining gap: none identified from the prior issue list

## Issue Status

### 1. Reconciliation export scoped against legacy `run_by`
- Previous status: Open `High`
- Current status: **Fixed**
- Current evidence:
  - [internal/app/exports/store.go](internal/app/exports/store.go:377)
  - [internal/app/exports/store.go](internal/app/exports/store.go:399)
  - [internal/app/exports/store.go](internal/app/exports/store.go:405)
  - [internal/app/exports/store.go](internal/app/exports/store.go:411)
  - [tests/integration/finance_flow_test.go](tests/integration/finance_flow_test.go:369)
  - [tests/integration/finance_flow_test.go](tests/integration/finance_flow_test.go:476)
  - [tests/integration/finance_flow_test.go](tests/integration/finance_flow_test.go:504)
  - [tests/api/exports_config_test.go](tests/api/exports_config_test.go:825)
  - [tests/api/exports_config_test.go](tests/api/exports_config_test.go:902)
- Notes:
  - Export generation now uses `COALESCE(r.initiated_by::TEXT, r.run_by::TEXT, '')`.
  - Scoped filtering also uses `COALESCE(r.initiated_by::TEXT, r.run_by::TEXT) = $1`.
  - CSV header was updated from `run_by` to `initiated_by`.
  - Integration coverage now verifies scoped CSV content for user A vs user B and the `initiated_by` column.
  - API-level coverage also checks the COALESCE fallback and header shape.

### 2. Reconciliation ignored statement-only rows
- Previous status: Open `High`
- Current status: **Fixed**
- Current evidence:
  - [internal/app/reconciliation/store.go](internal/app/reconciliation/store.go:480)
  - [internal/app/reconciliation/store.go](internal/app/reconciliation/store.go:488)
  - [internal/app/reconciliation/store.go](internal/app/reconciliation/store.go:603)
  - [internal/app/reconciliation/store.go](internal/app/reconciliation/store.go:619)
  - [internal/app/reconciliation/store.go](internal/app/reconciliation/store.go:654)
  - [tests/integration/finance_flow_test.go](tests/integration/finance_flow_test.go:563)
  - [tests/integration/finance_flow_test.go](tests/integration/finance_flow_test.go:632)
  - [tests/integration/finance_flow_test.go](tests/integration/finance_flow_test.go:648)
- Notes:
  - The store now queries unmatched statement rows separately.
  - Those rows are inserted as `unexpected_statement` variances.
  - Suggestion handling includes the new variance type.
  - Integration coverage now verifies that an orphan statement row produces an `unexpected_statement` variance with the expected amounts.

### 3. Recommendations were not truly tag-driven
- Previous status: Open `High`
- Current status: **Fixed**
- Current evidence:
  - [internal/app/recommendations/store.go](internal/app/recommendations/store.go:153)
  - [internal/app/recommendations/store.go](internal/app/recommendations/store.go:194)
  - [internal/app/recommendations/store.go](internal/app/recommendations/store.go:203)
  - [internal/app/recommendations/store.go](internal/app/recommendations/store.go:218)
  - [internal/app/recommendations/store.go](internal/app/recommendations/store.go:267)
  - [tests/unit/recommendations_dedup_test.go](tests/unit/recommendations_dedup_test.go:80)
- Notes:
  - Recommendation scoring now derives `tag_signal` from `resource_tags`.
  - `tag_overlap` is only emitted when `tagSignal > 0`.
  - View history now has its own factor name, `view_history`, instead of being mislabeled as tag overlap.
  - Unit tests were added for factor construction.

### 4. Learning progress accepted arbitrary resources without enrolled-path validation
- Previous status: Open `Medium`
- Current status: **Fixed**
- Current evidence:
  - [internal/app/learning/store.go](internal/app/learning/store.go:227)
  - [internal/app/learning/store.go](internal/app/learning/store.go:230)
  - [internal/app/learning/store.go](internal/app/learning/store.go:239)
  - [tests/api/learning_test.go](tests/api/learning_test.go:591)
  - [tests/api/learning_test.go](tests/api/learning_test.go:609)
- Notes:
  - `RecordProgress` now checks that the resource belongs to at least one active enrolled path for the user.
  - API tests now cover unenrolled and wrong-path progress writes and expect `learning.resource_not_enrolled`.

### 5. Admin audit UI expected `entries` while backend returned `events`
- Previous status: Open `Medium`
- Current status: **Fixed**
- Current evidence:
  - [internal/app/audit/handler.go](internal/app/audit/handler.go:35)
  - [web/src/features/admin/AdminPage.tsx](web/src/features/admin/AdminPage.tsx:1466)
- Notes:
  - The backend still returns `events`.
  - The UI now reads `data?.events ?? []`, so the contract mismatch reported in the old audit is resolved.

### 6. Admin users UI expected `last_login`, backend did not expose it
- Previous status: Open `Medium`
- Current status: **Fixed**
- Current evidence:
  - [internal/app/users/admin_handler.go](internal/app/users/admin_handler.go:54)
  - [internal/app/users/admin_handler.go](internal/app/users/admin_handler.go:69)
  - [internal/app/users/admin_handler.go](internal/app/users/admin_handler.go:83)
  - [internal/app/users/admin_handler.go](internal/app/users/admin_handler.go:156)
  - [web/src/features/admin/AdminPage.tsx](web/src/features/admin/AdminPage.tsx:1244)
  - [web/src/features/admin/AdminPage.tsx](web/src/features/admin/AdminPage.tsx:1397)
- Notes:
  - Admin list/get responses now expose `last_login`.
  - The frontend field is aligned with the backend response.

### 7. README misstated `/admin/audit` authorization
- Previous status: Open `Medium`
- Current status: **Fixed**
- Current evidence:
  - [README.md](README.md:189)
  - [cmd/api/main.go](cmd/api/main.go:373)
- Notes:
  - README now says `admin role`, matching the actual route guard.

## Updated Conclusion

The previous audit file is now stale in multiple material places. The prior implementation-level High and Medium defects listed there have been addressed in the current codebase, and the earlier reconciliation export proof gap is now covered by current tests as well.

If you want a refreshed acceptance verdict based only on these previously reported issues, it would be stronger than the old report and no longer support those earlier High-severity implementation findings as open defects.
