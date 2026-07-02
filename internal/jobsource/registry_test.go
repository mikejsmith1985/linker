package jobsource

import (
	"context"
	"errors"
	"testing"

	"github.com/mikejsmith1985/linker/internal/store"
)

// stubSource is a canned in-memory Source for registry tests.
type stubSource struct {
	name string
	out  []RawOpening
	err  error
}

func (s stubSource) Name() string { return s.name }
func (s stubSource) Discover(context.Context, Query) ([]RawOpening, error) {
	return s.out, s.err
}

func TestRegistryDeduplicatesAcrossSources(t *testing.T) {
	a := stubSource{name: "adzuna", out: []RawOpening{
		{Title: "Senior Engineer", Employer: "Acme", Location: "Remote", SourceName: "adzuna", OriginalURL: "https://a"},
	}}
	// Same role, different formatting + different board.
	b := stubSource{name: "jooble", out: []RawOpening{
		{Title: "  senior   engineer ", Employer: "ACME", Location: "remote", SourceName: "jooble", SalaryMin: 100, SalaryMax: 150},
	}}

	reg := NewRegistry(a, b)
	openings, health := reg.Discover(context.Background(), Query{})

	if len(openings) != 1 {
		t.Fatalf("got %d openings, want 1 (deduped)", len(openings))
	}
	got := openings[0]
	if len(got.SourceNames) != 2 {
		t.Errorf("SourceNames = %v, want both boards", got.SourceNames)
	}
	if got.SalaryMax != 150 {
		t.Errorf("SalaryMax = %d, want 150 (richer record wins)", got.SalaryMax)
	}
	if got.OriginalURL != "https://a" {
		t.Errorf("OriginalURL = %q, want kept from first", got.OriginalURL)
	}
	if health["adzuna"] != HealthSucceeded || health["jooble"] != HealthSucceeded {
		t.Errorf("health = %v, want both succeeded", health)
	}
}

func TestRegistryRecordsFailedSourceWithoutAborting(t *testing.T) {
	good := stubSource{name: "adzuna", out: []RawOpening{
		{Title: "Engineer", Employer: "Acme", Location: "NYC", SourceName: "adzuna"},
	}}
	bad := stubSource{name: "broken", err: errors.New("rate limited")}

	reg := NewRegistry(bad, good)
	openings, health := reg.Discover(context.Background(), Query{})

	if len(openings) != 1 {
		t.Fatalf("got %d openings, want 1 from the working source", len(openings))
	}
	if health["broken"] != HealthFailed {
		t.Errorf("health[broken] = %q, want failed", health["broken"])
	}
	if health["adzuna"] != HealthSucceeded {
		t.Errorf("health[adzuna] = %q, want succeeded", health["adzuna"])
	}
}

func TestRegistryRecordsNoResults(t *testing.T) {
	empty := stubSource{name: "adzuna", out: nil}
	reg := NewRegistry(empty)
	_, health := reg.Discover(context.Background(), Query{})
	if health["adzuna"] != HealthNoResults {
		t.Errorf("health[adzuna] = %q, want no_results", health["adzuna"])
	}
}

func TestCleanUTF8StripsInvalidAndNull(t *testing.T) {
	// Incomplete em-dash sequence (0xe2 0x80) and a null byte must be stripped.
	if got := cleanUTF8("ok\xe2\x80"); got != "ok" {
		t.Errorf("cleanUTF8 = %q, want ok (invalid bytes stripped)", got)
	}
	if got := cleanUTF8("a\x00b"); got != "ab" {
		t.Errorf("cleanUTF8 = %q, want ab (null stripped)", got)
	}
	if got := cleanUTF8("clean—text"); got != "clean—text" {
		t.Errorf("cleanUTF8 mangled valid text: %q", got)
	}
}

func TestToOpeningSanitizes(t *testing.T) {
	op := toOpening(RawOpening{Title: "Eng\xe2\x80", Description: "d\x00"}, "k")
	if op.Title != "Eng" || op.Description != "d" {
		t.Errorf("toOpening did not sanitize: %+v", op)
	}
}

func TestWorkLocationMapping(t *testing.T) {
	if workLocation("Remote") != store.WorkRemote {
		t.Error("Remote should map to WorkRemote")
	}
	if workLocation("weird") != store.WorkUnknown {
		t.Error("unknown value should map to WorkUnknown")
	}
}
