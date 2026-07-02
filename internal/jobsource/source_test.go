package jobsource

import (
	"context"
	"testing"
)

// TestHealthConstantValues guards the persisted source-health strings shown in
// the dashboard and stored on searches.
func TestHealthConstantValues(t *testing.T) {
	cases := map[string]string{
		HealthSucceeded: "succeeded",
		HealthFailed:    "failed",
		HealthNoResults: "no_results",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("health constant = %q, want %q", got, want)
		}
	}
}

// TestSourceInterfaceSatisfied is a compile-time-ish check that a minimal type
// can satisfy Source.
func TestSourceInterfaceSatisfied(t *testing.T) {
	var _ Source = stubSource{}
	got, err := stubSource{name: "x", out: []RawOpening{{Title: "t"}}}.Discover(context.Background(), Query{})
	if err != nil || len(got) != 1 {
		t.Errorf("Discover() = %v, %v", got, err)
	}
}

func TestFilterAndCapBounds(t *testing.T) {
	many := make([]RawOpening, 100)
	for i := range many {
		many[i] = RawOpening{Title: "Engineer"}
	}
	got := filterAndCap(many, []string{"engineer"}, defaultSourceCap)
	if len(got) != defaultSourceCap {
		t.Errorf("cap = %d, want %d", len(got), defaultSourceCap)
	}
	if none := filterAndCap(many, []string{"lawyer"}, defaultSourceCap); len(none) != 0 {
		t.Errorf("non-matching keyword kept %d, want 0", len(none))
	}
}

func TestMatchesAnyKeywordEmptyMatchesAll(t *testing.T) {
	if !matchesAnyKeyword("anything", nil) {
		t.Error("no keywords should match everything")
	}
}

func TestSearchTermsPrefersRoleTitlesCapped(t *testing.T) {
	q := Query{
		Keywords:   []string{"scrum", "kanban"},
		RoleTitles: []string{"Scrum Master", "Release Train Engineer", "Agile Coach", "Delivery Lead"},
	}
	terms := q.SearchTerms()
	if len(terms) != maxSearchTerms {
		t.Fatalf("got %d terms, want %d (capped)", len(terms), maxSearchTerms)
	}
	if terms[0] != "Scrum Master" {
		t.Errorf("first term = %q, want the first role title", terms[0])
	}
}

func TestSearchTermsFallsBackToJoinedKeywords(t *testing.T) {
	q := Query{Keywords: []string{"go", "postgres"}}
	terms := q.SearchTerms()
	if len(terms) != 1 || terms[0] != "go postgres" {
		t.Errorf("terms = %v, want [\"go postgres\"]", terms)
	}
}

func TestFilterKeywordsUnifiesSkillsAndRoles(t *testing.T) {
	q := Query{Keywords: []string{"scrum"}, RoleTitles: []string{"Agile Coach"}}
	got := q.FilterKeywords()
	if len(got) != 2 {
		t.Fatalf("got %v, want skills + role titles", got)
	}
}
