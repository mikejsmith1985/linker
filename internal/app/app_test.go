package app

import (
	"testing"

	"github.com/mikejsmith1985/linker/internal/config"
)

func TestBuildSourcesIncludesAdzunaWhenConfigured(t *testing.T) {
	sources := buildSources(config.Config{AdzunaAppID: "id", AdzunaAppKey: "key"})
	if len(sources) != 1 {
		t.Fatalf("got %d sources, want 1", len(sources))
	}
	if sources[0].Name() != "adzuna" {
		t.Errorf("source = %q, want adzuna", sources[0].Name())
	}
}

func TestBuildSourcesEmptyWithoutCredentials(t *testing.T) {
	if got := buildSources(config.Config{}); len(got) != 0 {
		t.Errorf("got %d sources, want 0 when Adzuna not configured", len(got))
	}
}
