# Changelog — linker

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Forge Workflow initialized with Forge Terminal Workflow Architect
- Standalone `linker` service that drafts LinkedIn posts from GitHub activity:
  - Environment-only configuration (`internal/config`), no hardcoded credentials.
  - Postgres persistence for activity events, repo cursors, and posts
    (`internal/store`), with idempotent startup migrations.
  - GitHub poller that detects new commits, releases, and README changes and
    de-duplicates them by repo/type/ref, baselining on first sight
    (`internal/github`).
  - Claude-backed drafting (`claude-opus-4-8`, adaptive thinking) in the user's
    agile-delivery + AI-implementation voice (`internal/claude`, `internal/persona`).
  - Buffer publishing behind a `Publisher` interface: live client plus a stub
    used automatically when Buffer is not configured (`internal/buffer`).
  - Orchestrator that polls, de-duplicates, drafts, and stores for review, plus
    draft regeneration (`internal/orchestrator`).
  - templ + HTMX review dashboard to edit, regenerate, reject, approve/queue
    posts, and view post history for cadence (`internal/web`).
  - Service wiring with a background poller and graceful shutdown
    (`internal/app`, `cmd/linker`).
  - Docker Compose stack (Postgres + app), Dockerfile, Makefile, `.env.example`,
    and README for one-command local setup.

### Changed

### Fixed
- Pre-push hook (`.forge/hooks/pre-push` and `.forge/hooks/pre-push.ps1`) no
  longer false-fails on every push. The scaffold template hardcoded
  `go build ./cmd/forge/` (Forge Terminal's own entrypoint), which does not
  exist here. It now runs `go tool templ generate` first — so the gitignored
  `*_templ.go` files exist — then builds every package with `go build ./...`,
  making the check entrypoint-agnostic.

### Removed
