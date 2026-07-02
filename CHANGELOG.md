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
- **Repurposed `linker` from a GitHub→LinkedIn post drafter into a resume-driven
  job matcher** (spec `specs/001-resume-job-matcher/`). MVP (User Story 1) delivered:
  - Removed the post-drafter domain (`internal/github`, `internal/buffer`,
    `internal/persona`) and the GitHub poller.
  - New env-only config keys: `ADZUNA_APP_ID`/`ADZUNA_APP_KEY`; dropped
    `GITHUB_*`/`BUFFER_*`/poll-cadence settings (`internal/config`).
  - Postgres schema for resumes, preferences, searches, job openings, scored
    match results, generated documents, and selections (`internal/store`).
  - Resume ingestion: PDF/DOCX/TXT text extraction plus LLM profile structuring,
    with empty/unreadable input rejected (`internal/resume`).
  - Pluggable job sources behind one interface with canonical-key de-duplication;
    Adzuna aggregator adapter as the default source (`internal/jobsource`).
  - 1–100 scoring: a deterministic gate (salary + work-location are strong gates,
    travel/relocate soft) blended with an LLM skill-fit judgment (`internal/scoring`).
  - Repurposed `claude` package into a general mockable `LLM` interface + fake so
    scoring/resume/document code is unit-tested fully mocked in <10ms.
  - `RunSearch` orchestrator: discover → dedup → gate → score → persist → rank,
    excluding sub-70 openings and reusing prior scores on re-run (`internal/orchestrator`).
  - templ + HTMX dashboard to upload a resume, set preferences, run a search, and
    review scored matches with per-source health (`internal/web`). CSS is served as
    a cacheable `/static/styles.css` asset, never inlined.

### Fixed
- Pre-push hook (`.forge/hooks/pre-push` and `.forge/hooks/pre-push.ps1`) no
  longer false-fails on every push. The scaffold template hardcoded
  `go build ./cmd/forge/` (Forge Terminal's own entrypoint), which does not
  exist here. It now runs `go tool templ generate` first — so the gitignored
  `*_templ.go` files exist — then builds every package with `go build ./...`,
  making the check entrypoint-agnostic.

### Removed
