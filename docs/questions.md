## Business Logic Questions Log

### 1. What does "fully offline" mean for this React + Go + PostgreSQL product?
- **Problem:** The prompt requires a fully offline environment but also mentions cross-device sync, LAN-based webhooks, and local recommendations.
- **My Understanding:** The platform must run entirely on a local network with no dependency on cloud identity, cloud search, SaaS queues, or hosted storage.
- **Solution:** Keep authentication, search indexing, scheduled jobs, file storage, exports, webhook delivery, recommendations, and audit data fully local. LAN-based webhooks are allowed only to other systems reachable inside the same offline network. :contentReference[oaicite:0]{index=0}

---

### 2. What is the correct authentication and session model for a decoupled React frontend?
- **Problem:** The prompt requires local username/password login, optional TOTP MFA, and configurable idle and maximum session timeouts, but it does not dictate JWT versus server-side sessions.
- **My Understanding:** The system needs revocable, timeout-aware sessions that are easy to reason about in a fully local deployment.
- **Solution:** Use server-issued opaque session tokens stored in HttpOnly cookies and persisted in PostgreSQL, with optional TOTP challenge completion before privilege elevation. Enforce 15-minute idle timeout and 8-hour absolute timeout from the server side.

---

### 3. How should optional MFA behave across different roles?
- **Problem:** The prompt says MFA is optional, but it does not say whether it is optional per user, per role, or globally.
- **My Understanding:** MFA should be supported for any user, but rollout may need to differ by role.
- **Solution:** Implement per-user TOTP enrollment and verification, then expose role-based rollout controls in the configuration center so administrators can keep it optional overall while requiring it for specific roles if needed.

---

### 4. What exactly is included in the unified library search surface?
- **Problem:** Learners can search a unified library of internal resources and job/skill tags, but the prompt does not define whether tags are first-class results or only filters.
- **My Understanding:** The search surface should primarily return learning resources while using taxonomy entities to expand, explain, and refine results.
- **Solution:** Return resources as the main result objects, but index job and skill tags, synonyms, and pinyin aliases as searchable metadata that can drive suggestions, filters, and recommendation explanations.

---

### 5. How should typo tolerance, synonym matching, and pinyin matching work together?
- **Problem:** The prompt requires all three search behaviors, but it does not define whether synonym and pinyin matching are always on or user-selectable.
- **My Understanding:** Exact and high-confidence local matches should come first, while broader expansions should remain transparent and controllable.
- **Solution:** Build one local index that supports exact, full-text, trigram fuzzy, synonym, and pinyin-expanded terms. Surface synonym and pinyin matching as optional query toggles in the UI, and record which expansions contributed to each result for explainability.

---

### 6. What counts as a near-duplicate resource for deduplication?
- **Problem:** The prompt requires deduplication across near-duplicate resources, but it does not define the fingerprinting rule.
- **My Understanding:** Deduplication should collapse obviously repeated content without hiding legitimately distinct editions or formats.
- **Solution:** Use normalized title, source identifier, publish date, canonical job/skill tags, and content checksum or source fingerprint where available. Keep duplicate groups reviewable by moderators and administrators instead of silently deleting records.

---

### 7. How should the recommendation diversity rule be enforced?
- **Problem:** The prompt says no more than 40% of a recommendation carousel can come from a single category, but it does not define the behavior when the candidate pool is small.
- **My Understanding:** Diversity is mandatory when enough inventory exists, but the UI should still show useful recommendations when the catalog is sparse.
- **Solution:** Apply the 40% cap whenever the candidate pool can satisfy it. If the pool is too narrow, fill the remaining slots with the best available items and store a trace note explaining why the cap could not be fully met.

---

### 8. How should "why recommended" be implemented?
- **Problem:** The prompt requires clear recommendation cues, but it does not define whether they are generic badges or stored rule traces.
- **My Understanding:** The explanation must be traceable and specific enough for users and reviewers to understand ranking decisions.
- **Solution:** Persist top contributing factors for each generated recommendation impression, such as matching job family, overlapping tags, prior completions, or popularity fallback. Expose short user-facing badges in the UI and fuller traces to admins.

---

### 9. How should learning path completion rules work for required and elective items?
- **Problem:** The prompt gives an example of "6 required items plus any 2 electives" but does not say whether those counts are fixed across all paths.
- **My Understanding:** Each learning path should define its own rule set while following the same evaluation model.
- **Solution:** Store learning-path rule definitions explicitly: required item set, elective pool, and minimum elective completions. Progress is complete only when all required items are complete and the elective threshold is met.

---

### 10. What does resume learning and cross-device sync mean in a local network?
- **Problem:** The prompt promises resume learning and cross-device sync, but it does not define whether this is browser-local state sync or server-backed synchronization.
- **My Understanding:** Devices on the same local network should converge through the shared backend, not through peer-to-peer browser tricks.
- **Solution:** Persist progress events, last position markers, and completion states in PostgreSQL. Every device reads and writes against the same local API so resume state follows the user across devices connected to the local deployment.

---

### 11. What should be included in the downloadable CSV learning-record export?
- **Problem:** The prompt requires CSV export of learning records, but it does not specify whether the export is user-facing only or also administrative.
- **My Understanding:** Learners need self-service exports, while admins may also need broader exports later.
- **Solution:** Implement a learner-scoped CSV export first as a prompt requirement, containing path enrollments, resource progress, completion timestamps, and earned statuses. Keep broader administrative exports separate so learner export permissions stay simple and safe.

---

### 12. How do procurement reviews, merchant replies, appeals, and arbitration fit the listed roles?
- **Problem:** The workflow requires merchant replies and arbitration outcomes, but the prompt does not define a merchant login role or a dedicated arbitrator role.
- **My Understanding:** Merchant replies must be recorded internally, and arbitration authority must stay within the listed organization roles.
- **Solution:** Procurement Specialists can record merchant replies as official correspondence artifacts linked to an order or dispute. Approvers and Finance Analysts can participate in arbitration decisions, while Content Moderators apply the resulting display state for reviews.

---

### 13. What are the exact role boundaries between Procurement Specialists, Approvers, Finance Analysts, Content Moderators, and System Administrators?
- **Problem:** The prompt lists several operational roles but does not describe each permission boundary in detail.
- **My Understanding:** The system needs strict separation so users only see the menus, pages, data fields, and actions required by their operational role.
- **Solution:** Learners browse, enroll, and export only their own learning data. Procurement Specialists manage orders, reviews, and merchant correspondence. Approvers decide approval and arbitration checkpoints. Finance Analysts own reconciliation, write-off approval, and settlement actions. Content Moderators manage review visibility and remediation queues. System Administrators manage users, permissions, configuration, compatibility rules, taxonomy governance, and explicit access to unmasked data.

---

### 14. How should the taxonomy support hierarchy, synonym consolidation, and conflict detection?
- **Problem:** The prompt requires hierarchical relationships, synonym consolidation, and conflict detection, but it does not define which conflicts are blocking versus informational.
- **My Understanding:** A taxonomy conflict becomes blocking when it would make search or recommendation behavior contradictory.
- **Solution:** Treat conflicting active synonyms pointing to different canonical tags as a blocking validation error. Route them into an admin review queue with full audit trails and require resolution before publication.

---

### 15. How should search indexing be stored and refreshed locally?
- **Problem:** The prompt requires local tokenization, weighting, fuzzy matching, nightly rebuilds, and incremental updates, but it does not prescribe the storage layout.
- **My Understanding:** Search must remain fast, inspectable, and fully local without introducing a second external search product.
- **Solution:** Use PostgreSQL-backed search documents and token tables with trigram and full-text indexes, plus locally generated normalized, synonym, and pinyin forms. Rebuild nightly through a scheduled job and update incrementally whenever content or taxonomy records change.

---

### 16. What does "archive pages are auto-generated by month and tag" mean in a React application?
- **Problem:** The prompt calls for auto-generated archive pages but does not define whether these are static files or dynamic routes.
- **My Understanding:** The important requirement is persistent archive browseability, not a static-site build pipeline.
- **Solution:** Generate and refresh archive browse records and counts in the backend, then expose them through dynamic React routes by month and tag. This preserves the archive requirement without introducing a separate static publishing system.

---

### 17. How should configurable warehouse and transportation billing rules work?
- **Problem:** The prompt requires configurable billing rules and settlement ledgers, but it does not define whether rules are global, vendor-specific, or department-specific.
- **My Understanding:** Billing rules need versioned configuration and scoped overrides.
- **Solution:** Model billing rules with effective dates and scope dimensions for vendor, warehouse, transportation mode, department, and cost center. Settlement runs use the version active at the time of calculation and record which rule version was applied.

---

### 18. How should the variance write-off threshold behave?
- **Problem:** The prompt gives an example that variances under $5.00 auto-suggest write-off but still require Finance approval, but it does not define whether currency or rounding is configurable.
- **My Understanding:** Variance handling must be deterministic and auditable.
- **Solution:** Store all monetary values in integer minor units for one configured base currency. Thresholds are configurable in the configuration center, suggestions are generated automatically, and Finance approval remains mandatory before any write-off is applied.

---

### 19. How should cost allocation by department and cost center be represented?
- **Problem:** The prompt requires cost allocation but does not say whether a settlement line may split across multiple departments or cost centers.
- **My Understanding:** Real reconciliation often needs split allocation.
- **Solution:** Allow settlement lines to carry one or more allocation rows whose percentages or amounts must sum exactly to the parent line. Validate that allocation totals reconcile before posting AR or AP entries.

---

### 20. What should the offline export targets look like?
- **Problem:** The prompt requires exports compatible with downstream finance systems via offline file drop or LAN-based webhooks, but it does not say whether one export path can substitute for the other.
- **My Understanding:** Both delivery modes are part of the requirement because different offline installations may depend on either one.
- **Solution:** Implement both a file-drop outbox directory and a LAN webhook dispatcher. Every export records payload checksum, delivery target, attempt history, retry state, and final disposition.

---

### 21. How should scheduled jobs with retry and compensation be applied across the system?
- **Problem:** The prompt names scheduled jobs with retry and compensation, but it does not list which domains require them.
- **My Understanding:** Retry and compensation should exist wherever local background work can partially fail and leave user-visible inconsistencies.
- **Solution:** Use scheduled jobs for search rebuilds, archive refreshes, export delivery, webhook retries, settlement generation, and cleanup of expired read-only compatibility windows. Each job type defines retry policy, failure recording, and compensation or rollback behavior where partial side effects exist.

---

### 22. How should app version compatibility and the 14-day read-only grace window work in a browser-based product?
- **Problem:** The prompt requires blocking unsupported clients while allowing read-only access for up to 14 days, but a React SPA does not naturally identify itself the same way as a native app.
- **My Understanding:** The frontend build must present a verifiable version string on every API request.
- **Solution:** Stamp each frontend build with a version identifier and send it with API requests. The backend compares that version against compatibility rules in the configuration center and returns full access, read-only grace access, or blocked status with the grace-window deadline.

---

### 23. What upload policy should apply to review images and dispute evidence?
- **Problem:** The prompt requires text and image attachments plus appeal evidence uploads, but it does not define file limits or allowed types.
- **My Understanding:** The repo needs safe, explicit defaults that stay local and auditable.
- **Solution:** Use conservative configurable defaults: review attachments accept JPG and PNG images, and dispute evidence accepts PDF, JPG, and PNG. Validate MIME type, file signature, file size, checksum, and per-record count limits in the backend, and store files locally with download authorization checks.

---

### 24. How should sensitive-data masking behave outside the administrator role?
- **Problem:** The prompt requires masking personal data in non-admin views, but it does not define whether Finance, Moderation, and Procurement roles ever see unmasked values.
- **My Understanding:** Masking is the default across all non-admin roles, with no implicit exceptions.
- **Solution:** Mask learner identity details, recovery material, and sensitive dispute metadata by default in all non-admin APIs and UI views. Only System Administrators can reveal full values, and every reveal action is access-logged with actor, reason, and timestamp.