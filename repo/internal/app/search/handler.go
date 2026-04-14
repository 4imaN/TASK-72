package search

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
	"portal/internal/platform/featureflag"
)

// splitAndTrim splits a comma-separated string and trims whitespace.
// Empty tokens are dropped.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

type Handler struct {
	store *Store
	gate  *featureflag.Gate
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// NewHandlerWithFlags constructs a Handler that consults feature flags for
// phased-rollout capabilities (search.pinyin_expansion, search.synonym_expansion).
func NewHandlerWithFlags(store *Store, flags featureflag.Checker, roles featureflag.RoleLookup) *Handler {
	return &Handler{store: store, gate: featureflag.New(flags, roles)}
}

func (h *Handler) Search(c echo.Context) error {
	// Multi-tag filter (comma-separated) — each must match.
	var tagCodes []string
	if raw := c.QueryParam("tags"); raw != "" {
		tagCodes = append(tagCodes, splitAndTrim(raw)...)
	}

	// Pinyin expansion is a phased-rollout feature (flag: search.pinyin_expansion).
	// Synonym expansion is always-on by default (flag defaults to enabled) but can
	// be rolled back or targeted to specific roles through the Config Center. In
	// both cases we let the flag be the authoritative answer when it's disabled
	// for the caller's role set — ignoring the client's query-string preference.
	wantPinyin := c.QueryParam("pinyin") == "true"
	if wantPinyin && h.gate != nil && !h.gate.EnabledFor(c, "search.pinyin_expansion") {
		wantPinyin = false
	}

	wantSynonyms := c.QueryParam("synonyms") != "false"
	if wantSynonyms && h.gate != nil && !h.gate.EnabledFor(c, "search.synonym_expansion") {
		wantSynonyms = false
	}

	q := Query{
		Q:           c.QueryParam("q"),
		UseSynonyms: wantSynonyms,
		UsePinyin:   wantPinyin,
		UseFuzzy:    c.QueryParam("fuzzy") != "false",
		Category:    c.QueryParam("category"),
		ContentType: c.QueryParam("content_type"),
		TagCode:     c.QueryParam("tag"),
		TagCodes:    tagCodes,
		FromDate:    c.QueryParam("from_date"),
		ToDate:      c.QueryParam("to_date"),
		Sort:        c.QueryParam("sort"),
		Limit:       parseIntParam(c.QueryParam("limit"), 20),
		Offset:      parseIntParam(c.QueryParam("offset"), 0),
	}

	resp, err := h.store.Search(c.Request().Context(), q)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) RebuildIndex(c echo.Context) error {
	n, err := h.store.RebuildIndex(c.Request().Context())
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"rebuilt": n})
}

func (h *Handler) GetArchiveBuckets(c echo.Context) error {
	bucketType := c.QueryParam("type") // "month" or "tag"
	buckets, err := h.store.GetArchiveBuckets(c.Request().Context(), bucketType)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"buckets": buckets})
}

// GetBucketResources handles GET /api/v1/archive/buckets/:type/:key/resources.
// Returns the resources that belong to the named bucket (month "YYYY-MM" or
// tag code), backed by archive_membership. Supports pagination via ?limit and
// ?offset (default 20 / 0). This is the read path that turns the bucket index
// into actual archive page browsing.
func (h *Handler) GetBucketResources(c echo.Context) error {
	bucketType := c.Param("type")
	bucketKey := c.Param("key")
	if bucketType == "" || bucketKey == "" {
		return common.BadRequest(c, "validation.required", "bucket type and key are required")
	}
	if bucketType != "month" && bucketType != "tag" {
		return common.BadRequest(c, "validation.invalid_type", "bucket type must be month or tag")
	}

	limit := parseIntParam(c.QueryParam("limit"), 20)
	if limit > 100 {
		limit = 100
	}
	offset := parseIntParam(c.QueryParam("offset"), 0)

	resources, total, err := h.store.GetBucketResources(c.Request().Context(),
		bucketType, bucketKey, limit, offset)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{
		"bucket_type": bucketType,
		"bucket_key":  bucketKey,
		"resources":   resources,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
	})
}

func parseIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil || v < 0 {
		return def
	}
	return v
}
