package app

import (
	"testing"

	"github.com/mikejsmith1985/linker/internal/config"
	"github.com/mikejsmith1985/linker/internal/jobsource"
)

func TestBuildSourcesIncludesKeyFreeDefaults(t *testing.T) {
	names := sourceNames(buildSources(config.Config{}))
	want := []string{"remotive", "remoteok", "arbeitnow", "jobicy"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want the key-free defaults %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("source[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestBuildSourcesAddsAdzunaWhenConfigured(t *testing.T) {
	names := sourceNames(buildSources(config.Config{AdzunaAppID: "id", AdzunaAppKey: "key"}))
	if len(names) != 5 || names[len(names)-1] != "adzuna" {
		t.Errorf("sources = %v, want key-free defaults + adzuna last", names)
	}
}

func TestBuildSourcesAddsJSearchWhenConfigured(t *testing.T) {
	names := sourceNames(buildSources(config.Config{RapidAPIKey: "rk"}))
	if len(names) != 5 || names[len(names)-1] != "jsearch" {
		t.Errorf("sources = %v, want key-free defaults + jsearch", names)
	}
}

func sourceNames(sources []jobsource.Source) []string {
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name()
	}
	return names
}
