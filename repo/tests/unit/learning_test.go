// tests/unit/learning_test.go — unit tests for learning domain helpers.
// No database required; tests exercise pure logic and CSV writing via io.Writer.
package unit_test

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"
)

// ── nullTime helper (copied from store to keep tests self-contained) ──────────

func nullTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// 1. TestNullTimeHelper — nullTime returns "" for nil, RFC3339 for non-nil
func TestNullTimeHelper(t *testing.T) {
	// nil case
	result := nullTime(nil)
	if result != "" {
		t.Errorf("expected empty string for nil time, got %q", result)
	}

	// non-nil case
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result = nullTime(&now)
	if result == "" {
		t.Error("expected non-empty string for non-nil time")
	}
	// Must parse as RFC3339
	parsed, err := time.Parse(time.RFC3339, result)
	if err != nil {
		t.Errorf("nullTime output is not valid RFC3339: %q — %v", result, err)
	}
	if !parsed.Equal(now) {
		t.Errorf("expected %v, got %v", now, parsed)
	}
}

// 2. TestCSVHeaderRow — writing a CSV header produces the expected columns
func TestCSVHeaderRow(t *testing.T) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)

	expectedHeaders := []string{
		"path_id", "path_title", "enrollment_status", "enrolled_at", "completed_at",
		"resource_id", "resource_title", "content_type", "item_type",
		"progress_status", "progress_pct", "resource_completed_at",
	}

	if err := cw.Write(expectedHeaders); err != nil {
		t.Fatalf("failed to write header: %v", err)
	}
	cw.Flush()

	if err := cw.Error(); err != nil {
		t.Fatalf("csv writer error: %v", err)
	}

	// Parse it back and verify
	r := csv.NewReader(strings.NewReader(buf.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("failed to read csv: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 row (header), got %d", len(records))
	}
	headers := records[0]
	if len(headers) != len(expectedHeaders) {
		t.Fatalf("expected %d columns, got %d", len(expectedHeaders), len(headers))
	}
	for i, h := range expectedHeaders {
		if headers[i] != h {
			t.Errorf("column %d: expected %q, got %q", i, h, headers[i])
		}
	}
}

// ── Completion rule logic (pure, no DB) ──────────────────────────────────────

type completionRuleInput struct {
	RequiredCount   int
	ElectiveMinimum int
	RequiredDone    int
	ElectiveDone    int
}

// evaluateCompletion mirrors the rule logic in store.GetPathProgress.
func evaluateCompletion(rules completionRuleInput) bool {
	allRequired := rules.RequiredDone >= rules.RequiredCount
	enoughElectives := rules.ElectiveDone >= rules.ElectiveMinimum
	return allRequired && enoughElectives
}

// 3. TestCompletionRuleRequiredAndElective — table-driven completion rule tests
func TestCompletionRuleRequiredAndElective(t *testing.T) {
	cases := []struct {
		name   string
		input  completionRuleInput
		expect bool
	}{
		{
			name:   "all required and elective met",
			input:  completionRuleInput{RequiredCount: 3, ElectiveMinimum: 2, RequiredDone: 3, ElectiveDone: 2},
			expect: true,
		},
		{
			name:   "all required met, elective exceeded",
			input:  completionRuleInput{RequiredCount: 2, ElectiveMinimum: 1, RequiredDone: 2, ElectiveDone: 3},
			expect: true,
		},
		{
			name:   "required not fully done",
			input:  completionRuleInput{RequiredCount: 3, ElectiveMinimum: 1, RequiredDone: 2, ElectiveDone: 1},
			expect: false,
		},
		{
			name:   "elective minimum not met",
			input:  completionRuleInput{RequiredCount: 2, ElectiveMinimum: 2, RequiredDone: 2, ElectiveDone: 1},
			expect: false,
		},
		{
			name:   "neither required nor elective done",
			input:  completionRuleInput{RequiredCount: 3, ElectiveMinimum: 2, RequiredDone: 0, ElectiveDone: 0},
			expect: false,
		},
		{
			name:   "no required items needed, elective met",
			input:  completionRuleInput{RequiredCount: 0, ElectiveMinimum: 1, RequiredDone: 0, ElectiveDone: 1},
			expect: true,
		},
		{
			name:   "no elective minimum, required met",
			input:  completionRuleInput{RequiredCount: 1, ElectiveMinimum: 0, RequiredDone: 1, ElectiveDone: 0},
			expect: true,
		},
		{
			name:   "zero counts — trivially complete",
			input:  completionRuleInput{RequiredCount: 0, ElectiveMinimum: 0, RequiredDone: 0, ElectiveDone: 0},
			expect: true,
		},
		{
			name:   "required exceeded, elective minimum zero",
			input:  completionRuleInput{RequiredCount: 2, ElectiveMinimum: 0, RequiredDone: 5, ElectiveDone: 0},
			expect: true,
		},
		{
			name:   "one short on required",
			input:  completionRuleInput{RequiredCount: 4, ElectiveMinimum: 0, RequiredDone: 3, ElectiveDone: 0},
			expect: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateCompletion(tc.input)
			if got != tc.expect {
				t.Errorf("evaluateCompletion(%+v) = %v, want %v", tc.input, got, tc.expect)
			}
		})
	}
}
