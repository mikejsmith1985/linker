// Package app wires the job matcher together and runs the service: it connects
// to Postgres, builds the resume-ingest / discovery / scoring pipeline, and
// serves the dashboard until the context is canceled.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/config"
	"github.com/mikejsmith1985/linker/internal/documents"
	"github.com/mikejsmith1985/linker/internal/jobsource"
	"github.com/mikejsmith1985/linker/internal/orchestrator"
	"github.com/mikejsmith1985/linker/internal/resume"
	"github.com/mikejsmith1985/linker/internal/scoring"
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

	ac := anthropic.NewClient(option.WithAPIKey(cfg.AnthropicAPIKey))
	llm := claude.NewClient(&ac.Messages, cfg.ClaudeModel)

	ingestor := resume.NewService(llm, st)
	scorer := scoring.NewScorer(llm)
	docService := documents.NewService(documents.NewGenerator(llm), st)

	sources := buildSources(cfg)
	if cfg.EnableBrowserSource {
		// The browser source self-gates on the user's saved acknowledgment, read
		// fresh at each search so toggling it in preferences takes effect.
		ackProvider := func() bool {
			prefs, err := st.GetPreferences(context.Background())
			return err == nil && prefs.BrowserAutomationAck
		}
		sources = append(sources, jobsource.NewBrowser(ackProvider, jobsource.NewPlaywrightRunner()))
	}
	registry := jobsource.NewRegistry(sources...)
	urlFactory := func(urls []string) orchestrator.Discoverer {
		return jobsource.NewRegistry(jobsource.NewURLPaste(urls, llm))
	}
	orch := orchestrator.New(st, registry, scorer, docService, urlFactory, log)
	server := web.NewServer(st, ingestor, orch, docService, log)

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
		"adzuna", cfg.AdzunaConfigured())

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// buildSources assembles the enabled discovery sources. The Adzuna aggregator is
// included when credentials are configured; user-pasted URLs and the opt-in
// browser source are added in later increments.
func buildSources(cfg config.Config) []jobsource.Source {
	var sources []jobsource.Source
	if cfg.AdzunaConfigured() {
		sources = append(sources, jobsource.NewAdzuna(cfg.AdzunaAppID, cfg.AdzunaAppKey))
	}
	return sources
}
