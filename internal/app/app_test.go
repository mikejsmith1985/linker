package app

import (
	"testing"

	"github.com/mikejsmith1985/linker/internal/config"
	"github.com/mikejsmith1985/linker/internal/jobsource"
)

func TestBuildSourcesAlwaysIncludesRemotive(t *testing.T) {
	sources := buildSources(config.Config{})
	if len(sources) != 1 || sources[0].Name() != "remotive" {
		t.Fatalf("got %v, want [remotive] with no credentials", sourceNames(sources))
	}
}

func TestBuildSourcesAddsAdzunaWhenConfigured(t *testing.T) {
	sources := buildSources(config.Config{AdzunaAppID: "id", AdzunaAppKey: "key"})
	names := sourceNames(sources)
	if len(names) != 2 || names[0] != "remotive" || names[1] != "adzuna" {
		t.Errorf("sources = %v, want [remotive adzuna]", names)
	}
}

func sourceNames(sources []jobsource.Source) []string {
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name()
	}
	return names
}
