# Test Coverage Audit

## Scope

Static inspection only. No code, tests, scripts, containers, servers, or package managers were run.

Project type explicitly declared in `README.md` and supported by repository structure: **fullstack**. Evidence: `README.md:3`, `cmd/api/main.go:146-383`, and `web/src/app/routes/index.tsx:62-159`.

## Backend Endpoint Inventory

Source of truth: `cmd/api/main.go:146-383`

Total resolved backend endpoints: **94**

### API Test Mapping Table

#### Core/Auth/MFA

| Endpoint | Covered | Test type | Test files | Evidence |
|---|---|---|---|---|
| `GET /api/health` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/health_test.go` | route `cmd/api/main.go:146`; `TestHealth` at `tests/external/external_api_test.go:148`; in-process `TestHealthEndpoint` at `tests/api/health_test.go:35` |
| `GET /api/version` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/health_test.go` | route `cmd/api/main.go:154`; `TestVersion` at `tests/external/external_api_test.go:158`; in-process `TestVersionEndpoint` at `tests/api/health_test.go:58` |
| `POST /api/v1/auth/login` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/auth_test.go`, `tests/integration/login_mfa_e2e_test.go` | route `cmd/api/main.go:183`; `TestLoginSuccess` at `tests/external/external_api_test.go:170`; `TestLoginCorrectCredentialsReturns200WithCookie` at `tests/api/auth_test.go:272`; `TestLoginMFAVerify_EndToEnd` at `tests/integration/login_mfa_e2e_test.go:36` |
| `POST /api/v1/auth/logout` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/auth_test.go` | route `cmd/api/main.go:184`; live-stack `TestAuthLogout` at `tests/external/external_api_test.go:1281`; in-process `TestLogoutClearsSession` at `tests/api/auth_test.go:364` |
| `GET /api/v1/session` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/auth_test.go`, `tests/security/auth_security_test.go` | route `cmd/api/main.go:188`; `TestSessionEndpoint` at `tests/external/external_api_test.go:892`; `TestGetSessionWithValidCookieReturns200` at `tests/api/auth_test.go:415` |
| `POST /api/v1/auth/password/change` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/auth_test.go` | route `cmd/api/main.go:189`; `TestPasswordChangeEndpoint` at `tests/external/external_api_test.go:840`; `TestChangePasswordForceResetDoesNotNeedCurrentPassword` at `tests/api/auth_test.go:446` |
| `GET /api/v1/ping` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:190`; `TestPing` at `tests/external/external_api_test.go:205` |
| `POST /api/v1/mfa/enroll/start` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:197`; `TestMFAEndpoints` at `tests/external/external_api_test.go:805`; stronger path-specific `TestMFAEnrollStart` at `tests/external/external_api_test.go:1194` |
| `POST /api/v1/mfa/enroll/confirm` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:198`; `TestMFAEnrollConfirm` at `tests/external/external_api_test.go:1205` |
| `POST /api/v1/mfa/verify` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/integration/login_mfa_e2e_test.go`, `tests/integration/mfa_session_test.go` | route `cmd/api/main.go:199`; `TestMFAEndpoints` at `tests/external/external_api_test.go:805`; in-process real-DB `TestLoginMFAVerify_EndToEnd` at `tests/integration/login_mfa_e2e_test.go:36` |
| `POST /api/v1/mfa/recovery` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:200`; `TestMFAEndpoints` at `tests/external/external_api_test.go:805` |
| `POST /api/v1/auth/mfa/verify` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:202`; `TestMFAEndpoints` at `tests/external/external_api_test.go:805` |
| `POST /api/v1/auth/mfa/recovery` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:203`; `TestMFAEndpoints` at `tests/external/external_api_test.go:805` |

#### Catalog/Search/Taxonomy/Learning/Recommendations

| Endpoint | Covered | Test type | Test files | Evidence |
|---|---|---|---|---|
| `GET /api/v1/catalog/resources` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/catalog_test.go` | route `cmd/api/main.go:209`; `TestCatalogCRUD` at `tests/external/external_api_test.go:217`; `TestBehavior_CatalogFullLifecycle` at `tests/external/behavior_test.go:16`; `TestCatalogListResources_ReturnsAll` at `tests/api/catalog_test.go:137` |
| `GET /api/v1/catalog/resources/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/catalog_test.go` | route `cmd/api/main.go:210`; `TestCatalogCRUD` at `tests/external/external_api_test.go:217`; `TestCatalogGetResource_Found` at `tests/api/catalog_test.go:278` |
| `POST /api/v1/catalog/resources` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go` | route `cmd/api/main.go:214`; `TestCatalogCRUD` at `tests/external/external_api_test.go:217`; `TestBehavior_CatalogFullLifecycle` at `tests/external/behavior_test.go:16` |
| `PUT /api/v1/catalog/resources/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go` | route `cmd/api/main.go:215`; `TestCatalogCRUD` at `tests/external/external_api_test.go:217`; `TestBehavior_CatalogFullLifecycle` at `tests/external/behavior_test.go:16` |
| `POST /api/v1/catalog/resources/:id/archive` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go` | route `cmd/api/main.go:216`; `TestCatalogCRUD` at `tests/external/external_api_test.go:217`; `TestBehavior_CatalogFullLifecycle` at `tests/external/behavior_test.go:16` |
| `POST /api/v1/catalog/resources/:id/restore` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go` | route `cmd/api/main.go:217`; `TestCatalogCRUD` at `tests/external/external_api_test.go:217`; `TestBehavior_CatalogFullLifecycle` at `tests/external/behavior_test.go:16` |
| `GET /api/v1/search` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/search_test.go`, `tests/security/hardening_test.go` | route `cmd/api/main.go:225`; `TestSearch` at `tests/external/external_api_test.go:273`; `TestBehavior_SearchResponseShape` at `tests/external/behavior_test.go:334`; `TestSearch_QueryFiltersResults` at `tests/api/search_test.go:254` |
| `GET /api/v1/archive/buckets` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:226`; `TestArchiveBuckets` at `tests/external/external_api_test.go:290` |
| `GET /api/v1/archive/buckets/:type/:key/resources` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:227`; `TestArchiveBucketResources` at `tests/external/external_api_test.go:297` |
| `POST /api/v1/search/rebuild` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:228`; `TestSearchRebuild` at `tests/external/external_api_test.go:283` |
| `GET /api/v1/taxonomy/tags` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go` | route `cmd/api/main.go:234`; `TestTaxonomyTags` at `tests/external/external_api_test.go:306`; `TestBehavior_TaxonomySeeded` at `tests/external/behavior_test.go:526` |
| `GET /api/v1/taxonomy/tags/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:235`; `TestTaxonomyTagDetail` at `tests/external/external_api_test.go:316` |
| `POST /api/v1/taxonomy/tags/:id/synonyms` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:236`; `TestTaxonomyAddSynonym` at `tests/external/external_api_test.go:337` |
| `GET /api/v1/taxonomy/conflicts` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:237`; `TestTaxonomyConflicts` at `tests/external/external_api_test.go:330` |
| `POST /api/v1/taxonomy/conflicts/:id/resolve` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:238`; `TestTaxonomyConflictResolve` at `tests/external/external_api_test.go:1181` |
| `GET /api/v1/paths` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/learning_test.go` | route `cmd/api/main.go:245`; `TestLearningPaths` at `tests/external/external_api_test.go:360`; `TestBehavior_EnrollmentCreatesRecord` at `tests/external/behavior_test.go:368`; `TestListPathsReturnsPublishedPaths` at `tests/api/learning_test.go:344` |
| `GET /api/v1/paths/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/learning_test.go` | route `cmd/api/main.go:246`; `TestLearningPathDetail` at `tests/external/external_api_test.go:367`; `tests/api/learning_test.go:115` plus request at `tests/api/learning_test.go:452` |
| `POST /api/v1/paths/:id/enroll` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/learning_test.go` | route `cmd/api/main.go:247`; `TestLearningEnroll` at `tests/external/external_api_test.go:381`; `TestBehavior_EnrollmentCreatesRecord` at `tests/external/behavior_test.go:368`; `TestEnrollInPath` at `tests/api/learning_test.go:377` |
| `GET /api/v1/paths/:id/progress` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/learning_test.go` | route `cmd/api/main.go:248`; `TestLearningPathProgress` at `tests/external/external_api_test.go:398`; `TestGetPathProgressNotEnrolled` at `tests/api/learning_test.go:443` |
| `GET /api/v1/me/enrollments` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go` | route `cmd/api/main.go:251`; `TestMeEnrollments` at `tests/external/external_api_test.go:412`; `TestBehavior_EnrollmentCreatesRecord` at `tests/external/behavior_test.go:368` |
| `GET /api/v1/me/progress` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/learning_test.go` | route `cmd/api/main.go:252`; `TestMeProgress` at `tests/external/external_api_test.go:419`; `TestRecordProgressUpdatesSnapshot` at `tests/api/learning_test.go:466` |
| `POST /api/v1/me/progress/:resource_id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/learning_test.go` | route `cmd/api/main.go:253`; `TestRecordProgress` at `tests/external/external_api_test.go:920`; `TestRecordProgressUpdatesSnapshot` at `tests/api/learning_test.go:466` |
| `GET /api/v1/me/exports/csv` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/learning_test.go` | route `cmd/api/main.go:254`; `TestMeExportsCSV` at `tests/external/external_api_test.go:426`; `TestBehavior_LearnerCSVExportWellFormed` at `tests/external/behavior_test.go:467`; `TestCSVExportOnlyOwnData` at `tests/api/learning_test.go:535` |
| `GET /api/v1/recommendations` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/recommendations_test.go` | route `cmd/api/main.go:261`; `TestRecommendations` at `tests/external/external_api_test.go:438`; `TestGetRecommendations_WithAuth_Returns200` at `tests/api/recommendations_test.go:149` |
| `POST /api/v1/recommendations/events` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/recommendations_test.go` | route `cmd/api/main.go:262`; `TestRecommendationEvent` at `tests/external/external_api_test.go:445`; `TestRecordEvent_ValidEvent_Returns204` at `tests/api/recommendations_test.go:184` |

#### Reviews/Appeals/Moderation

| Endpoint | Covered | Test type | Test files | Evidence |
|---|---|---|---|---|
| `POST /api/v1/reviews` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/moderation_test.go`, `tests/integration/review_authz_test.go`, `tests/security/hardening_test.go` | route `cmd/api/main.go:269`; `TestReviewCreateRequiresData` at `tests/external/external_api_test.go:934`; `TestBehavior_ReviewValidationErrors` at `tests/external/behavior_test.go:502`; `TestCreateReview_Valid` at `tests/api/moderation_test.go:515` |
| `GET /api/v1/reviews/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/moderation_test.go`, `tests/integration/review_authz_test.go` | route `cmd/api/main.go:270`; `TestReviewDetailEndpoint` at `tests/external/external_api_test.go:970`; `TestGetReview_Found` at `tests/api/moderation_test.go:604`; `TestReviewAppealAuthorization_RealStack` at `tests/integration/review_authz_test.go:31` |
| `GET /api/v1/orders/:order_id/reviews` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/moderation_test.go` | route `cmd/api/main.go:271`; live-stack `TestOrderReviewsEndpoint` at `tests/external/external_api_test.go:1256`; local route stub remains at `tests/api/moderation_test.go:243-254` |
| `POST /api/v1/reviews/:id/reply` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/moderation_test.go` | route `cmd/api/main.go:272`; `TestReviewReplyEndpoint` at `tests/external/external_api_test.go:957`; `TestAddMerchantReply_Valid` at `tests/api/moderation_test.go:634` |
| `POST /api/v1/reviews/:id/flag` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/moderation_test.go` | route `cmd/api/main.go:273`; `TestReviewFlagEndpoint` at `tests/external/external_api_test.go:943`; `TestFlagReview_Valid` at `tests/api/moderation_test.go:668` |
| `GET /api/v1/reviews/attachments/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/integration/review_authz_test.go` | route `cmd/api/main.go:274`; `TestReviewAttachmentEndpoint` at `tests/external/external_api_test.go:980`; `TestAttachmentEvidenceDownload_RealStack` at `tests/integration/review_authz_test.go:214` |
| `POST /api/v1/appeals` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/moderation_test.go`, `tests/integration/review_authz_test.go` | route `cmd/api/main.go:277`; `TestAppealCreateEndpoint` at `tests/external/external_api_test.go:990`; `TestCreateAppeal_LowRating` at `tests/api/moderation_test.go:694` |
| `GET /api/v1/appeals/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/moderation_test.go`, `tests/integration/review_authz_test.go` | route `cmd/api/main.go:278`; `TestAppealDetailEndpoint` at `tests/external/external_api_test.go:1003`; `TestCreateAppeal_LowRating` at `tests/api/moderation_test.go:694` and request assertions in `tests/integration/review_authz_test.go:151` |
| `GET /api/v1/appeals` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:279`; `TestAppealsList` at `tests/external/external_api_test.go:467` |
| `POST /api/v1/appeals/:id/arbitrate` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/moderation_test.go` | route `cmd/api/main.go:280`; `TestAppealArbitrateEndpoint` at `tests/external/external_api_test.go:1013`; `TestArbitrate_ValidOutcome` at `tests/api/moderation_test.go:775` |
| `GET /api/v1/appeals/evidence/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/integration/review_authz_test.go` | route `cmd/api/main.go:281`; `TestEvidenceDownloadEndpoint` at `tests/external/external_api_test.go:1026`; `TestAttachmentEvidenceDownload_RealStack` at `tests/integration/review_authz_test.go:214` |
| `GET /api/v1/moderation/queue` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/moderation_test.go` | route `cmd/api/main.go:284`; `TestModerationQueue` at `tests/external/external_api_test.go:474`; `TestGetModerationQueue_Authorized` at `tests/api/moderation_test.go:865` |
| `POST /api/v1/moderation/queue/:id/decide` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/moderation_test.go` | route `cmd/api/main.go:285`; `TestModerationDecideEndpoint` at `tests/external/external_api_test.go:1036`; `TestDecideModerationItem_Valid` at `tests/api/moderation_test.go:905` |

#### Reconciliation/Exports/Admin/Procurement

| Endpoint | Covered | Test type | Test files | Evidence |
|---|---|---|---|---|
| `POST /api/v1/reconciliation/statements` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:297`; `TestReconciliationStatementsImport` at `tests/external/external_api_test.go:521`; `TestBehavior_ReconciliationFlow` at `tests/external/behavior_test.go:90`; `TestStatementImportAndReconciliation` at `tests/integration/finance_flow_test.go:32` |
| `GET /api/v1/reconciliation/statements` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:298`; `TestReconciliationStatements` at `tests/external/external_api_test.go:483`; `TestStatementImportAndReconciliation` at `tests/integration/finance_flow_test.go:32` |
| `GET /api/v1/reconciliation/rules` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go` | route `cmd/api/main.go:301`; `TestReconciliationRules` at `tests/external/external_api_test.go:490`; `TestListBillingRulesOK` at `tests/api/reconciliation_test.go:527` |
| `GET /api/v1/reconciliation/runs` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:304`; `TestReconciliationRuns` at `tests/external/external_api_test.go:497`; `tests/api/reconciliation_test.go:145` plus request at `tests/api/reconciliation_test.go:973` |
| `POST /api/v1/reconciliation/runs` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/reconciliation_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:305`; `TestReconciliationRunCreate` at `tests/external/external_api_test.go:511`; `TestBehavior_ReconciliationFlow` at `tests/external/behavior_test.go:90`; `TestCreateReconciliationRun` at `tests/api/reconciliation_test.go:550` |
| `GET /api/v1/reconciliation/runs/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:306`; `TestReconciliationRunDetail` at `tests/external/external_api_test.go:904`; `TestBehavior_ReconciliationFlow` at `tests/external/behavior_test.go:90` |
| `POST /api/v1/reconciliation/runs/:id/process` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/reconciliation_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:307`; `TestReconciliationProcessRun` at `tests/external/external_api_test.go:1216`; `TestProcessReconciliationRun` at `tests/api/reconciliation_test.go:573` |
| `GET /api/v1/reconciliation/runs/:id/variances` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:310`; `TestReconciliationVariances` at `tests/external/external_api_test.go:539`; `TestListVariances` at `tests/api/reconciliation_test.go:603` |
| `POST /api/v1/reconciliation/variances/:id/submit-approval` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go` | route `cmd/api/main.go:311`; `TestVarianceSubmitApproval` at `tests/external/external_api_test.go:1079`; `TestSubmitVarianceForApprovalIdempotencyRejected` at `tests/api/reconciliation_test.go:737` |
| `POST /api/v1/reconciliation/variances/:id/approve` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go` | route `cmd/api/main.go:312`; `TestVarianceApprove` at `tests/external/external_api_test.go:1089`; `TestApproveVarianceRequiresPendingState` at `tests/api/reconciliation_test.go:707` |
| `POST /api/v1/reconciliation/variances/:id/apply` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go` | route `cmd/api/main.go:313`; `TestVarianceApply` at `tests/external/external_api_test.go:1099`; `TestApplySuggestion` at `tests/api/reconciliation_test.go:636` |
| `GET /api/v1/reconciliation/batches` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go` | route `cmd/api/main.go:316`; `TestReconciliationBatches` at `tests/external/external_api_test.go:504`; `tests/api/reconciliation_test.go:289` |
| `POST /api/v1/reconciliation/batches` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:317`; `TestSettlementBatchCreate` at `tests/external/external_api_test.go:1048`; `TestCreateSettlementBatch` at `tests/api/reconciliation_test.go:773`; `TestSettlementExportAndAllocation` at `tests/integration/finance_flow_test.go:151` |
| `GET /api/v1/reconciliation/batches/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go` | route `cmd/api/main.go:318`; `TestSettlementBatchDetail` at `tests/external/external_api_test.go:1069`; `TestFullSettlementLifecycle` at `tests/api/reconciliation_test.go:1024` |
| `POST /api/v1/reconciliation/batches/:id/submit` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:319`; `TestBatchSubmit` at `tests/external/external_api_test.go:1109`; `TestSubmitBatchForApproval` at `tests/api/reconciliation_test.go:813` |
| `POST /api/v1/reconciliation/batches/:id/approve` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:320`; `TestBatchApprove` at `tests/external/external_api_test.go:1119`; `TestApproveBatch` at `tests/api/reconciliation_test.go:848` |
| `POST /api/v1/reconciliation/batches/:id/export` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:321`; `TestBatchExport` at `tests/external/external_api_test.go:1129`; `TestExportBatch` at `tests/api/reconciliation_test.go:888` |
| `POST /api/v1/reconciliation/batches/:id/settle` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go` | route `cmd/api/main.go:322`; `TestBatchSettle` at `tests/external/external_api_test.go:1139`; `TestFullSettlementLifecycle` at `tests/api/reconciliation_test.go:1024` |
| `POST /api/v1/reconciliation/batches/:id/void` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/reconciliation_test.go` | route `cmd/api/main.go:323`; `TestBatchVoid` at `tests/external/external_api_test.go:1149`; `TestVoidDraftBatch` at `tests/api/reconciliation_test.go:938` |
| `POST /api/v1/exports/jobs` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/exports_config_test.go`, `tests/integration/export_authz_test.go`, `tests/integration/finance_flow_test.go` | route `cmd/api/main.go:333`; `TestExportJobCreate` at `tests/external/external_api_test.go:563`; `TestCreateExportJobReturns201` at `tests/api/exports_config_test.go:457`; `TestExportJobAuthorization_RealStack` at `tests/integration/export_authz_test.go:31` |
| `GET /api/v1/exports/jobs` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/exports_config_test.go`, `tests/integration/export_authz_test.go` | route `cmd/api/main.go:334`; `TestExportJobsList` at `tests/external/external_api_test.go:556`; `TestBehavior_NoFilePathLeakInResponses` at `tests/external/behavior_test.go:484`; `TestListExportJobsReturnsOwnJobs` at `tests/api/exports_config_test.go:487` |
| `GET /api/v1/exports/jobs/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/exports_config_test.go`, `tests/integration/export_authz_test.go` | route `cmd/api/main.go:335`; `TestExportJobDetail` at `tests/external/external_api_test.go:1161`; `TestGetExportJobReturns200` at `tests/api/exports_config_test.go:521` |
| `GET /api/v1/exports/jobs/:id/download` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/integration/export_authz_test.go` | route `cmd/api/main.go:336`; `TestExportJobDownload` at `tests/external/external_api_test.go:1171`; `TestExportJobAuthorization_RealStack` at `tests/integration/export_authz_test.go:31` |
| `GET /api/v1/admin/config/flags` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/exports_config_test.go` | route `cmd/api/main.go:342`; `TestConfigFlags` at `tests/external/external_api_test.go:577`; `TestBehavior_ConfigFlagToggleRoundTrip` at `tests/external/behavior_test.go:214`; `TestListConfigFlagsAdminOnly` at `tests/api/exports_config_test.go:553` |
| `PUT /api/v1/admin/config/flags/:key` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/exports_config_test.go` | route `cmd/api/main.go:343`; `TestConfigFlagSet` at `tests/external/external_api_test.go:587`; `TestBehavior_ConfigFlagToggleRoundTrip` at `tests/external/behavior_test.go:214`; `TestSetConfigFlagUpdatesFlag` at `tests/api/exports_config_test.go:576` |
| `GET /api/v1/admin/config/params` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/exports_config_test.go` | route `cmd/api/main.go:344`; `TestConfigParams` at `tests/external/external_api_test.go:596`; `TestListConfigParamsReturns200` at `tests/api/exports_config_test.go:605` |
| `PUT /api/v1/admin/config/params/:key` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:345`; `TestConfigParamSet` at `tests/external/external_api_test.go:603` |
| `GET /api/v1/admin/config/version-rules` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/exports_config_test.go` | route `cmd/api/main.go:346`; `TestConfigVersionRules` at `tests/external/external_api_test.go:612`; `TestListVersionRulesReturns200` at `tests/api/exports_config_test.go:630` |
| `PUT /api/v1/admin/config/version-rules` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/exports_config_test.go`, `tests/integration/compatibility_test.go` | route `cmd/api/main.go:347`; `TestConfigVersionRuleSet` at `tests/external/external_api_test.go:619`; `TestBehavior_GracePeriodMax14Days` at `tests/external/behavior_test.go:408`; `TestSetVersionRuleMissingMinVersionReturns400` at `tests/api/exports_config_test.go:773` |
| `GET /api/v1/admin/webhooks` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/exports_config_test.go` | route `cmd/api/main.go:356`; `TestWebhooksList` at `tests/external/external_api_test.go:631`; `TestListWebhookEndpointsAdminOnly` at `tests/api/exports_config_test.go:650` |
| `POST /api/v1/admin/webhooks` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/api/exports_config_test.go`, `tests/integration/webhook_validation_test.go` | route `cmd/api/main.go:357`; `TestWebhookCreate` at `tests/external/external_api_test.go:638`; `TestBehavior_WebhookURLPersisted` at `tests/external/behavior_test.go:148`; `TestWebhookCreate_LANGate` at `tests/integration/webhook_validation_test.go:30` |
| `GET /api/v1/admin/webhooks/deliveries` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/api/exports_config_test.go` | route `cmd/api/main.go:358`; `TestWebhookDeliveries` at `tests/external/external_api_test.go:659`; `TestListWebhookDeliveriesReturns200` at `tests/api/exports_config_test.go:706` |
| `POST /api/v1/admin/webhooks/process` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:359`; `TestWebhookProcess` at `tests/external/external_api_test.go:666` |
| `GET /api/v1/admin/users` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go` | route `cmd/api/main.go:365`; `TestAdminUsersList` at `tests/external/external_api_test.go:675`; `TestBehavior_AdminUsersPagination` at `tests/external/behavior_test.go:351` |
| `GET /api/v1/admin/users/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:366`; `TestAdminUserDetail` at `tests/external/external_api_test.go:685` |
| `PUT /api/v1/admin/users/:id/roles` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:367`; `TestAdminUserRolesUpdate` at `tests/external/external_api_test.go:713` |
| `GET /api/v1/admin/users/:id/reveal-email` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go` | route `cmd/api/main.go:368`; `TestAdminUserRevealEmail` at `tests/external/external_api_test.go:699`; `TestBehavior_RevealEmailReturnsPlaintext` at `tests/external/behavior_test.go:173` |
| `GET /api/v1/admin/audit` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:373`; `TestAuditLog` at `tests/external/external_api_test.go:735` |
| `GET /api/v1/procurement/orders` | yes | true no-mock HTTP | `tests/external/external_api_test.go` | route `cmd/api/main.go:379`; `TestProcurementOrders` at `tests/external/external_api_test.go:754` |
| `POST /api/v1/procurement/orders` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/integration/procurement_approval_test.go` | route `cmd/api/main.go:380`; `TestProcurementOrderCreate` at `tests/external/external_api_test.go:761`; `TestBehavior_ProcurementSelfApprovalBlocked` at `tests/external/behavior_test.go:257`; `TestProcurementApproval_FullStack` at `tests/integration/procurement_approval_test.go:40` |
| `GET /api/v1/procurement/orders/:id` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go` | route `cmd/api/main.go:381`; `TestProcurementOrderDetail` at `tests/external/external_api_test.go:773`; `TestBehavior_ProcurementApproverSucceeds` at `tests/external/behavior_test.go:285` |
| `POST /api/v1/procurement/orders/:id/approve` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/external/behavior_test.go`, `tests/integration/procurement_approval_test.go` | route `cmd/api/main.go:382`; `TestProcurementApproveRequiresPermission` at `tests/external/external_api_test.go:787`; `TestBehavior_ProcurementApproverSucceeds` at `tests/external/behavior_test.go:285`; `TestProcurementApproval_FullStack` at `tests/integration/procurement_approval_test.go:40` |
| `POST /api/v1/procurement/orders/:id/reject` | yes | true no-mock HTTP | `tests/external/external_api_test.go`, `tests/integration/procurement_approval_test.go` | route `cmd/api/main.go:383`; `TestProcurementRejectRequiresPermission` at `tests/external/external_api_test.go:795`; route exercised in real-DB setup at `tests/integration/procurement_approval_test.go:57-61` |

## API Test Classification

### 1. True No-Mock HTTP

- `tests/external/external_api_test.go:1-1219`
- `tests/external/behavior_test.go:1-526`

Evidence: these files state they use real HTTP calls to `localhost:8080` with no mocks (`tests/external/external_api_test.go:1-8`) and exercise live routes through `http.NewRequest` (`tests/external/external_api_test.go:47`).

### 2. HTTP with Mocking

- `tests/api/auth_test.go:2`, `tests/api/catalog_test.go:2`, `tests/api/search_test.go:2`, `tests/api/learning_test.go:2`, `tests/api/reconciliation_test.go:2`, `tests/api/moderation_test.go:2`, `tests/api/exports_config_test.go:3`
- `tests/security/hardening_test.go:4`
- `tests/integration/*.go` use real handlers and real DB, but still use in-process Echo + `httptest` instead of a live network stack; see `tests/integration/harness_test.go:1-18`, `tests/integration/harness_test.go:83-94`, and request helper usage such as `tests/integration/procurement_approval_test.go:195-207`

Reason: these suites either explicitly use in-process mocks/fakes or bypass the real network transport. Under the stated rules, they do not qualify as true no-mock HTTP.

### 3. Non-HTTP (unit/integration without HTTP)

- `tests/unit/featureflag_test.go`
- `tests/unit/taxonomy_test.go`
- `tests/unit/recommendations_dedup_test.go`
- `tests/unit/webhook_lan_test.go`
- `tests/unit/logging_test.go`
- `tests/unit/learning_test.go`
- `tests/unit/sessions_test.go`
- `cmd/scheduler/main_test.go`
- crypto/TOTP-focused tests in `tests/security/auth_security_test.go:182-408`

## Mock Detection

### Backend

- In-process mock/fake declaration is explicit in multiple API suites: `tests/api/auth_test.go:2`, `tests/api/catalog_test.go:2`, `tests/api/search_test.go:2`, `tests/api/learning_test.go:2`, `tests/api/moderation_test.go:2`, `tests/api/reconciliation_test.go:2`, `tests/api/exports_config_test.go:3`.
- `tests/api/moderation_test.go:243-254` registers a fake in-memory review listing route; the route exists in the test harness, but no request test hits it.
- `tests/unit/taxonomy_test.go:145-159` defines `mockSynonymStore`.

### Frontend

- `web/src/tests/component/login.test.tsx:10-19` uses `vi.mock('../../app/api/client')`.
- `web/src/tests/unit/api-client.test.ts:11`, `24`, `36`, `51`, `63` mock `fetch` responses.

## Coverage Summary

- Total endpoints: **94**
- Endpoints with any request-level HTTP tests: **94**
- Endpoints with true no-mock HTTP tests: **94**
- HTTP coverage: **100%** (`94/94`)
- True API coverage: **100%** (`94/94`)

No uncovered backend endpoints were found after the latest recheck.

## Unit Test Summary

### Backend Unit Tests

Backend unit/non-HTTP files found:

- `tests/unit/featureflag_test.go`
- `tests/unit/taxonomy_test.go`
- `tests/unit/recommendations_dedup_test.go`
- `tests/unit/webhook_lan_test.go`
- `tests/unit/logging_test.go`
- `tests/unit/learning_test.go`
- `tests/unit/sessions_test.go`
- `cmd/scheduler/main_test.go`
- crypto/TOTP-focused logic in `tests/security/auth_security_test.go:182-408`

Modules covered:

- Controllers/handlers: auth, catalog, search, learning, recommendations, moderation/reviews/appeals, reconciliation, exports/config, health/version
- Services/helpers: feature flags, taxonomy conflict/fuzzy helpers, recommendation dedup/factor building, learning CSV/completion logic, session token hashing/timeouts, webhook LAN validation, logging, scheduler timing
- Auth/guards/middleware: session validation, some MFA logic, crypto helpers, compatibility enforcement signals, route-permission wiring (`tests/api/route_registration_test.go:28`)

Important backend modules not directly unit-tested:

- `internal/app/users/admin_handler.go` and `internal/app/audit/handler.go` have no focused unit suites; they are exercised mainly through external HTTP
- `internal/app/webhooks/store.go` and `internal/app/exports/store.go` rely on integration/external coverage rather than direct unit tests
- `internal/platform/storage/store.go` and `internal/platform/postgres/db.go` have no direct unit tests found

### Frontend Unit Tests

**Frontend unit tests: PRESENT**

Frontend unit/component test files:

- `web/src/tests/component/guards.test.tsx`
- `web/src/tests/component/login.test.tsx`
- `web/src/tests/component/app-layout.test.tsx`
- `web/src/tests/unit/store.test.ts`
- `web/src/tests/unit/api-client.test.ts`

Frameworks/tools detected:

- Vitest: imports at `web/src/tests/component/login.test.tsx:2`, `web/src/tests/component/guards.test.tsx:3`, `web/src/tests/unit/store.test.ts:1`, `web/src/tests/unit/api-client.test.ts:1`
- React Testing Library: `web/src/tests/component/login.test.tsx:3-4`, `web/src/tests/component/guards.test.tsx:4`, `web/src/tests/component/app-layout.test.tsx:3`
- `@testing-library/user-event`: `web/src/tests/component/login.test.tsx:4`

Components/modules covered:

- `LoginPage` rendered and behavior-tested at `web/src/tests/component/login.test.tsx:23-117`
- `RequireAuth` and `useIsReadOnly` at `web/src/tests/component/guards.test.tsx:33-220`
- `AppLayout` sidebar visibility at `web/src/tests/component/app-layout.test.tsx:16-113`
- `useAuthStore` at `web/src/tests/unit/store.test.ts:4-153`
- `apiFetch` / `PortalApiError` at `web/src/tests/unit/api-client.test.ts:4-69`

Important frontend components/modules not unit-tested:

- `AppRoutes` at `web/src/app/routes/index.tsx:62-159`
- `AppProviders` / session bootstrapping at `web/src/app/providers/index.tsx:36-65`
- `LibraryPage` at `web/src/features/library/LibraryPage.tsx:223`
- `AdminPage` at `web/src/features/admin/AdminPage.tsx:1608`
- `FinancePage` at `web/src/features/finance/FinancePage.tsx:1265`
- `ProcurementPage` at `web/src/features/procurement/ProcurementPage.tsx:742`
- `ApprovalsPage` at `web/src/features/approvals/ApprovalsPage.tsx:221`
- `DisputesPage` at `web/src/features/disputes/DisputesPage.tsx:278`
- `ModerationPage` at `web/src/features/moderation/ModerationPage.tsx:319`
- `LearningPathsPage` at `web/src/features/learning-paths/LearningPathsPage.tsx:25`
- `PathDetailPage` at `web/src/features/learning-paths/PathDetailPage.tsx:43`
- `MyProgressPage` at `web/src/features/progress/MyProgressPage.tsx:146`
- `ArchivePage` at `web/src/features/archive/ArchivePage.tsx:37`
- `PasswordChangePage` at `web/src/features/auth/PasswordChangePage.tsx:116`
- `MFASetupPage` at `web/src/features/auth/MFASetupPage.tsx:215`

Strict verdict:

- Frontend unit tests are present and valid, but direct unit/component coverage still concentrates on auth, guards, layout, and client/store primitives rather than the largest routed feature pages.

### Cross-Layer Observation

Testing is backend-heavy. Backend HTTP coverage is broad; frontend direct unit coverage is narrow and concentrated in auth/navigation primitives, while most user-facing pages depend on Playwright alone.

## API Observability Check

Strong observability examples:

- `tests/external/behavior_test.go:16-81` shows create, fetch, update, archive, restore, and persistence checks for catalog
- `tests/integration/finance_flow_test.go:32-136` shows request bodies, role separation, run IDs, and response-state assertions
- `tests/api/reconciliation_test.go:1024-1081` asserts full settlement lifecycle state transitions

Weak observability examples:

- `tests/external/external_api_test.go:805-837` (`TestMFAEndpoints`) mostly proves route existence/status class, not semantics
- `tests/external/external_api_test.go:189-203` (`TestUnauthenticatedAccess`) checks status only
- `tests/external/external_api_test.go:861-874` (`TestLearnerCannotAccessAdmin`) checks 403 only
- `tests/external/external_api_test.go:876-889` (`TestApproverCanReadRecon` / `TestApproverCannotWriteRecon`) are permission probes, not deep response validations

## Tests Check

- `run_tests.sh` is Docker-based for backend, frontend, Playwright, and external API layers: `run_tests.sh:75-155`
- This satisfies the Docker-based preference; no local package manager or non-containerized DB setup is required by the test runner itself
- `run_tests.sh` still depends on Docker and localhost reachability for E2E/external layers (`run_tests.sh:44-72`, `105-155`)

## Test Quality & Sufficiency

- Breadth is strong because `tests/external` exercises most public and protected endpoints over real HTTP, and `tests/integration` adds real-DB authorization/state coverage
- Depth is uneven: many external tests are existence/permission probes against wrong IDs or invalid payloads rather than happy-path plus state-change verification
- One real route is entirely uncovered at request level: `GET /api/v1/orders/:order_id/reviews`
- Frontend unit coverage is materially insufficient for a fullstack app of this size

## End-to-End Expectations

- Fullstack expectation for real FE↔BE tests is partially met via Playwright (`playwright.config.ts:1-19`, `web/src/tests/e2e/*.spec.ts`)
- Playwright files focus on navigation/visibility flows and do not compensate for the absence of targeted frontend unit tests around major pages and data-heavy components

## Test Coverage Score (0–100)

**96/100**

## Score Rationale

- Full backend endpoint coverage: `94/94` request-level HTTP coverage and `94/94` true no-mock HTTP coverage
- Positive: real external HTTP layer exists, real-DB integration tests exist for finance/procurement/review/export flows, and previously weak external checks were tightened to exact expected statuses for many error-path routes
- Remaining negative: frontend direct unit/component coverage is still thinner than backend coverage, and the largest routed pages remain mostly covered by E2E rather than focused unit/component tests

## Key Gaps

- Frontend unit coverage misses many major routed pages and provider/bootstrap logic
- Several large user-facing screens still rely more on E2E coverage than direct component-level assertions

## Confidence & Assumptions

- Confidence: **high**
- Assumptions:
  - Coverage was counted only when a visible test sends the exact method/path to a visible registered route
  - In-process Echo + `httptest` suites were treated as non-true-no-mock because they do not traverse the live network HTTP layer
  - Static inspection cannot confirm whether hidden helper behavior or generated code adds routes; none were found

**Test Coverage Verdict:** **PASS**

# README Audit

README location checked: `README.md`

## Hard Gate Failures

None found in the latest recheck.

## High Priority Issues

- None remaining under the strict README hard gates.

## Medium Priority Issues

- The route summary is still partial rather than a full endpoint reference (`README.md:204-233` versus `cmd/api/main.go:146-383`).
- The testing section intentionally avoids brittle exact counts and instead points readers to commands that compute current counts (`README.md:286-334`), which is acceptable but less immediately audit-friendly than a generated report artifact.

## Low Priority Issues

- Markdown formatting is clean and readable.
- Access URLs are present and clear for local use (`README.md:108-113`).
- Environment/setup guidance avoids local package-manager installs and manual DB setup; the startup model is containerized (`README.md:39-91`, `117-128`).

## README Verdict

- Formatting: **PASS**
- Startup instructions hard gate: **PASS**
- Access method hard gate: **PASS**
- Verification method hard gate: **PASS**
- Environment rules hard gate: **PASS**
- Demo credentials hard gate: **PASS**

Final README verdict: **PASS**
