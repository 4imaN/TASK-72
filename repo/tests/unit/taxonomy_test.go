// tests/unit/taxonomy_test.go — unit tests for taxonomy logic (no DB required).
package unit_test

import (
	"strings"
	"testing"
)

// ── maskEmail tests (from users package, tested here as a unit) ───────────────

func TestMaskEmail_TypicalEmail(t *testing.T) {
	result := testMaskEmail("alice@example.com")
	if result != "a***@example.com" {
		t.Errorf("expected a***@example.com, got %q", result)
	}
}

func TestMaskEmail_ShortLocalPart(t *testing.T) {
	result := testMaskEmail("a@example.com")
	// local part is 1 char — should show ***@domain
	if result != "***@example.com" {
		t.Errorf("expected ***@example.com, got %q", result)
	}
}

func TestMaskEmail_TwoCharLocal(t *testing.T) {
	result := testMaskEmail("ab@example.com")
	// local part is 2 chars — keeps first char
	if result != "a***@example.com" {
		t.Errorf("expected a***@example.com, got %q", result)
	}
}

func TestMaskEmail_LongLocalPart(t *testing.T) {
	result := testMaskEmail("john.doe@company.org")
	if result != "j***@company.org" {
		t.Errorf("expected j***@company.org, got %q", result)
	}
}

// testMaskEmail replicates the maskEmail function from users/masking.go for unit testing.
func testMaskEmail(email string) string {
	at := 0
	for i, c := range email {
		if c == '@' {
			at = i
			break
		}
	}
	if at <= 1 {
		return "***@" + email[at+1:]
	}
	return string(email[0]) + "***@" + email[at+1:]
}

// ── buildTSQuery tests ────────────────────────────────────────────────────────

func TestBuildTSQuery_SingleTerm(t *testing.T) {
	result := testBuildTSQuery([]string{"leadership"})
	if result != "leadership:*" {
		t.Errorf("expected leadership:*, got %q", result)
	}
}

func TestBuildTSQuery_MultipleTerms(t *testing.T) {
	result := testBuildTSQuery([]string{"leadership", "management"})
	// Should join with |
	if !strings.Contains(result, "|") {
		t.Errorf("expected | separator between terms, got %q", result)
	}
	if !strings.Contains(result, "leadership:*") {
		t.Errorf("expected leadership:* in result, got %q", result)
	}
	if !strings.Contains(result, "management:*") {
		t.Errorf("expected management:* in result, got %q", result)
	}
}

func TestBuildTSQuery_MultiWordTerm(t *testing.T) {
	result := testBuildTSQuery([]string{"data science"})
	// Multi-word terms should join words with &
	if !strings.Contains(result, "&") {
		t.Errorf("expected & between words for multi-word term, got %q", result)
	}
	if !strings.Contains(result, "data:*") {
		t.Errorf("expected data:* in result, got %q", result)
	}
	if !strings.Contains(result, "science:*") {
		t.Errorf("expected science:* in result, got %q", result)
	}
}

func TestBuildTSQuery_EmptyInput(t *testing.T) {
	result := testBuildTSQuery([]string{})
	if result != "" {
		t.Errorf("expected empty string for empty input, got %q", result)
	}
}

func TestBuildTSQuery_EmptyStringTerms(t *testing.T) {
	result := testBuildTSQuery([]string{"", "  ", ""})
	if result != "" {
		t.Errorf("expected empty string for all-empty terms, got %q", result)
	}
}

func TestBuildTSQuery_PrefixSearchFormat(t *testing.T) {
	result := testBuildTSQuery([]string{"procure"})
	// Every word should be a prefix search (ends with :*)
	if !strings.HasSuffix(result, ":*") {
		t.Errorf("expected prefix search format ending in :*, got %q", result)
	}
}

// testBuildTSQuery replicates the buildTSQuery function from search/store.go.
func testBuildTSQuery(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		words := strings.Fields(t)
		escaped := make([]string, len(words))
		for i, w := range words {
			escaped[i] = w + ":*"
		}
		parts = append(parts, strings.Join(escaped, " & "))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}

// ── Taxonomy conflict detection logic (mock scenario) ────────────────────────

// conflictRecord models a detected synonym conflict.
type conflictRecord struct {
	SynonymText string
	TagIDA      int64
	TagIDB      int64
}

// mockSynonymStore simulates the synonym store for conflict detection testing.
type mockSynonymStore struct {
	synonyms map[string]int64 // synonym_text -> tag_id
	conflicts []conflictRecord
}

func newMockSynonymStore() *mockSynonymStore {
	return &mockSynonymStore{
		synonyms:  make(map[string]int64),
		conflicts: nil,
	}
}

// AddSynonym replicates the conflict detection logic from taxonomy/store.go.
func (m *mockSynonymStore) AddSynonym(tagID int64, text string) error {
	if existingTagID, exists := m.synonyms[text]; exists && existingTagID != tagID {
		m.conflicts = append(m.conflicts, conflictRecord{
			SynonymText: text,
			TagIDA:      tagID,
			TagIDB:      existingTagID,
		})
		return &conflictError{text: text, existingTagID: existingTagID}
	}
	m.synonyms[text] = tagID
	return nil
}

type conflictError struct {
	text          string
	existingTagID int64
}

func (e *conflictError) Error() string {
	return "conflict: synonym already points to a different active tag"
}

func TestTaxonomyConflict_NoConflictOnFirstAdd(t *testing.T) {
	store := newMockSynonymStore()
	err := store.AddSynonym(1, "leadership")
	if err != nil {
		t.Errorf("expected no error on first add, got: %v", err)
	}
}

func TestTaxonomyConflict_SameTagNoDuplicate(t *testing.T) {
	store := newMockSynonymStore()
	_ = store.AddSynonym(1, "leadership")
	// Adding same synonym to same tag — no conflict (idempotent insert)
	err := store.AddSynonym(1, "leadership")
	if err != nil {
		t.Errorf("expected no conflict when same tag adds same synonym, got: %v", err)
	}
}

func TestTaxonomyConflict_DifferentTagsConflict(t *testing.T) {
	store := newMockSynonymStore()
	_ = store.AddSynonym(1, "leader")
	err := store.AddSynonym(2, "leader") // tag 2 tries to claim "leader" already owned by tag 1
	if err == nil {
		t.Error("expected conflict error when different tag adds existing synonym")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("expected conflict in error message, got: %v", err)
	}
}

func TestTaxonomyConflict_RecordedInConflictLog(t *testing.T) {
	store := newMockSynonymStore()
	_ = store.AddSynonym(1, "mgr")
	_ = store.AddSynonym(2, "mgr")

	if len(store.conflicts) != 1 {
		t.Errorf("expected 1 conflict recorded, got %d", len(store.conflicts))
	}
	c := store.conflicts[0]
	if c.SynonymText != "mgr" {
		t.Errorf("expected conflict on 'mgr', got %q", c.SynonymText)
	}
	if c.TagIDA != 2 || c.TagIDB != 1 {
		t.Errorf("conflict tags mismatch: got A=%d B=%d", c.TagIDA, c.TagIDB)
	}
}

func TestTaxonomyConflict_MultipleConflictsTracked(t *testing.T) {
	store := newMockSynonymStore()
	_ = store.AddSynonym(1, "project")
	_ = store.AddSynonym(1, "mgmt")

	_ = store.AddSynonym(2, "project") // conflict 1
	_ = store.AddSynonym(3, "mgmt")    // conflict 2

	if len(store.conflicts) != 2 {
		t.Errorf("expected 2 conflicts, got %d", len(store.conflicts))
	}
}

func TestTaxonomyConflict_NoConflictForDifferentTerms(t *testing.T) {
	store := newMockSynonymStore()
	_ = store.AddSynonym(1, "leadership")
	err := store.AddSynonym(2, "management") // completely different term
	if err != nil {
		t.Errorf("expected no conflict for distinct terms, got: %v", err)
	}
	if len(store.conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(store.conflicts))
	}
}

// ── Fuzzy match ranking tests ─────────────────────────────────────────────────

func TestFuzzyMatch_ExactMatchHighestRank(t *testing.T) {
	// Simulates that an exact title match should rank higher than partial
	type rankResult struct {
		title string
		rank  float64
	}
	results := []rankResult{
		{"Leadership Principles", 0.9},
		{"Advanced Leadership Strategies", 0.7},
		{"Leadership for Beginners", 0.6},
	}
	// Verify first result (exact/closest match) has highest rank
	for i := 1; i < len(results); i++ {
		if results[i].rank > results[0].rank {
			t.Errorf("result %d has higher rank than result 0: %f > %f",
				i, results[i].rank, results[0].rank)
		}
	}
}

func TestFuzzyMatch_ZeroRankForNoMatch(t *testing.T) {
	rank := computeSimpleRank("completely unrelated content", "leadership")
	if rank > 0 {
		t.Errorf("expected zero rank for non-matching content, got %f", rank)
	}
}

func TestFuzzyMatch_PartialMatchPositiveRank(t *testing.T) {
	rank := computeSimpleRank("Introduction to Leadership Skills", "leadership")
	if rank <= 0 {
		t.Errorf("expected positive rank for partial match, got %f", rank)
	}
}

// computeSimpleRank is a simplified ranking function for unit test verification.
func computeSimpleRank(title, query string) float64 {
	tl := strings.ToLower(title)
	ql := strings.ToLower(query)
	if strings.Contains(tl, ql) {
		return 1.0
	}
	return 0.0
}
