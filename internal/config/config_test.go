package config

import "testing"

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
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(envFunc(map[string]string{
		"DATABASE_URL":   "postgres://db",
		"CLAUDE_MODEL":   "claude-sonnet-5",
		"ADZUNA_APP_ID":  "id123",
		"ADZUNA_APP_KEY": "key456",
		"HTTP_ADDR":      ":9999",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ClaudeModel != "claude-sonnet-5" {
		t.Errorf("ClaudeModel = %q", cfg.ClaudeModel)
	}
	if cfg.HTTPAddr != ":9999" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if !cfg.AdzunaConfigured() {
		t.Error("AdzunaConfigured() = false, want true when id and key set")
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

func TestJSearchConfigured(t *testing.T) {
	if (Config{}).JSearchConfigured() {
		t.Error("JSearchConfigured() = true, want false without a key")
	}
	if !(Config{RapidAPIKey: "k"}).JSearchConfigured() {
		t.Error("JSearchConfigured() = false, want true with a key")
	}
}

func TestAdzunaConfigured(t *testing.T) {
	cases := []struct {
		id, key string
		want    bool
	}{
		{"", "", false},
		{"id", "", false},
		{"", "key", false},
		{"id", "key", true},
	}
	for _, c := range cases {
		got := Config{AdzunaAppID: c.id, AdzunaAppKey: c.key}.AdzunaConfigured()
		if got != c.want {
			t.Errorf("AdzunaConfigured(%q,%q) = %v, want %v", c.id, c.key, got, c.want)
		}
	}
}
