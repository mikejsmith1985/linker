package config

import (
	"testing"
	"time"
)

// envFunc builds a Getenv from a map for deterministic, isolated tests.
func envFunc(m map[string]string) Getenv {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(envFunc(map[string]string{
		"DATABASE_URL": "postgres://x",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ClaudeModel != defaultClaudeModel {
		t.Errorf("ClaudeModel = %q, want %q", cfg.ClaudeModel, defaultClaudeModel)
	}
	if cfg.PollInterval != defaultPollInterval {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, defaultPollInterval)
	}
	if cfg.PostMinInterval != defaultPostMinInterval {
		t.Errorf("PostMinInterval = %v, want %v", cfg.PostMinInterval, defaultPostMinInterval)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.GitHubRepos != nil {
		t.Errorf("GitHubRepos = %v, want nil", cfg.GitHubRepos)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(envFunc(map[string]string{
		"DATABASE_URL":      "postgres://db",
		"CLAUDE_MODEL":      "claude-sonnet-4-6",
		"GITHUB_REPOS":      " owner/a , owner/b ,, owner/c ",
		"POLL_INTERVAL":     "30s",
		"POST_MIN_INTERVAL": "1h",
		"HTTP_ADDR":         ":9999",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ClaudeModel != "claude-sonnet-4-6" {
		t.Errorf("ClaudeModel = %q", cfg.ClaudeModel)
	}
	want := []string{"owner/a", "owner/b", "owner/c"}
	if len(cfg.GitHubRepos) != len(want) {
		t.Fatalf("GitHubRepos = %v, want %v", cfg.GitHubRepos, want)
	}
	for i := range want {
		if cfg.GitHubRepos[i] != want[i] {
			t.Errorf("GitHubRepos[%d] = %q, want %q", i, cfg.GitHubRepos[i], want[i])
		}
	}
	if cfg.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v", cfg.PollInterval)
	}
	if cfg.PostMinInterval != time.Hour {
		t.Errorf("PostMinInterval = %v", cfg.PostMinInterval)
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	if _, err := Load(envFunc(map[string]string{
		"DATABASE_URL":  "postgres://db",
		"POLL_INTERVAL": "not-a-duration",
	})); err == nil {
		t.Fatal("expected error for invalid POLL_INTERVAL, got nil")
	}
}

func TestLoadNonPositiveDuration(t *testing.T) {
	if _, err := Load(envFunc(map[string]string{
		"DATABASE_URL":      "postgres://db",
		"POST_MIN_INTERVAL": "-5m",
	})); err == nil {
		t.Fatal("expected error for non-positive POST_MIN_INTERVAL, got nil")
	}
}

func TestValidate(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Error("expected error when DATABASE_URL missing")
	}
	if err := (Config{DatabaseURL: "x"}).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBufferConfigured(t *testing.T) {
	cases := []struct {
		token, profile string
		want           bool
	}{
		{"", "", false},
		{"tok", "", false},
		{"", "prof", false},
		{"tok", "prof", true},
	}
	for _, c := range cases {
		got := Config{BufferAccessToken: c.token, BufferProfileID: c.profile}.BufferConfigured()
		if got != c.want {
			t.Errorf("BufferConfigured(%q,%q) = %v, want %v", c.token, c.profile, got, c.want)
		}
	}
}
