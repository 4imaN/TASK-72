package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Resource struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	ContentType string    `json:"content_type"`
	Category    string    `json:"category"`
	PublishDate *string   `json:"publish_date,omitempty"`
	IsPublished bool      `json:"is_published"`
	IsArchived  bool      `json:"is_archived"`
	ViewCount   int64     `json:"view_count"`
	JobFamilyID *int64    `json:"job_family_id,omitempty"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ListFilter struct {
	Category    string
	ContentType string
	TagCode     string
	FromDate    string
	ToDate      string
	JobFamilyID *int64
	Published   *bool
}

type ListOptions struct {
	Filter  ListFilter
	Sort    string // "relevance", "popular", "recent"
	Limit   int
	Offset  int
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetByID(ctx context.Context, id string) (*Resource, error) {
	r := &Resource{}
	var pubDate *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, title, coalesce(description,''), content_type, category,
		       to_char(publish_date, 'YYYY-MM-DD'), is_published, is_archived,
		       view_count, job_family_id, created_at, updated_at
		FROM resources WHERE id = $1`, id).
		Scan(&r.ID, &r.Title, &r.Description, &r.ContentType, &r.Category,
			&pubDate, &r.IsPublished, &r.IsArchived, &r.ViewCount, &r.JobFamilyID,
			&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get resource: %w", err)
	}
	r.PublishDate = pubDate
	r.Tags = s.getTags(ctx, id)
	return r, nil
}

func (s *Store) List(ctx context.Context, opts ListOptions) ([]Resource, int, error) {
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 20
	}

	args := []any{}
	argN := 1
	where := "WHERE r.is_published = TRUE AND r.is_archived = FALSE"

	if opts.Filter.Category != "" {
		where += fmt.Sprintf(" AND r.category = $%d", argN)
		args = append(args, opts.Filter.Category)
		argN++
	}
	if opts.Filter.ContentType != "" {
		where += fmt.Sprintf(" AND r.content_type = $%d", argN)
		args = append(args, opts.Filter.ContentType)
		argN++
	}
	if opts.Filter.FromDate != "" {
		where += fmt.Sprintf(" AND r.publish_date >= $%d::date", argN)
		args = append(args, opts.Filter.FromDate)
		argN++
	}
	if opts.Filter.ToDate != "" {
		where += fmt.Sprintf(" AND r.publish_date <= $%d::date", argN)
		args = append(args, opts.Filter.ToDate)
		argN++
	}
	if opts.Filter.TagCode != "" {
		where += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM resource_tags rt2
			JOIN skill_tags st2 ON st2.id = rt2.tag_id
			WHERE rt2.resource_id = r.id AND st2.code = $%d)`, argN)
		args = append(args, opts.Filter.TagCode)
		argN++
	}
	if opts.Filter.JobFamilyID != nil {
		where += fmt.Sprintf(" AND r.job_family_id = $%d", argN)
		args = append(args, *opts.Filter.JobFamilyID)
		argN++
	}

	orderBy := "r.created_at DESC"
	switch opts.Sort {
	case "popular":
		orderBy = "r.view_count DESC, r.created_at DESC"
	case "recent":
		orderBy = "r.publish_date DESC NULLS LAST, r.created_at DESC"
	}

	countArgs := make([]any, len(args))
	copy(countArgs, args)

	var total int
	countQuery := "SELECT COUNT(*) FROM resources r " + where
	_ = s.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total)

	query := fmt.Sprintf(`
		SELECT r.id, r.title, coalesce(r.description,''), r.content_type, r.category,
		       to_char(r.publish_date, 'YYYY-MM-DD'), r.is_published, r.is_archived,
		       r.view_count, r.job_family_id, r.created_at, r.updated_at
		FROM resources r
		%s ORDER BY %s LIMIT $%d OFFSET $%d`,
		where, orderBy, argN, argN+1)
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list resources: %w", err)
	}
	defer rows.Close()

	var resources []Resource
	for rows.Next() {
		var r Resource
		var pubDate *string
		if err := rows.Scan(&r.ID, &r.Title, &r.Description, &r.ContentType, &r.Category,
			&pubDate, &r.IsPublished, &r.IsArchived, &r.ViewCount, &r.JobFamilyID,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		r.PublishDate = pubDate
		r.Tags = s.getTags(ctx, r.ID)
		resources = append(resources, r)
	}
	return resources, total, nil
}

// IncrementViewCount increments a resource's view_count and, in the same
// transaction, refreshes the cached popularity_score in search_documents so
// ranking stays consistent with the source of truth. This is the incremental
// update path that keeps the index fresh between nightly rebuilds.
func (s *Store) IncrementViewCount(ctx context.Context, id string) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE resources SET view_count = view_count + 1 WHERE id = $1`, id); err != nil {
		return
	}
	// Update only if an index row already exists — avoid re-ingesting a
	// resource that the nightly rebuild has not picked up yet.
	if _, err := tx.Exec(ctx, `
		UPDATE search_documents
		SET popularity_score = (SELECT view_count::float FROM resources WHERE id = $1),
		    last_rebuilt_at  = NOW()
		WHERE resource_id = $1`, id); err != nil {
		return
	}
	_ = tx.Commit(ctx)
}

func (s *Store) getTags(ctx context.Context, resourceID string) []string {
	rows, _ := s.pool.Query(ctx, `
		SELECT st.code FROM skill_tags st
		JOIN resource_tags rt ON rt.tag_id = st.id
		WHERE rt.resource_id = $1`, resourceID)
	if rows == nil {
		return []string{}
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		_ = rows.Scan(&t)
		tags = append(tags, t)
	}
	if tags == nil {
		return []string{}
	}
	return tags
}

// Create inserts a new resource and seeds its search_documents row so the
// resource is immediately queryable (the nightly rebuild would otherwise be
// the only way it gets indexed).
func (s *Store) Create(ctx context.Context, r Resource, createdBy string) (string, error) {
	id := uuid.New().String()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO resources (id, title, description, content_type, category, publish_date, is_published, created_by)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6,'')::date, $7, NULLIF($8,'')::uuid)`,
		id, r.Title, r.Description, r.ContentType, r.Category,
		ptrStr(r.PublishDate), r.IsPublished, createdBy,
	)
	if err != nil {
		return "", fmt.Errorf("create resource: %w", err)
	}

	// Incremental index refresh for the new resource. Only published resources
	// are indexed — draft/unpublished content stays out of search_documents.
	if r.IsPublished {
		s.indexResource(ctx, id)
	}
	return id, nil
}

// Update patches title/description/content_type/category/publish_date/is_published
// on an existing resource. Only fields explicitly present in the input are
// applied (zero values mean "no change") so callers can do partial updates.
// The search index is refreshed for published resources; unpublishing a
// resource removes it from the index.
func (s *Store) Update(ctx context.Context, id string, r Resource) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE resources
		SET title         = COALESCE(NULLIF($2,''), title),
		    description   = COALESCE(NULLIF($3,''), description),
		    content_type  = COALESCE(NULLIF($4,''), content_type),
		    category      = COALESCE(NULLIF($5,''), category),
		    publish_date  = COALESCE(NULLIF($6,'')::date, publish_date),
		    is_published  = $7,
		    updated_at    = NOW()
		WHERE id = $1`,
		id, r.Title, r.Description, r.ContentType, r.Category,
		ptrStr(r.PublishDate), r.IsPublished,
	)
	if err != nil {
		return fmt.Errorf("update resource: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("resource not found")
	}

	if r.IsPublished {
		s.indexResource(ctx, id)
	} else {
		s.removeFromIndex(ctx, id)
	}
	return nil
}

// Archive marks a resource as archived (soft-delete). Archived resources are
// excluded from List and Search but remain in the database for audit purposes.
// The search_documents row is removed so the archived resource cannot surface.
func (s *Store) Archive(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE resources SET is_archived = TRUE, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("archive resource: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("resource not found")
	}
	s.removeFromIndex(ctx, id)
	return nil
}

// Restore reverses Archive — un-archives the resource and re-indexes it if
// still published.
func (s *Store) Restore(ctx context.Context, id string) error {
	var isPublished bool
	err := s.pool.QueryRow(ctx, `
		UPDATE resources SET is_archived = FALSE, updated_at = NOW()
		WHERE id = $1 RETURNING is_published`, id).Scan(&isPublished)
	if err != nil {
		return fmt.Errorf("restore resource: %w", err)
	}
	if isPublished {
		s.indexResource(ctx, id)
	}
	return nil
}

// indexResource is the shared upsert path used by Create and Update.
func (s *Store) indexResource(ctx context.Context, id string) {
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO search_documents (resource_id, title_tokens, body_tokens, combined_tokens, popularity_score, last_rebuilt_at)
		SELECT r.id,
		       to_tsvector('english', r.title),
		       to_tsvector('english', coalesce(r.description,'')),
		       setweight(to_tsvector('english', r.title), 'A') ||
		       setweight(to_tsvector('english', coalesce(r.description,'')), 'B'),
		       r.view_count::float,
		       NOW()
		FROM resources r WHERE r.id = $1
		ON CONFLICT (resource_id) DO UPDATE SET
			title_tokens     = EXCLUDED.title_tokens,
			body_tokens      = EXCLUDED.body_tokens,
			combined_tokens  = EXCLUDED.combined_tokens,
			popularity_score = EXCLUDED.popularity_score,
			last_rebuilt_at  = EXCLUDED.last_rebuilt_at`, id)
}

// removeFromIndex deletes the resource's search_documents row so an archived
// or unpublished resource cannot appear in search results.
func (s *Store) removeFromIndex(ctx context.Context, id string) {
	_, _ = s.pool.Exec(ctx, `DELETE FROM search_documents WHERE resource_id = $1`, id)
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
