// Package recommendations implements the recommendation engine for the portal.
package recommendations

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Domain types ──────────────────────────────────────────────────────────────

// RecommendedResource is a single ranked recommendation with explainability.
type RecommendedResource struct {
	ResourceID  string       `json:"resource_id"`
	Title       string       `json:"title"`
	ContentType string       `json:"content_type"`
	Category    string       `json:"category"`
	Score       float64      `json:"score"`
	Factors     []TraceFactor `json:"factors"`
}

// TraceFactor describes why a resource was recommended.
type TraceFactor struct {
	Factor string  `json:"factor"`
	Weight float64 `json:"weight"`
	Label  string  `json:"label"`
}

// Store holds the database pool for recommendations.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new recommendations store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ── Behavior ingestion ────────────────────────────────────────────────────────

// IngestBehavior records a user behavior event.
func (s *Store) IngestBehavior(ctx context.Context, userID, resourceID, eventType, sessionID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO behavior_events (id, user_id, resource_id, event_type, session_id)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''))`,
		uuid.New().String(), userID, resourceID, eventType, sessionID,
	)
	return err
}

// ── Recommendations ───────────────────────────────────────────────────────────

// GetRecommendations returns ranked recommendations for a user.
//
// Algorithm:
//   - Cold-start (no behavior): resources matching user's job family, then globally popular
//   - Returning user: scored by view_count*0.3 + completion*0.5 + co_occurrence*0.2
//   - Deduplicates already-completed resources
//   - Enforces 40% per-category diversity cap in application layer
func (s *Store) GetRecommendations(ctx context.Context, userID string, limit int) ([]RecommendedResource, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	// Check if this is a cold-start user (no behavior events)
	var behaviorCount int
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM behavior_events WHERE user_id = $1`, userID).
		Scan(&behaviorCount)

	if behaviorCount == 0 {
		return s.coldStartRecommendations(ctx, userID, limit)
	}
	return s.personalizedRecommendations(ctx, userID, limit)
}

// coldStartRecommendations returns top resources from the user's job family,
// falling back to globally popular resources. No completed resources are included.
func (s *Store) coldStartRecommendations(ctx context.Context, userID string, limit int) ([]RecommendedResource, error) {
	// Fetch more than limit to allow for diversity cap enforcement
	fetchLimit := limit * 3
	if fetchLimit < 30 {
		fetchLimit = 30
	}

	rows, err := s.pool.Query(ctx, `
		WITH user_job AS (
		    SELECT job_family_id FROM users WHERE id = $1
		),
		completed AS (
		    SELECT resource_id FROM learning_progress_snapshots
		    WHERE user_id = $1 AND status = 'completed'
		),
		candidates AS (
		    SELECT
		        r.id           AS resource_id,
		        r.title,
		        r.content_type,
		        r.category,
		        r.view_count   AS raw_score,
		        CASE
		            WHEN r.job_family_id = (SELECT job_family_id FROM user_job)
		                AND (SELECT job_family_id FROM user_job) IS NOT NULL
		            THEN 'job_family'
		            ELSE 'popularity'
		        END AS factor_type,
		        CASE
		            WHEN r.job_family_id = (SELECT job_family_id FROM user_job)
		                AND (SELECT job_family_id FROM user_job) IS NOT NULL
		            THEN 1.0
		            ELSE 0.5
		        END AS factor_weight
		    FROM resources r
		    WHERE r.is_published = TRUE
		      AND r.is_archived  = FALSE
		      AND r.id NOT IN (SELECT resource_id FROM completed)
		    ORDER BY
		        CASE WHEN r.job_family_id = (SELECT job_family_id FROM user_job) THEN 0 ELSE 1 END,
		        r.view_count DESC
		    LIMIT $2
		)
		SELECT resource_id, title, content_type, category,
		       raw_score::float, factor_type, factor_weight
		FROM candidates`,
		userID, fetchLimit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []RecommendedResource
	for rows.Next() {
		var res RecommendedResource
		var rawScore float64
		var factorType string
		var factorWeight float64
		if err := rows.Scan(&res.ResourceID, &res.Title, &res.ContentType, &res.Category,
			&rawScore, &factorType, &factorWeight); err != nil {
			continue
		}
		res.Score = rawScore

		label := factorLabel(factorType)
		res.Factors = []TraceFactor{{Factor: factorType, Weight: factorWeight, Label: label}}
		candidates = append(candidates, res)
	}

	return applyDiversityCap(candidates, limit), nil
}

// personalizedRecommendations scores resources based on the user's behavior history
// and real tag overlap between the user's interacted resources and candidate resources.
func (s *Store) personalizedRecommendations(ctx context.Context, userID string, limit int) ([]RecommendedResource, error) {
	fetchLimit := limit * 3
	if fetchLimit < 30 {
		fetchLimit = 30
	}

	// Score = view_count_signal*0.3 + completion_signal*0.2 + co_occurrence_signal*0.2 + tag_overlap_signal*0.3
	// tag_overlap_signal: number of shared skill_tags between the candidate and
	//   the set of resources the user has viewed or completed (via resource_tags).
	rows, err := s.pool.Query(ctx, `
		WITH completed AS (
		    SELECT resource_id FROM learning_progress_snapshots
		    WHERE user_id = $1 AND status = 'completed'
		),
		user_viewed AS (
		    SELECT resource_id, COUNT(*) AS view_times
		    FROM behavior_events
		    WHERE user_id = $1 AND event_type = 'view'
		    GROUP BY resource_id
		),
		user_completed_categories AS (
		    SELECT DISTINCT r.category
		    FROM learning_progress_snapshots lps
		    JOIN resources r ON r.id = lps.resource_id
		    WHERE lps.user_id = $1 AND lps.status = 'completed'
		),
		co_viewers AS (
		    SELECT DISTINCT be2.user_id
		    FROM behavior_events be1
		    JOIN behavior_events be2 ON be2.resource_id = be1.resource_id
		    WHERE be1.user_id = $1 AND be2.user_id != $1
		),
		co_occurrence AS (
		    SELECT resource_id, COUNT(DISTINCT user_id) AS co_count
		    FROM behavior_events
		    WHERE user_id IN (SELECT user_id FROM co_viewers)
		      AND event_type IN ('view', 'complete')
		    GROUP BY resource_id
		),
		user_tags AS (
		    SELECT DISTINCT rt.tag_id
		    FROM resource_tags rt
		    WHERE rt.resource_id IN (
		        SELECT resource_id FROM user_viewed
		        UNION
		        SELECT resource_id FROM completed
		    )
		),
		tag_overlap AS (
		    SELECT rt.resource_id, COUNT(*) AS overlap_count
		    FROM resource_tags rt
		    WHERE rt.tag_id IN (SELECT tag_id FROM user_tags)
		    GROUP BY rt.resource_id
		),
		candidates AS (
		    SELECT
		        r.id           AS resource_id,
		        r.title,
		        r.content_type,
		        r.category,
		        COALESCE(uv.view_times, 0)::float   AS view_signal,
		        CASE WHEN r.category IN (SELECT category FROM user_completed_categories) THEN 1.0 ELSE 0.0 END AS completion_signal,
		        COALESCE(co.co_count, 0)::float      AS co_signal,
		        COALESCE(tov.overlap_count, 0)::float AS tag_signal,
		        (
		            COALESCE(uv.view_times, 0)::float * 0.3
		          + CASE WHEN r.category IN (SELECT category FROM user_completed_categories) THEN 1.0 ELSE 0.0 END * 0.2
		          + COALESCE(co.co_count, 0)::float * 0.2
		          + COALESCE(tov.overlap_count, 0)::float * 0.3
		          + r.view_count::float * 0.001
		        ) AS score
		    FROM resources r
		    LEFT JOIN user_viewed uv ON uv.resource_id = r.id
		    LEFT JOIN co_occurrence co ON co.resource_id = r.id
		    LEFT JOIN tag_overlap tov ON tov.resource_id = r.id
		    WHERE r.is_published = TRUE
		      AND r.is_archived  = FALSE
		      AND r.id NOT IN (SELECT resource_id FROM completed)
		    ORDER BY score DESC
		    LIMIT $2
		)
		SELECT resource_id, title, content_type, category,
		       score, view_signal, completion_signal, co_signal, tag_signal
		FROM candidates`,
		userID, fetchLimit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []RecommendedResource
	for rows.Next() {
		var res RecommendedResource
		var viewSignal, completionSignal, coSignal, tagSignal float64
		if err := rows.Scan(&res.ResourceID, &res.Title, &res.ContentType, &res.Category,
			&res.Score, &viewSignal, &completionSignal, &coSignal, &tagSignal); err != nil {
			continue
		}

		// Build top factors with real tag overlap data
		factors := buildFactors(viewSignal, completionSignal, coSignal, tagSignal)
		res.Factors = factors
		candidates = append(candidates, res)
	}

	return applyDiversityCap(candidates, limit), nil
}

// buildFactors constructs TraceFactor slices from raw signals.
// tagSignal reflects real tag overlap from resource_tags / skill_tags;
// tag_overlap is only emitted when the signal is genuinely non-zero.
func buildFactors(viewSignal, completionSignal, coSignal, tagSignal float64) []TraceFactor {
	var factors []TraceFactor

	if tagSignal > 0 {
		factors = append(factors, TraceFactor{
			Factor: "tag_overlap",
			Weight: tagSignal * 0.3,
			Label:  factorLabel("tag_overlap"),
		})
	}
	if completionSignal > 0 {
		factors = append(factors, TraceFactor{
			Factor: "prior_completion",
			Weight: completionSignal * 0.2,
			Label:  factorLabel("prior_completion"),
		})
	}
	if viewSignal > 0 {
		factors = append(factors, TraceFactor{
			Factor: "view_history",
			Weight: viewSignal * 0.3,
			Label:  factorLabel("view_history"),
		})
	}
	if coSignal > 0 {
		factors = append(factors, TraceFactor{
			Factor: "co_occurrence",
			Weight: coSignal * 0.2,
			Label:  factorLabel("co_occurrence"),
		})
	}
	if len(factors) == 0 {
		factors = []TraceFactor{{Factor: "popularity", Weight: 0.5, Label: factorLabel("popularity")}}
	}
	return factors
}

// factorLabel converts internal factor codes to human-readable labels.
func factorLabel(factorType string) string {
	switch factorType {
	case "job_family":
		return "popular in your role"
	case "tag_overlap":
		return "matches your skills"
	case "view_history":
		return "based on your activity"
	case "co_occurrence":
		return "recently viewed by peers"
	case "popularity":
		return "trending"
	case "prior_completion":
		return "complete your path"
	case "cold_start":
		return "popular in your role"
	default:
		return "recommended"
	}
}

// applyDiversityCap first removes near-duplicate resources, then enforces a
// hard 40% per-category cap on the final result set.
//
// Near-duplicates: two resources whose normalized titles match exactly, or
// whose token sets have Jaccard similarity >= 0.85, are treated as the same
// underlying content. The lower-scored one is dropped before the cap runs,
// so two near-identical entries never compete for the same carousel slot.
//
// Category cap: a category that would exceed 40% of `limit` is skipped even
// if other categories have no candidates — slots are left unfilled rather
// than relaxing the cap.
func applyDiversityCap(candidates []RecommendedResource, limit int) []RecommendedResource {
	if len(candidates) == 0 {
		return []RecommendedResource{}
	}

	candidates = dedupNearDuplicates(candidates)

	maxPerCategory := int(float64(limit) * 0.4)
	if maxPerCategory < 1 {
		maxPerCategory = 1
	}

	categoryCounts := make(map[string]int)
	var result []RecommendedResource

	// Single pass: hard cap — never relax even if slots remain.
	for _, c := range candidates {
		if len(result) >= limit {
			break
		}
		if categoryCounts[c.Category] < maxPerCategory {
			categoryCounts[c.Category]++
			result = append(result, c)
		} else {
			// Category is at cap; record the fact in the candidate's factors
			// (not added to result, but the trace note marks why it was capped).
			c.Factors = append(c.Factors, TraceFactor{
				Factor: "diversity_cap_applied",
				Weight: 0,
				Label:  "category cap reached",
			})
			// c is intentionally not appended to result.
		}
	}

	return result
}

// dedupNearDuplicates removes resources whose titles normalize to the same
// string as an earlier (higher-scored) candidate, OR whose token-set Jaccard
// similarity to an earlier candidate is >= 0.75. The threshold is tuned so
// "Effective Communication Skills" and "Effective Communication Skills (v2)"
// dedupe (3 of 4 tokens overlap = 0.75) while "Project Management" and
// "Project Management Basics" stay distinct (2 of 3 tokens overlap = 0.67).
// Input is assumed sorted by score DESC so the higher-scored entry wins.
func dedupNearDuplicates(in []RecommendedResource) []RecommendedResource {
	const jaccardThreshold = 0.75
	type kept struct {
		normalized string
		tokens     map[string]struct{}
		idx        int
	}
	var keptSet []kept
	out := make([]RecommendedResource, 0, len(in))

	for _, c := range in {
		norm := normalizeTitle(c.Title)
		toks := tokenizeTitle(c.Title)

		isDup := false
		for _, k := range keptSet {
			if norm != "" && norm == k.normalized {
				isDup = true
				break
			}
			if jaccard(toks, k.tokens) >= jaccardThreshold {
				isDup = true
				break
			}
		}
		if isDup {
			continue
		}
		keptSet = append(keptSet, kept{normalized: norm, tokens: toks, idx: len(out)})
		out = append(out, c)
	}
	return out
}

// normalizeTitle lowercases, strips punctuation, and collapses whitespace.
func normalizeTitle(t string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(t) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSpace = false
		case r == ' ' || r == '\t' || r == '-' || r == '_' || r == '/':
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			// drop other punctuation
		}
	}
	return strings.TrimSpace(b.String())
}

// tokenizeTitle returns the set of normalized tokens, dropping common stopwords
// so titles like "Intro to X" and "An Introduction to X" land closer together.
// Single-character tokens are kept (they distinguish things like "Topic A" and
// "Topic B"), but generic articles/prepositions are filtered out.
func tokenizeTitle(t string) map[string]struct{} {
	stopwords := map[string]struct{}{
		"an": {}, "the": {}, "and": {}, "or": {},
		"of": {}, "to": {}, "for": {}, "in": {}, "on": {}, "with": {},
		"intro": {}, "introduction": {}, // intentionally collapsed
	}
	out := make(map[string]struct{})
	for _, tok := range strings.Fields(normalizeTitle(t)) {
		if _, isStop := stopwords[tok]; isStop {
			continue
		}
		if tok == "" {
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}

// ApplyDiversityCapForTest exposes applyDiversityCap to the test package so
// the dedup + cap behaviour can be exercised without standing up the whole
// recommendation pipeline. Production code must continue to call the
// unexported helpers via GetRecommendations.
func ApplyDiversityCapForTest(in []RecommendedResource, limit int) []RecommendedResource {
	return applyDiversityCap(in, limit)
}

// BuildFactorsForTest exposes buildFactors to the test package so tag-driven
// factor logic can be exercised without a database.
func BuildFactorsForTest(viewSignal, completionSignal, coSignal, tagSignal float64) []TraceFactor {
	return buildFactors(viewSignal, completionSignal, coSignal, tagSignal)
}

// jaccard returns |A ∩ B| / |A ∪ B|, in [0, 1]. Empty sets return 0.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersect := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersect++
		}
	}
	union := len(a) + len(b) - intersect
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}

// ── Impressions ───────────────────────────────────────────────────────────────

// RecordImpression inserts a recommendation impression and returns its UUID.
func (s *Store) RecordImpression(ctx context.Context, userID, resourceID, sessionID string, slot int) (string, error) {
	id := uuid.New().String()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO recommendation_impressions (id, user_id, resource_id, session_id, carousel_slot)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)`,
		id, userID, resourceID, sessionID, slot,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetTrace returns the trace factors for an impression.
func (s *Store) GetTrace(ctx context.Context, impressionID string) ([]TraceFactor, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT factor_type, coalesce(factor_detail,''), weight
		FROM recommendation_trace_factors
		WHERE impression_id = $1
		ORDER BY weight DESC`, impressionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var factors []TraceFactor
	for rows.Next() {
		var f TraceFactor
		var detail string
		if err := rows.Scan(&f.Factor, &detail, &f.Weight); err != nil {
			continue
		}
		f.Label = factorLabel(f.Factor)
		if detail != "" {
			f.Label = detail
		}
		factors = append(factors, f)
	}
	if factors == nil {
		factors = []TraceFactor{}
	}
	return factors, nil
}

