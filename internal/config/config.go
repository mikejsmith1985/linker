// Package config loads linker's runtime configuration exclusively from
// environment variables. No credential is ever hardcoded; everything that is
// secret (API keys) is read from the environment at startup.
package config

import (
	"fmt"
	"strings"
)

// Config holds all runtime settings for the resume-driven job matcher.
type Config struct {
	DatabaseURL string

	AnthropicAPIKey string
	ClaudeModel     string

	// Adzuna is the default compliant job-aggregator source.
	AdzunaAppID  string
	AdzunaAppKey string

	HTTPAddr string
}

// Getenv matches the signature of os.Getenv and lets tests inject a fake
// environment without touching the real process environment.
type Getenv func(string) string

const (
	defaultClaudeModel = "claude-opus-4-8"
	defaultHTTPAddr    = ":8080"
)

// Load reads configuration using the supplied getenv function (pass os.Getenv
// in production) and applies defaults. It returns an error only when a value is
// present but malformed; missing optional values fall back to defaults.
func Load(getenv Getenv) (Config, error) {
	cfg := Config{
		DatabaseURL:     getenv("DATABASE_URL"),
		AnthropicAPIKey: getenv("ANTHROPIC_API_KEY"),
		ClaudeModel:     firstNonEmpty(getenv("CLAUDE_MODEL"), defaultClaudeModel),
		AdzunaAppID:     getenv("ADZUNA_APP_ID"),
		AdzunaAppKey:    getenv("ADZUNA_APP_KEY"),
		HTTPAddr:        firstNonEmpty(getenv("HTTP_ADDR"), defaultHTTPAddr),
	}
	return cfg, nil
}

// Validate reports configuration that would prevent the service from doing
// useful work. It is intentionally lenient: the app can boot and serve the
// dashboard even before Claude/Adzuna credentials are supplied, so only the
// database is strictly required.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	return nil
}

// AdzunaConfigured reports whether the Adzuna aggregator source can run.
// When false, the source registry skips it (and reports it as unavailable).
func (c Config) AdzunaConfigured() bool {
	return c.AdzunaAppID != "" && c.AdzunaAppKey != ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
