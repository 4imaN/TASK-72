// tests/unit/recommendations_dedup_test.go — covers near-duplicate dedup in
// the recommendation diversity cap. The exported entry point is
// recommendations.ApplyDiversityCapForTest, but to avoid widening the public
// surface we replicate the dedup logic here as a behavioral assertion: send
// a known input through the public GetRecommendations would require a DB, so
// we instead exercise the package-level helper via a test-only export shim.
//
// Because dedupNearDuplicates is unexported, this test asserts the observable
// behaviour through the exported applyDiversityCap path on representative
// fixtures.
package unit_test

import (
	"testing"

	"portal/internal/app/recommendations"
)

func TestRecommendationsDedup_RemovesExactTitleDuplicates(t *testing.T) {
	in := []recommendations.RecommendedResource{
		{ResourceID: "a", Title: "Intro to Python", Category: "engineering", Score: 0.9},
		{ResourceID: "b", Title: "intro to python", Category: "data", Score: 0.7},  // case + category differ
		{ResourceID: "c", Title: "Advanced SQL",   Category: "data", Score: 0.5},
	}
	out := recommendations.ApplyDiversityCapForTest(in, 10)
	if len(out) != 2 {
		t.Fatalf("expected 2 results after dedup, got %d: %+v", len(out), out)
	}
	if out[0].ResourceID != "a" || out[1].ResourceID != "c" {
		t.Errorf("expected [a, c], got %+v", out)
	}
}

func TestRecommendationsDedup_RemovesNearDuplicatesByJaccard(t *testing.T) {
	// Two very similar titles — share most tokens after stopword removal.
	in := []recommendations.RecommendedResource{
		{ResourceID: "a", Title: "Effective Communication Skills",        Category: "comm",     Score: 0.95},
		{ResourceID: "b", Title: "Effective Communication Skills (v2)",   Category: "comm",     Score: 0.90},
		{ResourceID: "c", Title: "Project Management Fundamentals",       Category: "pm",       Score: 0.80},
	}
	out := recommendations.ApplyDiversityCapForTest(in, 10)
	if len(out) != 2 {
		t.Fatalf("expected 2 results after dedup, got %d: %+v", len(out), out)
	}
	if out[0].ResourceID != "a" || out[1].ResourceID != "c" {
		t.Errorf("expected higher-scored 'a' to survive; got %+v", out)
	}
}

func TestRecommendationsDedup_DistinctTitlesPreserved(t *testing.T) {
	// Ensure we do not over-dedup. Two resources with distinct topics survive.
	in := []recommendations.RecommendedResource{
		{ResourceID: "a", Title: "Compliance and Ethics", Category: "compliance",  Score: 0.9},
		{ResourceID: "b", Title: "Data Analysis Basics", Category: "data",        Score: 0.8},
		{ResourceID: "c", Title: "Leadership Principles", Category: "leadership",  Score: 0.7},
	}
	out := recommendations.ApplyDiversityCapForTest(in, 10)
	if len(out) != 3 {
		t.Fatalf("expected all 3 to survive, got %d: %+v", len(out), out)
	}
}

func TestRecommendationsDedup_HonorsDiversityCapAfterDedup(t *testing.T) {
	// 5 distinct titles all in the same category; cap = 40% of 5 = 2.
	in := []recommendations.RecommendedResource{
		{ResourceID: "a", Title: "Topic A", Category: "data", Score: 0.9},
		{ResourceID: "b", Title: "Topic B", Category: "data", Score: 0.8},
		{ResourceID: "c", Title: "Topic C", Category: "data", Score: 0.7},
		{ResourceID: "d", Title: "Topic D", Category: "data", Score: 0.6},
		{ResourceID: "e", Title: "Topic E", Category: "data", Score: 0.5},
	}
	out := recommendations.ApplyDiversityCapForTest(in, 5)
	if len(out) != 2 {
		t.Fatalf("expected 2 results (40%% of 5), got %d", len(out))
	}
}

// ── Tag-driven factor tests ─────────────────────────────────────────────────

func TestBuildFactors_TagOverlapOnlyWhenReal(t *testing.T) {
	// When tagSignal is zero, tag_overlap must NOT appear.
	factors := recommendations.BuildFactorsForTest(3.0, 1.0, 2.0, 0.0)
	for _, f := range factors {
		if f.Factor == "tag_overlap" {
			t.Error("tag_overlap must not appear when tagSignal is 0")
		}
	}
}

func TestBuildFactors_TagOverlapEmittedWhenPositive(t *testing.T) {
	// When tagSignal > 0, tag_overlap MUST appear.
	factors := recommendations.BuildFactorsForTest(0.0, 0.0, 0.0, 5.0)
	found := false
	for _, f := range factors {
		if f.Factor == "tag_overlap" {
			found = true
			if f.Label != "matches your skills" {
				t.Errorf("expected label 'matches your skills', got %q", f.Label)
			}
			if f.Weight <= 0 {
				t.Errorf("expected positive weight for tag_overlap, got %f", f.Weight)
			}
		}
	}
	if !found {
		t.Error("expected tag_overlap factor when tagSignal > 0")
	}
}

func TestBuildFactors_ViewHistoryNotLabeledTagOverlap(t *testing.T) {
	// View signal should now produce view_history, not tag_overlap.
	factors := recommendations.BuildFactorsForTest(3.0, 0.0, 0.0, 0.0)
	for _, f := range factors {
		if f.Factor == "tag_overlap" {
			t.Error("view signal should not produce tag_overlap factor")
		}
	}
	found := false
	for _, f := range factors {
		if f.Factor == "view_history" {
			found = true
		}
	}
	if !found {
		t.Error("expected view_history factor when viewSignal > 0")
	}
}

func TestBuildFactors_FallbackToPopularity(t *testing.T) {
	// When all signals are zero, fallback to popularity.
	factors := recommendations.BuildFactorsForTest(0, 0, 0, 0)
	if len(factors) != 1 {
		t.Fatalf("expected 1 factor (popularity), got %d", len(factors))
	}
	if factors[0].Factor != "popularity" {
		t.Errorf("expected popularity factor, got %q", factors[0].Factor)
	}
}

func TestBuildFactors_AllSignalsPresent(t *testing.T) {
	factors := recommendations.BuildFactorsForTest(2.0, 1.0, 3.0, 4.0)
	factorSet := map[string]bool{}
	for _, f := range factors {
		factorSet[f.Factor] = true
	}
	for _, expected := range []string{"tag_overlap", "prior_completion", "view_history", "co_occurrence"} {
		if !factorSet[expected] {
			t.Errorf("expected factor %q to be present", expected)
		}
	}
}
