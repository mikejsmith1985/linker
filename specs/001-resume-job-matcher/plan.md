# Implementation Plan: Resume-Driven Job Matcher

**Branch**: `feature/resume-job-matcher` (to be created from `main`) | **Date**: 2026-07-02 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/001-resume-job-matcher/spec.md`

## Summary

Repurpose the existing single-user, self-hosted `linker` service from a GitHub→LinkedIn post drafter into a resume-driven job matcher. The user uploads one active resume and states preferences (required salary, work-location, willingness to travel/relocate). The service discovers openings through compliant job-search APIs (primary), user-pasted posting URLs, and an opt-in browser-automation mode for non-API sources. Each opening is scored 1–100 combining LLM skill-fit with deterministic preference gating (salary + work-location are strong gates; travel/relocate are soft factors). Openings below 70 are dropped. The three highest-scoring openings get an auto-generated no-fabrication tailored resume and cover letter; the rest generate on first open and cache. Results, scores, documents, and selections persist in Postgres and render in a templ/HTMX dashboard where the user reviews, edits, selects, and opens the live posting to apply manually. The system never submits an application.

Technical approach: keep the framework scaffolding already proven in this repo (chi router, templ + HTMX views, pgx/Postgres store, Anthropic SDK, env config, orchestrator/app wiring, docker-compose). Replace the domain packages (`github`, `buffer`, `persona`, `orchestrator` poster loop) with job-matching packages (`resume`, `jobsource`, `scoring`, `documents`, and a repurposed `orchestrator`).

## Technical Context

**Language/Version**: Go 1.25.0 (existing `go.mod`)

**Primary Dependencies** (reused): `github.com/go-chi/chi/v5` (routing), `github.com/a-h/templ` (server-rendered views) + HTMX (progressive interactivity), `github.com/jackc/pgx/v5` (Postgres), `github.com/anthropics/anthropic-sdk-go` (Claude — scoring + document generation), `golang.org/x/oauth2` (already present).

**Primary Dependencies** (new):
- `github.com/playwright-community/playwright-go` — opt-in browser-automation discovery for non-API sources (honors the user's explicit Playwright request; requires browser binaries, which fits the default-off gating).
- A PDF text extractor (`github.com/ledongthuc/pdf`) and DOCX extractor (stdlib `archive/zip` + `encoding/xml` over `word/document.xml`) for resume ingestion; plain text handled directly.
- Job aggregator client via stdlib `net/http` against a compliant JSON API (Adzuna as the default adapter; see research.md).

**Storage**: PostgreSQL (existing `internal/store` with `pgx` + `schema.sql`; `pgxmock` for unit tests).

**Testing**: Go `testing` + `pgxmock/v4` for unit (mocked, <10ms), testcontainers-style real Postgres for integration, `go test ./...` via `make test`. TDD Red→Green→Refactor per Article V.

**Target Platform**: Linux container (docker-compose) and local `make run`; single-user self-hosted.

**Project Type**: Single Go web service (monolith with server-rendered UI). Not split frontend/backend.

**Performance Goals**: A full on-demand search across enabled API sources (up to ~100 raw postings), including de-dup, gating, LLM scoring, and top-3 document generation, completes in under ~60s. An on-demand document generation for a single opening returns in under ~10s. Dashboard interactions (HTMX partials) render in under ~300ms excluding LLM calls.

**Constraints**: Secrets (ANTHROPIC_API_KEY, job-API keys) never in plaintext in code/logs — injected via env/Forge Vault (Article IX). Browser-automation discovery is default-off and requires explicit user risk acknowledgment before running (FR-022/023). No-fabrication rule enforced on generated documents (FR-007a).

**Scale/Scope**: One user, one active resume, on-demand searches; a search retains up to a few hundred openings; scheduled/continuous discovery is designed-for but out of the initial build (FR-020).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.* No `.specify/memory/constitution.md` exists; the governing rules are the Forge Articles (global `CLAUDE.md`) and `.github/copilot-instructions.md`.

| Article | Gate | Status |
|---------|------|--------|
| I — Prime Directive (BEST route) | Production-ready design, reuse proven framework, no quick-and-dirty | PASS |
| III — Branching (GitHub Flow) | Work lands on `feature/resume-job-matcher`, PR to `main`; never commit to `main` | PASS (branch to be created before implementation) |
| IV — Code Quality | Self-documenting names, verb-first funcs, <40-line funcs, file/function doc comments, no magic numbers (70 threshold, top-3 count become named constants) | PASS (enforced during implementation) |
| V — Testing (three layers, TDD) | Unit mocked <10ms; integration real Postgres; TDD ordering. Scoring gating and no-fabrication are unit-testable; source adapters integration-tested | PASS |
| VI — Documentation | `CHANGELOG.md` updated per PR; no auxiliary status docs; `specs/001-.../` tree is the exempt pipeline artifact | PASS |
| VII — Framework-First | Reuse chi/templ/pgx/anthropic already in repo; new libs (Playwright, PDF/DOCX parse, job API) fill documented gaps, not rebuilt infra | PASS |
| VIII — Release (local pipeline) | No GitHub Actions release; `git tag` + `gh release create` or `scripts/local-release.ps1` if present | PASS (no release in this feature) |
| IX — Vault Zero-Knowledge | API keys via env/vault; never logged | PASS |
| X — Verification & Proof | quickstart.md defines runnable end-to-end validation; behavior verified, not just compile | PASS |
| XI — Output/Dashboard Restraint | Single app dashboard; no extra HTML dashboards; no narrated phase names | PASS |

**Result: GATE PASSED.** No violations; Complexity Tracking not required.

## Project Structure

### Documentation (this feature)

```text
specs/001-resume-job-matcher/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (internal interface contracts)
│   ├── jobsource.md
│   ├── scoring.md
│   ├── documents.md
│   └── http-routes.md
├── checklists/
│   └── requirements.md  # from /speckit-specify + /speckit-clarify
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

Repurposes the existing single-project Go layout. Packages to **remove/replace** from the current post-drafter: `internal/github`, `internal/buffer`, `internal/persona`, and the GitHub-poller behavior in `internal/orchestrator`.

```text
cmd/linker/
└── main.go                     # wiring: config → store → sources → orchestrator → web (reused, re-pointed)

internal/
├── config/                     # env config (reused; new keys: job-API creds, feature flags)
├── store/                      # pgx store + schema.sql (reused; new tables — see data-model.md)
│   ├── store.go
│   ├── models.go
│   └── schema.sql
├── resume/                     # NEW: ingest + parse (PDF/DOCX/TXT) → extracted profile
│   ├── parser.go               #   text extraction per format
│   └── profile.go              #   LLM-assisted skill/experience extraction
├── jobsource/                  # NEW: discovery adapters behind one interface
│   ├── source.go               #   Source interface + registry + de-dup
│   ├── aggregator.go           #   compliant API adapter (Adzuna default)
│   ├── urlpaste.go             #   fetch + parse a single pasted posting URL
│   └── browser.go              #   opt-in Playwright discovery (default-off, gated)
├── scoring/                    # NEW: 1–100 fit scoring
│   ├── gate.go                 #   deterministic salary + work-location gating
│   └── scorer.go               #   LLM skill-fit + preference blend + explanation
├── documents/                  # NEW: tailored resume + cover letter generation
│   └── generator.go            #   no-fabrication generation (eager top-3 / on-demand)
├── orchestrator/               # REPURPOSED: run a search end-to-end (discover→dedup→gate→score→persist→gen top-3)
│   └── orchestrator.go
├── app/                        # app lifecycle wiring (reused, re-pointed)
├── buffer/ (REMOVED)           # auto-submit deleted — system never submits (FR-013)
└── web/                        # chi + templ/HTMX (reused; new views)
    ├── server.go               #   routes: resume upload, preferences, search, results, doc review, select/open
    ├── layout.templ
    ├── results.templ           #   NEW: single consolidated scored view
    ├── job.templ               #   NEW: one opening — score, explanation, docs, select/open
    └── settings.templ          #   NEW: preferences + source toggles + browser-automation acknowledgment
```

**Structure Decision**: Single Go web service (Option 1, single project). This matches the existing repo and the single-user self-hosted model; no separate frontend/backend split is warranted because the UI is server-rendered templ/HTMX served by the same binary.

## Complexity Tracking

Not required — Constitution Check passed with no violations.
