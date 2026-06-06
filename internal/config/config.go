// Package config loads linker's runtime configuration exclusively from
// environment variables. No credential is ever hardcoded; everything that is
// secret (API keys, tokens) is read from the environment at startup.
package config

import (
	"fmt"
	"strings"
	"time"
)

// Config holds all runtime settings for the linker service.
type Config struct {
	DatabaseURL string

	AnthropicAPIKey string
	ClaudeModel     string

	GitHubToken string
	GitHubRepos []string

	BufferAccessToken string
	BufferProfileID   string

	PollInterval    time.Duration
	PostMinInterval time.Duration

	PersonaPromptPath string
	HTTPAddr          string
}

// Getenv matches the signature of os.Getenv and lets tests inject a fake
// environment without touching the real process environment.
type Getenv func(string) string

const (
	defaultClaudeModel     = "claude-opus-4-8"
	defaultPollInterval    = 15 * time.Minute
	defaultPostMinInterval = 24 * time.Hour
	defaultHTTPAddr        = ":8080"
)

// Load reads configuration using the supplied getenv function (pass os.Getenv
// in production) and applies defaults. It returns an error only when a value is
// present but malformed; missing optional values fall back to defaults.
func Load(getenv Getenv) (Config, error) {
	cfg := Config{
		DatabaseURL:       getenv("DATABASE_URL"),
		AnthropicAPIKey:   getenv("ANTHROPIC_API_KEY"),
		ClaudeModel:       firstNonEmpty(getenv("CLAUDE_MODEL"), defaultClaudeModel),
		GitHubToken:       getenv("GITHUB_TOKEN"),
		GitHubRepos:       parseRepos(getenv("GITHUB_REPOS")),
		BufferAccessToken: getenv("BUFFER_ACCESS_TOKEN"),
		BufferProfileID:   getenv("BUFFER_PROFILE_ID"),
		PersonaPromptPath: getenv("PERSONA_PROMPT_PATH"),
		HTTPAddr:          firstNonEmpty(getenv("HTTP_ADDR"), defaultHTTPAddr),
	}

	var err error
	if cfg.PollInterval, err = parseDuration(getenv("POLL_INTERVAL"), defaultPollInterval); err != nil {
		return Config{}, fmt.Errorf("POLL_INTERVAL: %w", err)
	}
	if cfg.PostMinInterval, err = parseDuration(getenv("POST_MIN_INTERVAL"), defaultPostMinInterval); err != nil {
		return Config{}, fmt.Errorf("POST_MIN_INTERVAL: %w", err)
	}

	return cfg, nil
}

// Validate reports configuration that would prevent the service from doing
// useful work. It is intentionally lenient: the app can boot and serve the
// dashboard even before Claude/GitHub credentials are supplied, so only the
// database is strictly required.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	return nil
}

// BufferConfigured reports whether a live Buffer publisher should be used.
// When false, the caller falls back to the stub publisher.
func (c Config) BufferConfigured() bool {
	return c.BufferAccessToken != "" && c.BufferProfileID != ""
}

func parseRepos(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	repos := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			repos = append(repos, t)
		}
	}
	if len(repos) == 0 {
		return nil
	}
	return repos
}

func parseDuration(raw string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be positive, got %q", raw)
	}
	return d, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
