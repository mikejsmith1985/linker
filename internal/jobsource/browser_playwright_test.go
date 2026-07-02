package jobsource

import (
	"strings"
	"testing"
)

// The live Playwright runner cannot be exercised without installed browser
// binaries, so these tests cover the parts that are pure: construction and that
// it satisfies the BrowserRunner interface used by the gated Browser source.
func TestNewPlaywrightRunnerConfigured(t *testing.T) {
	runner := NewPlaywrightRunner()
	if runner == nil {
		t.Fatal("NewPlaywrightRunner returned nil")
	}
	if !strings.Contains(runner.searchURLTemplate, "%s") {
		t.Errorf("search URL template %q missing a keyword placeholder", runner.searchURLTemplate)
	}
}

func TestPlaywrightRunnerSatisfiesBrowserRunner(t *testing.T) {
	var _ BrowserRunner = NewPlaywrightRunner()
}
