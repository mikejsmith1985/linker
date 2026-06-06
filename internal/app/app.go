// Package app wires linker's components together and runs the service: it
// connects to Postgres, builds the drafting/publishing/polling pipeline, starts
// the background poller, and serves the dashboard until the context is canceled.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mikejsmith1985/linker/internal/buffer"
	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/config"
	"github.com/mikejsmith1985/linker/internal/github"
	"github.com/mikejsmith1985/linker/internal/orchestrator"
	"github.com/mikejsmith1985/linker/internal/persona"
	"github.com/mikejsmith1985/linker/internal/store"
	"github.com/mikejsmith1985/linker/internal/web"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Run boots the service and blocks until ctx is canceled or the HTTP server
// fails. It owns all external connections.
func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	st := store.New(pool)
	if err := st.RunMigrations(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	systemPrompt, err := persona.Load(cfg.PersonaPromptPath)
	if err != nil {
		return fmt.Errorf("load persona: %w", err)
	}

	ac := anthropic.NewClient(option.WithAPIKey(cfg.AnthropicAPIKey))
	drafter := claude.NewClient(&ac.Messages, cfg.ClaudeModel, systemPrompt)

	source := github.New(cfg.GitHubToken)
	publisher := SelectPublisher(cfg, log)
	orch := orchestrator.New(st, source, drafter, cfg.GitHubRepos, log)
	server := web.NewServer(st, publisher, orch, log)

	// Background poller.
	go pollLoop(ctx, cfg.PollInterval, orch.Tick, log)

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	log.Info("linker started",
		"addr", cfg.HTTPAddr,
		"repos", cfg.GitHubRepos,
		"publisher", publisherName(cfg),
		"poll_interval", cfg.PollInterval.String())

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// SelectPublisher returns a live Buffer client when credentials are configured,
// otherwise a stub that records posts locally.
func SelectPublisher(cfg config.Config, log *slog.Logger) buffer.Publisher {
	if cfg.BufferConfigured() {
		return buffer.NewLiveClient(cfg.BufferAccessToken, cfg.BufferProfileID)
	}
	return buffer.NewStub(log)
}

func publisherName(cfg config.Config) string {
	if cfg.BufferConfigured() {
		return "buffer"
	}
	return "stub"
}

// pollLoop runs tick immediately, then on every interval, until ctx is done.
// A tick error is logged but never stops the loop.
func pollLoop(ctx context.Context, interval time.Duration, tick func(context.Context) error, log *slog.Logger) {
	runOnce := func() {
		if err := tick(ctx); err != nil {
			log.Error("poll tick failed", "err", err)
		}
	}
	runOnce()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}
