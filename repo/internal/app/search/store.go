package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"portal/internal/app/catalog"
)

type Query struct {
	Q           string
	UseSynonyms bool
	UsePinyin   bool
	UseFuzzy    bool
	Category    string
	ContentType string
	TagCode     string   // single-tag legacy param
	TagCodes    []string // multi-tag param; all listed tags must match
	FromDate    string
	ToDate      string
	Sort        string // relevance, popular, recent
	Limit       int
	Offset      int
}

type Result struct {
	catalog.Resource
	Rank            float64  `json:"rank"`
	MatchedTerms    []string `json:"matched_terms,omitempty"`
	MatchedSynonyms []string `json:"matched_synonyms,omitempty"`
}

// TaxonomyHit surfaces skill tags whose canonical name or active synonyms
// match the search query, providing a "discovery" dimension alongside the
// resource results. This is what makes the search "unified" — learners can
// find job families/tags, not only resources.
type TaxonomyHit struct {
	TagID         int64    `json:"tag_id"`
	Code          string   `json:"code"`
	CanonicalName string   `json:"canonical_name"`
	ResourceCount int      `json:"resource_count"`
	MatchedVia    string   `json:"matched_via"` // "canonical" or "synonym:<text>"
}

type SearchResponse struct {
	Results          []Result       `json:"results"`
	Total            int            `json:"total"`
	Limit            int            `json:"limit"`
	Offset           int            `json:"offset"`
	ExpandedSynonyms []string       `json:"expanded_synonyms,omitempty"`
	ExpandedPinyin   []string       `json:"expanded_pinyin,omitempty"`
	TaxonomyHits     []TaxonomyHit  `json:"taxonomy_hits,omitempty"`
}

type ArchiveBucket struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Search executes a local full-text + trigram search over indexed resources.
// It queries search_documents as the primary source when available,
// falling back to a direct resource query when the index is empty.
func (s *Store) Search(ctx context.Context, q Query) (*SearchResponse, error) {
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 20
	}

	var expandedSynonyms, expandedPinyin []string

	queryTerms := []string{}
	if q.Q != "" {
		queryTerms = append(queryTerms, q.Q)
	}

	if q.UseSynonyms && q.Q != "" {
		synonyms, _ := s.expandSynonyms(ctx, q.Q)
		expandedSynonyms = synonyms
		queryTerms = append(queryTerms, synonyms...)
	}
	if q.UsePinyin && q.Q != "" {
		pinyin, _ := s.expandPinyin(ctx, q.Q)
		expandedPinyin = pinyin
		queryTerms = append(queryTerms, pinyin...)
	}

	// Check whether search_documents has been populated.
	var docCount int
	_ = s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM search_documents").Scan(&docCount)
	if docCount == 0 {
		// Fall back to direct resource query when index is empty.
		resp, err := s.searchDirect(ctx, q, queryTerms)
		if err != nil {
			return nil, err
		}
		resp.ExpandedSynonyms = expandedSynonyms
		resp.ExpandedPinyin = expandedPinyin
		return resp, nil
	}

	// ── Phase 1: rank candidates from search_documents ────────────────────────
	// Build filter args for resource-level filters applied after join.
	sdArgs := []any{}
	sdArgN := 1

	// Resource-level filters (applied via join to resources).
	resourceFilters := ""
	if q.Category != "" {
		resourceFilters += fmt.Sprintf(" AND r.category = $%d", sdArgN)
		sdArgs = append(sdArgs, q.Category)
		sdArgN++
	}
	if q.ContentType != "" {
		resourceFilters += fmt.Sprintf(" AND r.content_type = $%d", sdArgN)
		sdArgs = append(sdArgs, q.ContentType)
		sdArgN++
	}
	if q.TagCode != "" {
		resourceFilters += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM resource_tags rt2 JOIN skill_tags st2 ON st2.id = rt2.tag_id
			WHERE rt2.resource_id = r.id AND st2.code = $%d)`, sdArgN)
		sdArgs = append(sdArgs, q.TagCode)
		sdArgN++
	}
	for _, code := range q.TagCodes {
		resourceFilters += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM resource_tags rt3 JOIN skill_tags st3 ON st3.id = rt3.tag_id
			WHERE rt3.resource_id = r.id AND st3.code = $%d)`, sdArgN)
		sdArgs = append(sdArgs, code)
		sdArgN++
	}
	if q.FromDate != "" {
		resourceFilters += fmt.Sprintf(" AND r.publish_date >= $%d::date", sdArgN)
		sdArgs = append(sdArgs, q.FromDate)
		sdArgN++
	}
	if q.ToDate != "" {
		resourceFilters += fmt.Sprintf(" AND r.publish_date <= $%d::date", sdArgN)
		sdArgs = append(sdArgs, q.ToDate)
		sdArgN++
	}

	var rankExpr, textFilter string
	if len(queryTerms) > 0 {
		tsQuery := buildTSQuery(queryTerms)
		sdArgs = append(sdArgs, tsQuery)
		tsArgN := sdArgN
		sdArgN++

		fuzzyParts := []string{}
		for _, term := range queryTerms {
			sdArgs = append(sdArgs, "%"+strings.ToLower(term)+"%")
			fuzzyParts = append(fuzzyParts, fmt.Sprintf("lower(r.title) LIKE $%d", sdArgN))
			sdArgN++
		}
		fuzzyExpr := strings.Join(fuzzyParts, " OR ")

		if q.UseFuzzy && q.Q != "" {
			sdArgs = append(sdArgs, q.Q)
			trgmArgN := sdArgN
			sdArgN++
			textFilter = fmt.Sprintf(` AND (sd.combined_tokens @@ to_tsquery('english', $%d) OR %s OR similarity(r.title, $%d) > 0.2)`,
				tsArgN, fuzzyExpr, trgmArgN)
			rankExpr = fmt.Sprintf(
				"ts_rank(sd.combined_tokens, to_tsquery('english', $%d)) + similarity(r.title, $%d)*0.5",
				tsArgN, trgmArgN)
		} else {
			textFilter = fmt.Sprintf(` AND (sd.combined_tokens @@ to_tsquery('english', $%d) OR %s)`,
				tsArgN, fuzzyExpr)
			rankExpr = fmt.Sprintf(
				"ts_rank(sd.combined_tokens, to_tsquery('english', $%d))",
				tsArgN)
		}
	}

	orderBy := "rank DESC, r.created_at DESC"
	if len(queryTerms) == 0 {
		rankExpr = "0"
		switch q.Sort {
		case "popular":
			orderBy = "r.view_count DESC, r.created_at DESC"
		case "recent":
			orderBy = "r.publish_date DESC NULLS LAST, r.created_at DESC"
		default:
			orderBy = "r.created_at DESC"
		}
	} else {
		switch q.Sort {
		case "popular":
			orderBy = "r.view_count DESC, rank DESC"
		case "recent":
			orderBy = "r.publish_date DESC NULLS LAST, rank DESC"
		}
	}

	baseJoin := `FROM search_documents sd
		JOIN resources r ON r.id = sd.resource_id
		WHERE r.is_published = TRUE AND r.is_archived = FALSE` + resourceFilters + textFilter

	// Count
	var total int
	countArgs := make([]any, len(sdArgs))
	copy(countArgs, sdArgs)
	countQ := "SELECT COUNT(*) " + baseJoin
	_ = s.pool.QueryRow(ctx, countQ, countArgs...).Scan(&total)

	// ── Phase 2: fetch full resource data for ranked candidates ───────────────
	mainQ := fmt.Sprintf(`
		SELECT r.id, r.title, coalesce(r.description,''), r.content_type, r.category,
		       to_char(r.publish_date,'YYYY-MM-DD'), r.is_published, r.is_archived,
		       r.view_count, r.job_family_id, r.created_at, r.updated_at,
		       (%s) AS rank
		%s ORDER BY %s LIMIT $%d OFFSET $%d`,
		rankExpr, baseJoin, orderBy, sdArgN, sdArgN+1)
	sdArgs = append(sdArgs, q.Limit, q.Offset)

	rows, err := s.pool.Query(ctx, mainQ, sdArgs...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var res Result
		var pubDate *string
		if err := rows.Scan(
			&res.ID, &res.Title, &res.Description, &res.ContentType, &res.Category,
			&pubDate, &res.IsPublished, &res.IsArchived, &res.ViewCount, &res.JobFamilyID,
			&res.CreatedAt, &res.UpdatedAt, &res.Rank,
		); err != nil {
			continue
		}
		res.PublishDate = pubDate
		results = append(results, res)
	}
	if results == nil {
		results = []Result{}
	}

	resp := &SearchResponse{
		Results:          results,
		Total:            total,
		Limit:            q.Limit,
		Offset:           q.Offset,
		ExpandedSynonyms: expandedSynonyms,
		ExpandedPinyin:   expandedPinyin,
	}
	if q.Q != "" {
		resp.TaxonomyHits = s.searchTaxonomy(ctx, q.Q)
	}
	return resp, nil
}

// searchDirect is the fallback search that queries resources directly.
// Used when search_documents index is empty.
func (s *Store) searchDirect(ctx context.Context, q Query, queryTerms []string) (*SearchResponse, error) {
	args := []any{}
	argN := 1

	where := "WHERE r.is_published = TRUE AND r.is_archived = FALSE"
	if q.Category != "" {
		where += fmt.Sprintf(" AND r.category = $%d", argN)
		args = append(args, q.Category)
		argN++
	}
	if q.ContentType != "" {
		where += fmt.Sprintf(" AND r.content_type = $%d", argN)
		args = append(args, q.ContentType)
		argN++
	}
	if q.TagCode != "" {
		where += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM resource_tags rt2 JOIN skill_tags st2 ON st2.id = rt2.tag_id
			WHERE rt2.resource_id = r.id AND st2.code = $%d)`, argN)
		args = append(args, q.TagCode)
		argN++
	}
	// Multi-tag filter: every code in TagCodes must match (AND semantics).
	for _, code := range q.TagCodes {
		where += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM resource_tags rt3 JOIN skill_tags st3 ON st3.id = rt3.tag_id
			WHERE rt3.resource_id = r.id AND st3.code = $%d)`, argN)
		args = append(args, code)
		argN++
	}
	if q.FromDate != "" {
		where += fmt.Sprintf(" AND r.publish_date >= $%d::date", argN)
		args = append(args, q.FromDate)
		argN++
	}
	if q.ToDate != "" {
		where += fmt.Sprintf(" AND r.publish_date <= $%d::date", argN)
		args = append(args, q.ToDate)
		argN++
	}

	var rankExpr, textWhere string
	if len(queryTerms) > 0 {
		tsQuery := buildTSQuery(queryTerms)
		args = append(args, tsQuery)
		tsArgN := argN
		argN++

		fuzzyParts := []string{}
		for _, term := range queryTerms {
			args = append(args, "%"+strings.ToLower(term)+"%")
			fuzzyParts = append(fuzzyParts, fmt.Sprintf("lower(r.title) LIKE $%d", argN))
			argN++
		}
		fuzzyExpr := strings.Join(fuzzyParts, " OR ")

		if q.UseFuzzy && q.Q != "" {
			args = append(args, q.Q)
			trgmArgN := argN
			argN++
			textWhere = fmt.Sprintf(` AND (to_tsvector('english', coalesce(r.title,'') || ' ' || coalesce(r.description,'')) @@ to_tsquery('english', $%d) OR %s OR similarity(r.title, $%d) > 0.2)`,
				tsArgN, fuzzyExpr, trgmArgN)
			rankExpr = fmt.Sprintf(
				"ts_rank(to_tsvector('english', coalesce(r.title,'') || ' ' || coalesce(r.description,'')), to_tsquery('english', $%d)) + similarity(r.title, $%d)*0.5",
				tsArgN, trgmArgN)
		} else {
			textWhere = fmt.Sprintf(` AND (to_tsvector('english', coalesce(r.title,'') || ' ' || coalesce(r.description,'')) @@ to_tsquery('english', $%d) OR %s)`,
				tsArgN, fuzzyExpr)
			rankExpr = fmt.Sprintf(
				"ts_rank(to_tsvector('english', coalesce(r.title,'') || ' ' || coalesce(r.description,'')), to_tsquery('english', $%d))",
				tsArgN)
		}
	}

	where += textWhere

	orderBy := "rank DESC, r.created_at DESC"
	if len(queryTerms) == 0 {
		rankExpr = "0"
		switch q.Sort {
		case "popular":
			orderBy = "r.view_count DESC, r.created_at DESC"
		case "recent":
			orderBy = "r.publish_date DESC NULLS LAST, r.created_at DESC"
		default:
			orderBy = "r.created_at DESC"
		}
	} else {
		switch q.Sort {
		case "popular":
			orderBy = "r.view_count DESC, rank DESC"
		case "recent":
			orderBy = "r.publish_date DESC NULLS LAST, rank DESC"
		}
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	countQ := "SELECT COUNT(*) FROM resources r " + where
	_ = s.pool.QueryRow(ctx, countQ, countArgs...).Scan(&total)

	mainQ := fmt.Sprintf(`
		SELECT r.id, r.title, coalesce(r.description,''), r.content_type, r.category,
		       to_char(r.publish_date,'YYYY-MM-DD'), r.is_published, r.is_archived,
		       r.view_count, r.job_family_id, r.created_at, r.updated_at,
		       (%s) AS rank
		FROM resources r
		%s ORDER BY %s LIMIT $%d OFFSET $%d`,
		rankExpr, where, orderBy, argN, argN+1)
	args = append(args, q.Limit, q.Offset)

	rows, err := s.pool.Query(ctx, mainQ, args...)
	if err != nil {
		return nil, fmt.Errorf("search direct query: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var res Result
		var pubDate *string
		if err := rows.Scan(
			&res.ID, &res.Title, &res.Description, &res.ContentType, &res.Category,
			&pubDate, &res.IsPublished, &res.IsArchived, &res.ViewCount, &res.JobFamilyID,
			&res.CreatedAt, &res.UpdatedAt, &res.Rank,
		); err != nil {
			continue
		}
		res.PublishDate = pubDate
		results = append(results, res)
	}
	if results == nil {
		results = []Result{}
	}

	resp := &SearchResponse{
		Results: results,
		Total:   total,
		Limit:   q.Limit,
		Offset:  q.Offset,
	}
	if q.Q != "" {
		resp.TaxonomyHits = s.searchTaxonomy(ctx, q.Q)
	}
	return resp, nil
}

// searchTaxonomy returns skill tags whose canonical name or active synonyms
// match the query term (case-insensitive prefix). Capped at 5 hits to keep
// the response lightweight. Each hit includes the number of published resources
// tagged with that tag, so the UI can show "Python (14 resources)".
func (s *Store) searchTaxonomy(ctx context.Context, term string) []TaxonomyHit {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT st.id, st.code, st.canonical_name,
		       (SELECT count(*) FROM resource_tags rt JOIN resources r ON r.id = rt.resource_id
		        WHERE rt.tag_id = st.id AND r.is_published = TRUE AND r.is_archived = FALSE) AS resource_count,
		       CASE
		         WHEN lower(st.canonical_name) LIKE lower($1) || '%' THEN 'canonical'
		         ELSE 'synonym:' || ts.synonym_text
		       END AS matched_via
		FROM skill_tags st
		LEFT JOIN tag_synonyms ts
		  ON ts.tag_id = st.id AND ts.is_active = TRUE AND lower(ts.synonym_text) LIKE lower($1) || '%'
		WHERE lower(st.canonical_name) LIKE lower($1) || '%'
		   OR ts.id IS NOT NULL
		ORDER BY resource_count DESC
		LIMIT 5`, term)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var hits []TaxonomyHit
	for rows.Next() {
		var h TaxonomyHit
		if err := rows.Scan(&h.TagID, &h.Code, &h.CanonicalName,
			&h.ResourceCount, &h.MatchedVia); err != nil {
			continue
		}
		hits = append(hits, h)
	}
	return hits
}

// RebuildIndex rebuilds all search_documents from resources.
func (s *Store) RebuildIndex(ctx context.Context) (int, error) {
	result, err := s.pool.Exec(ctx, `
		INSERT INTO search_documents (resource_id, title_tokens, body_tokens, combined_tokens, popularity_score, last_rebuilt_at)
		SELECT
			r.id,
			to_tsvector('english', r.title),
			to_tsvector('english', coalesce(r.description,'')),
			setweight(to_tsvector('english', r.title), 'A') ||
			setweight(to_tsvector('english', coalesce(r.description,'')), 'B'),
			r.view_count::float,
			NOW()
		FROM resources r
		WHERE r.is_published = TRUE
		ON CONFLICT (resource_id) DO UPDATE SET
			title_tokens     = EXCLUDED.title_tokens,
			body_tokens      = EXCLUDED.body_tokens,
			combined_tokens  = EXCLUDED.combined_tokens,
			popularity_score = EXCLUDED.popularity_score,
			last_rebuilt_at  = EXCLUDED.last_rebuilt_at`)
	if err != nil {
		return 0, err
	}
	n := int(result.RowsAffected())
	return n, nil
}

// UpdateResourceIndex updates the search document for a single resource.
func (s *Store) UpdateResourceIndex(ctx context.Context, resourceID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO search_documents (resource_id, title_tokens, body_tokens, combined_tokens, popularity_score, last_rebuilt_at)
		SELECT
			r.id,
			to_tsvector('english', r.title),
			to_tsvector('english', coalesce(r.description,'')),
			setweight(to_tsvector('english', r.title), 'A') ||
			setweight(to_tsvector('english', coalesce(r.description,'')), 'B'),
			r.view_count::float,
			NOW()
		FROM resources r WHERE r.id = $1
		ON CONFLICT (resource_id) DO UPDATE SET
			title_tokens    = EXCLUDED.title_tokens,
			body_tokens     = EXCLUDED.body_tokens,
			combined_tokens = EXCLUDED.combined_tokens,
			last_rebuilt_at = EXCLUDED.last_rebuilt_at`,
		resourceID)
	return err
}

// RefreshArchiveBuckets rebuilds the archive_buckets and archive_membership
// tables. Both bucket_types ("month" and "tag") are rebuilt, then the
// membership table is fully re-populated so lookups of "which resources
// belong to this bucket?" stay consistent with the aggregated counts.
func (s *Store) RefreshArchiveBuckets(ctx context.Context) error {
	// Bucket rows: month bucket, one per distinct YYYY-MM. Archived resources
	// are excluded everywhere — bucket counts must agree with the resources
	// returned by GetBucketResources, which itself excludes is_archived.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO archive_buckets (bucket_type, bucket_key, display_label, resource_count, last_updated_at)
		SELECT 'month',
		       to_char(r.publish_date, 'YYYY-MM'),
		       to_char(r.publish_date, 'Month YYYY'),
		       COUNT(*),
		       NOW()
		FROM resources r
		WHERE r.is_published = TRUE AND r.is_archived = FALSE AND r.publish_date IS NOT NULL
		GROUP BY to_char(r.publish_date, 'YYYY-MM'), to_char(r.publish_date, 'Month YYYY')
		ON CONFLICT (bucket_type, bucket_key) DO UPDATE SET
			resource_count  = EXCLUDED.resource_count,
			last_updated_at = EXCLUDED.last_updated_at`); err != nil {
		return fmt.Errorf("refresh month buckets: %w", err)
	}

	// Bucket rows: tag bucket, one per skill_tag that has published, non-archived resources.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO archive_buckets (bucket_type, bucket_key, display_label, resource_count, last_updated_at)
		SELECT 'tag', st.code, st.canonical_name, COUNT(DISTINCT rt.resource_id), NOW()
		FROM skill_tags st
		JOIN resource_tags rt ON rt.tag_id = st.id
		JOIN resources r ON r.id = rt.resource_id AND r.is_published = TRUE AND r.is_archived = FALSE
		GROUP BY st.code, st.canonical_name
		ON CONFLICT (bucket_type, bucket_key) DO UPDATE SET
			resource_count  = EXCLUDED.resource_count,
			last_updated_at = EXCLUDED.last_updated_at`); err != nil {
		return fmt.Errorf("refresh tag buckets: %w", err)
	}

	// Membership: rebuild the join table in full. Wrapping in a transaction
	// keeps lookups consistent — readers either see the old contents or the
	// new contents, never a half-truncated view.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin membership tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `TRUNCATE TABLE archive_membership`); err != nil {
		return fmt.Errorf("truncate membership: %w", err)
	}

	// month membership: every published, non-archived resource with a
	// publish_date belongs to its YYYY-MM bucket.
	if _, err := tx.Exec(ctx, `
		INSERT INTO archive_membership (resource_id, bucket_id, added_at)
		SELECT r.id, ab.id, NOW()
		FROM resources r
		JOIN archive_buckets ab
		  ON ab.bucket_type = 'month'
		 AND ab.bucket_key  = to_char(r.publish_date, 'YYYY-MM')
		WHERE r.is_published = TRUE AND r.is_archived = FALSE AND r.publish_date IS NOT NULL
		ON CONFLICT DO NOTHING`); err != nil {
		return fmt.Errorf("rebuild month membership: %w", err)
	}

	// tag membership: every (resource, tag) edge maps to the tag bucket.
	if _, err := tx.Exec(ctx, `
		INSERT INTO archive_membership (resource_id, bucket_id, added_at)
		SELECT DISTINCT r.id, ab.id, NOW()
		FROM resources r
		JOIN resource_tags rt ON rt.resource_id = r.id
		JOIN skill_tags    st ON st.id = rt.tag_id
		JOIN archive_buckets ab
		  ON ab.bucket_type = 'tag'
		 AND ab.bucket_key  = st.code
		WHERE r.is_published = TRUE AND r.is_archived = FALSE
		ON CONFLICT DO NOTHING`); err != nil {
		return fmt.Errorf("rebuild tag membership: %w", err)
	}

	// Prune buckets whose resource_count is 0 (a tag lost all its published
	// resources, or a month has none any more). Keeps GetArchiveBuckets tidy.
	if _, err := tx.Exec(ctx, `
		DELETE FROM archive_buckets WHERE resource_count = 0`); err != nil {
		return fmt.Errorf("prune empty buckets: %w", err)
	}

	return tx.Commit(ctx)
}

// GetBucketResources returns the resources whose membership maps them to the
// (bucket_type, bucket_key) pair, with paging. Joins through archive_membership
// → archive_buckets so the lookup is O(bucket size) instead of scanning all
// resources. Returns (resources, total) so the UI can show "showing 1–20 of N".
func (s *Store) GetBucketResources(ctx context.Context, bucketType, bucketKey string, limit, offset int) ([]catalog.Resource, int, error) {
	var bucketID int64
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM archive_buckets WHERE bucket_type = $1 AND bucket_key = $2`,
		bucketType, bucketKey).Scan(&bucketID)
	if err != nil {
		// Empty bucket / missing bucket — return empty list rather than 500.
		return []catalog.Resource{}, 0, nil
	}

	var total int
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM archive_membership am
		 JOIN resources r ON r.id = am.resource_id
		 WHERE am.bucket_id = $1 AND r.is_published = TRUE AND r.is_archived = FALSE`,
		bucketID).Scan(&total)

	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.title, coalesce(r.description,''), r.content_type, r.category,
		       to_char(r.publish_date,'YYYY-MM-DD'), r.is_published, r.is_archived,
		       r.view_count, r.job_family_id, r.created_at, r.updated_at
		FROM archive_membership am
		JOIN resources r ON r.id = am.resource_id
		WHERE am.bucket_id = $1
		  AND r.is_published = TRUE AND r.is_archived = FALSE
		ORDER BY r.publish_date DESC NULLS LAST, r.created_at DESC
		LIMIT $2 OFFSET $3`, bucketID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("bucket resources: %w", err)
	}
	defer rows.Close()

	out := make([]catalog.Resource, 0)
	for rows.Next() {
		var r catalog.Resource
		var pubDate *string
		if err := rows.Scan(&r.ID, &r.Title, &r.Description, &r.ContentType, &r.Category,
			&pubDate, &r.IsPublished, &r.IsArchived, &r.ViewCount, &r.JobFamilyID,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
			continue
		}
		r.PublishDate = pubDate
		out = append(out, r)
	}
	return out, total, nil
}

// GetArchiveBuckets returns archive buckets optionally filtered by type.
func (s *Store) GetArchiveBuckets(ctx context.Context, bucketType string) ([]ArchiveBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT bucket_type, bucket_key, display_label, resource_count
		FROM archive_buckets
		WHERE ($1 = '' OR bucket_type = $1)
		ORDER BY bucket_key DESC`, bucketType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buckets []ArchiveBucket
	for rows.Next() {
		var b ArchiveBucket
		if err := rows.Scan(&b.Type, &b.Key, &b.Label, &b.Count); err != nil {
			continue
		}
		buckets = append(buckets, b)
	}
	if buckets == nil {
		buckets = []ArchiveBucket{}
	}
	return buckets, nil
}

// expandSynonyms looks up active synonyms whose text matches terms in the query.
func (s *Store) expandSynonyms(ctx context.Context, term string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ts2.synonym_text
		FROM tag_synonyms ts1
		JOIN skill_tags st ON st.id = ts1.tag_id
		JOIN tag_synonyms ts2 ON ts2.tag_id = st.id AND ts2.is_active = TRUE AND ts2.synonym_type = 'alias'
		WHERE ts1.is_active = TRUE AND lower(ts1.synonym_text) = lower($1)
		  AND lower(ts2.synonym_text) <> lower($1)
		LIMIT 10`, term)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var synonyms []string
	for rows.Next() {
		var syn string
		_ = rows.Scan(&syn)
		synonyms = append(synonyms, syn)
	}
	return synonyms, nil
}

func (s *Store) expandPinyin(ctx context.Context, term string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ts2.synonym_text
		FROM tag_synonyms ts1
		JOIN skill_tags st ON st.id = ts1.tag_id
		JOIN tag_synonyms ts2 ON ts2.tag_id = st.id AND ts2.is_active = TRUE AND ts2.synonym_type = 'pinyin'
		WHERE ts1.is_active = TRUE AND lower(ts1.synonym_text) = lower($1)
		LIMIT 10`, term)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var p string
		_ = rows.Scan(&p)
		results = append(results, p)
	}
	return results, nil
}

func buildTSQuery(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		words := strings.Fields(t)
		escaped := make([]string, len(words))
		for i, w := range words {
			escaped[i] = w + ":*" // prefix search
		}
		parts = append(parts, strings.Join(escaped, " & "))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}
