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
