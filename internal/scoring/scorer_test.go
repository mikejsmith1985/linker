package scoring

import (
	"context"
	"testing"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/store"
)

func TestScorerBlendsFitWithGate(t *testing.T) {
	// Strong skill fit, but salary + work-location gates should push it below 70.
	llm := &claude.Fake{Text: `{"fit": 95, "explanation": "Deep Go and distributed systems match."}`}
	scorer := NewScorer(llm)

	opening := store.JobOpening{
		Title: "Staff Engineer", Employer: "Acme", Location: "NYC",
		WorkLocationType: store.WorkOnsite, SalaryMin: 50000, SalaryMax: 60000,
	}
	prefs := store.Preferences{RequiredSalaryMin: 150000, WorkLocationPref: store.WorkRemote, WillingToRelocate: true}

	got, err := scorer.Score(context.Background(), "Go engineer, 10 years", opening, prefs)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if got.Value >= QualifyingScoreThreshold {
		t.Errorf("score = %d, want < %d after gates", got.Value, QualifyingScoreThreshold)
	}
	if got.IsQualifying {
		t.Error("IsQualifying = true, want false")
	}
	if got.GatePenalties[GateSalary] == 0 || got.GatePenalties[GateWorkLocation] == 0 {
		t.Errorf("expected salary+work-location gates, got %v", got.GatePenalties)
	}
}

func TestScorerQualifiesCleanMatch(t *testing.T) {
	llm := &claude.Fake{Text: `{"fit": 88, "explanation": "Excellent match."}`}
	scorer := NewScorer(llm)

	opening := store.JobOpening{Title: "Backend Engineer", WorkLocationType: store.WorkRemote}
	prefs := store.Preferences{WorkLocationPref: store.WorkRemote}

	got, err := scorer.Score(context.Background(), "profile", opening, prefs)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if got.Value != 88 || !got.IsQualifying {
		t.Errorf("score = %d qualifying = %v, want 88/true", got.Value, got.IsQualifying)
	}
}

func TestParseFitToleratesProse(t *testing.T) {
	fit, expl := parseFit("Sure! Here is my rating:\n{\"fit\": 73, \"explanation\": \"solid\"}\nHope that helps.")
	if fit != 73 {
		t.Errorf("fit = %d, want 73", fit)
	}
	if expl != "solid" {
		t.Errorf("explanation = %q, want solid", expl)
	}
}

func TestParseFitClampsOutOfRange(t *testing.T) {
	fit, _ := parseFit(`{"fit": 250, "explanation": "x"}`)
	if fit != MaxScore {
		t.Errorf("fit = %d, want clamped to %d", fit, MaxScore)
	}
}
