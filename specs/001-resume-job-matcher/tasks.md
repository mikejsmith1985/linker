# Tasks: Resume-Driven Job Matcher

**Input**: Design documents from `specs/001-resume-job-matcher/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: INCLUDED — the constitution (Article V) mandates TDD (Red→Green→Refactor). Test tasks precede their implementation and must fail first.

**Organization**: Grouped by user story (derived from the spec's primary flow + functional requirements) so each is an independently testable increment.

**Go convention**: tests are `_test.go` files colocated with the package under test (matches the existing repo). Unit tests use `pgxmock/v4` and run <10ms; integration tests use a real Postgres.

## Derived user stories & priorities

| Story | Priority | Delivers | Spec refs |
|-------|----------|----------|-----------|
| US1 | P1 (MVP) | Upload resume, set preferences, run a search, see qualifying scored openings in one view | FR-001–006, FR-009, FR-014–019, FR-024 |
| US2 | P2 | No-fabrication tailored resume + cover letter (eager top-3, on-demand rest); review/edit/download | FR-007, FR-007a, FR-008, FR-010 |
| US3 | P3 | Select openings and open the live posting for manual submission (never auto-submit) | FR-011, FR-012, FR-013 |
| US4 | P4 | Paste one or more posting URLs and have each scored/tailored like a discovered opening | FR-021 |
| US5 | P5 | Opt-in Playwright browser discovery for non-API sources, gated by risk acknowledgment | FR-022, FR-023 |

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Branch, repurpose the repo off the old post-drafter domain, and scaffold new packages.

- [x] T001 Reconcile the working tree, then create branch `feature/resume-job-matcher` from a clean `main` (Article III; **P1**): commit/stash/discard the current uncommitted changes to `internal/web/layout.templ`, `internal/web/server.go`, `internal/web/server_test.go`, and `CHANGELOG.md` on `fix/dashboard-stylesheet-not-rendering` first, since T012/T025 rewrite those files. Do not work on `main`
- [x] T002 [P] Remove obsolete domain packages `internal/github/`, `internal/buffer/`, `internal/persona/` and all references to them in `cmd/linker/main.go`, `internal/app/app.go`, `internal/orchestrator/orchestrator.go`
- [x] T003 [P] Add dependencies `github.com/playwright-community/playwright-go` and `github.com/ledongthuc/pdf` to `go.mod`; run `go mod tidy`
- [x] T004 [P] Update `.env.example` and `internal/config/config.go`: remove `GITHUB_*`/`BUFFER_*`, add `ADZUNA_APP_ID`/`ADZUNA_APP_KEY`, source-enable flags; update `internal/config/config_test.go`
- [x] T005 [P] Scaffold empty package dirs with purpose-comment doc files: `internal/resume/`, `internal/jobsource/`, `internal/scoring/`, `internal/documents/`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Persistence, the job-source interface + de-dup, scoring constants, and the orchestrator skeleton — everything the user stories build on.

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

- [x] T006 Extend `internal/store/schema.sql` with tables `resumes`, `search_preferences`, `searches`, `job_openings`, `match_results`, `generated_documents`, `selections` per `data-model.md`
- [x] T007 [P] Add Go structs for all seven entities in `internal/store/models.go`
- [x] T008 Implement pgx CRUD for each entity in `internal/store/store.go`, with `pgxmock` unit tests in `internal/store/store_test.go` (write failing tests first)
- [x] T009 [P] Define `Source` interface, `Query`, `RawOpening` in `internal/jobsource/source.go` per `contracts/jobsource.md`
- [x] T010 Implement source registry + canonical-key de-duplication in `internal/jobsource/registry.go` with failing-first unit tests in `internal/jobsource/registry_test.go` (FR-014, SC-007)
- [x] T011 [P] Define named scoring constants (`QualifyingScoreThreshold`=70, `EagerDocumentTopN`=3, gate penalties) in `internal/scoring/constants.go` per `contracts/scoring.md`
- [x] T011a [P] Define a mockable Claude/LLM client interface (`Complete`/`Structured` methods) plus an in-memory fake in `internal/claude/client.go` and `internal/claude/fake.go`, repurposing the existing `internal/claude` package, so all LLM-dependent unit tests (scoring, resume profile, documents) run 100% mocked in <10ms (**N1**, Article V)
- [x] T012 [P] Update `internal/web/layout.templ` for the matcher and register the chi routes from `contracts/http-routes.md` in `internal/web/server.go` with stub handlers
- [x] T013 Repurpose `internal/orchestrator/orchestrator.go` with a `RunSearch` entrypoint skeleton (discover→dedup→gate→score→persist→gen top-3), on-demand and scheduler-free (FR-020)

**Checkpoint**: Foundation ready — user stories can begin.

---

## Phase 3: User Story 1 - Discover & score into one view (Priority: P1) 🎯 MVP

**Goal**: The user uploads a resume, sets preferences, runs an on-demand search, and sees every qualifying (≥70) opening with score, explanation, and per-source health in a single view.

**Independent Test**: Upload a resume, set preferences, click Search → a consolidated ranked list appears; no sub-70 opening is shown; changing a preference and re-running changes the ranking; a failed source is reported, not hidden.

### Tests for User Story 1 (write first, must FAIL) ⚠️

- [x] T014 [P] [US1] Unit tests for the deterministic gate (salary + work-location gates fire, travel/relocate soft only, clamp 1–100) in `internal/scoring/gate_test.go`
- [x] T015 [P] [US1] Unit tests for resume extraction (PDF/DOCX/TXT → text; empty/garbage rejected) in `internal/resume/parser_test.go`
- [x] T016 [P] [US1] Integration test mapping an Adzuna JSON fixture to `RawOpening` in `internal/jobsource/aggregator_test.go`
- [x] T017 [P] [US1] Integration test `RunSearch` excludes <70 results and records `source_health` in `internal/orchestrator/orchestrator_test.go`
- [x] T018 [P] [US1] Route test `GET /search/{id}` never renders a sub-70 result and shows source health in `internal/web/server_test.go`

### Implementation for User Story 1

- [x] T019 [P] [US1] Implement PDF/DOCX/TXT text extraction in `internal/resume/parser.go` (ledongthuc/pdf; zip+xml for docx)
- [x] T020 [US1] Implement LLM structured-profile extraction (skills/roles/dates) in `internal/resume/profile.go` (depends on T019)
- [x] T021 [P] [US1] Implement `ApplyGates` deterministic gating in `internal/scoring/gate.go` (FR-005a)
- [x] T022 [US1] Implement LLM `ScoreFit` + score composition/clamp + explanation in `internal/scoring/scorer.go` (depends on T011, T021; FR-004, FR-015)
- [x] T023 [P] [US1] Implement the Adzuna aggregator adapter in `internal/jobsource/aggregator.go` (FR-003)
- [x] T024 [US1] Implement `RunSearch` (discover→dedup→gate→score→persist, source-health capture) in `internal/orchestrator/orchestrator.go` (depends on T008, T010, T022, T023)
- [x] T024a [US1] On re-run, reuse previously scored openings for the same `canonical_key` rather than re-scoring identical postings, while still surfacing newly discovered openings, in `internal/orchestrator/orchestrator.go` with a failing-first test in `internal/orchestrator/orchestrator_test.go` (**C1**/FR-025)
- [x] T025 [US1] Implement handlers `GET /` (dashboard home: active-resume status + latest results, **C3**), `POST /resume` (replace active resume while keeping prior searches tied to the old resume version, **U1**/FR-001a), `GET/POST /settings`, `POST /search`, `GET /search/{id}` in `internal/web/server.go` (depends on T024; FR-001, FR-002, FR-006, FR-009, FR-016, FR-018)
- [x] T026 [P] [US1] Implement the consolidated results view (scores, explanations, source health, empty "no qualifying matches" state) in `internal/web/results.templ`
- [x] T027 [P] [US1] Implement the preferences/settings view in `internal/web/settings.templ`
- [x] T028 [US1] Wire `cmd/linker/main.go`: config → store → sources → orchestrator → web (depends on T024, T025)

**Checkpoint**: MVP — a user can go from resume+preferences to a scored, de-duplicated, qualifying-only results view.

---

## Phase 4: User Story 2 - Tailored documents, no fabrication (Priority: P2)

**Goal**: Every qualifying opening has a tailored resume + cover letter — auto-generated for the top 3 scores, on-demand and cached for the rest — derived only from the resume's facts, with additions flagged.

**Independent Test**: Open results → top 3 already have documents; open a lower-ranked qualifying job → documents generate within seconds and cache; a job demanding a skill absent from the resume shows that skill flagged, not claimed as experience; user can edit and download.

### Tests for User Story 2 (write first, must FAIL) ⚠️

- [x] T029 [P] [US2] Unit tests for no-fabrication flagging (skill absent from resume → flagged; clean reword → no flags; refuses score<70) in `internal/documents/generator_test.go`
- [x] T030 [P] [US2] Integration test: eager top-3 generation during search + on-demand generation caches on second open, in `internal/web/server_test.go`

### Implementation for User Story 2

- [x] T031 [US2] Implement `Generate` + deterministic no-fabrication verification pass in `internal/documents/generator.go` (depends on T011; FR-007a, FR-008)
- [x] T032 [US2] Hook eager top-3 document generation into `RunSearch` in `internal/orchestrator/orchestrator.go` (depends on T024, T031; FR-007)
- [x] T033 [US2] Implement on-demand generation + caching in the `GET /job/{matchId}` handler in `internal/web/server.go` (depends on T031)
- [x] T034 [P] [US2] Implement the job-detail view (score, explanation, documents, fabrication flags, inline edit) in `internal/web/job.templ`
- [x] T035 [US2] Implement edit-save (`POST /job/{matchId}/documents/{docType}`) and download (`GET …/download?fmt=pdf|txt`) handlers in `internal/web/server.go` (FR-010)

**Checkpoint**: US1 + US2 both work independently.

---

## Phase 5: User Story 3 - Select & open for manual submission (Priority: P3)

**Goal**: The user selects openings to pursue and opens each live posting for manual submission; the system records the action and submits nothing.

**Independent Test**: Select a job, click proceed → the original posting opens in a new tab and the selection/open is recorded; verify no outbound application request is ever made.

### Tests for User Story 3 (write first, must FAIL) ⚠️

- [x] T036 [P] [US3] Route test: `POST /job/{matchId}/open` records `was_posting_opened`, returns the external URL, and makes zero submission calls, in `internal/web/server_test.go` (FR-013, SC-006)

### Implementation for User Story 3

- [x] T037 [US3] Implement `POST /job/{matchId}/select` and `POST /job/{matchId}/open` handlers in `internal/web/server.go` (depends on T008; FR-011, FR-012)
- [x] T038 [US3] Add select/open controls to `internal/web/job.templ` and `internal/web/results.templ`

**Checkpoint**: US1–US3 independently functional.

---

## Phase 6: User Story 4 - Paste a URL to assess (Priority: P4)

**Goal**: The user pastes one or more posting URLs; each is fetched, scored, and (if ≥70) given documents — same treatment as a discovered opening; bad URLs are isolated.

**Independent Test**: Paste a valid posting URL → it appears scored in results; paste a non-job URL → it is reported un-assessable without breaking the others.

### Tests for User Story 4 (write first, must FAIL) ⚠️

- [x] T039 [P] [US4] Unit tests for the urlpaste adapter (parses a posting; a non-job/unfetchable URL errors per-URL without failing the batch) in `internal/jobsource/urlpaste_test.go`

### Implementation for User Story 4

- [x] T040 [US4] Implement the urlpaste adapter in `internal/jobsource/urlpaste.go` (FR-021)
- [x] T041 [US4] Implement `POST /search/urls` handler + paste-URL input in `internal/web/server.go` and `internal/web/results.templ` (depends on T024, T040)

**Checkpoint**: US1–US4 independently functional.

---

## Phase 7: User Story 5 - Opt-in browser automation (Priority: P5)

**Goal**: A Playwright-based discovery source for non-API boards (incl. LinkedIn), disabled by default and only runnable after explicit risk acknowledgment.

**Independent Test**: Try to enable the browser source without acknowledging → rejected; acknowledge → it runs (or shows a clear browser-install message); default state performs no automated non-API access.

### Tests for User Story 5 (write first, must FAIL) ⚠️

- [x] T042 [P] [US5] Unit test: browser adapter returns `ErrAcknowledgmentRequired` when the ack flag is false, in `internal/jobsource/browser_test.go` (FR-023)
- [x] T043 [P] [US5] Route test: `POST /settings` rejects enabling the browser source without the ack checkbox, in `internal/web/server_test.go`

### Implementation for User Story 5

- [x] T044 [US5] Implement the gated Playwright browser adapter in `internal/jobsource/browser.go` (FR-022; default-off)
- [x] T045 [US5] Enforce the acknowledgment in `POST /settings` and add the risk-acknowledgment UI to `internal/web/settings.templ` (FR-023)
- [x] T046 [US5] Document the `playwright install` browser-binary setup step in `README.md`

**Checkpoint**: All five stories independently functional.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [ ] T047 [P] Update `CHANGELOG.md` describing the pivot to the job matcher (Article VI)
- [ ] T048 [P] Rewrite `README.md` to describe the resume-driven job matcher (replace the post-drafter description)
- [ ] T049 Update `docker-compose.yml` and `Dockerfile` for the new env keys (drop GitHub/Buffer, add Adzuna)
- [ ] T050 Run `make vet` and `make test`; ensure all unit (<10ms) and integration suites are green
- [ ] T051 Execute `quickstart.md` validation scenarios 1–9 and confirm each passes by observation (Article X)
- [ ] T051a Verify the plan's performance targets by observation (**C2**): a full search completes in <~60s, on-demand single-document generation returns in <~10s, and HTMX partial renders (excluding LLM calls) return in <~300ms; record the measured figures alongside the T051 validation

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: start immediately.
- **Foundational (Phase 2)**: after Setup — **blocks all stories**.
- **User Stories (Phases 3–7)**: after Foundational. US1 first (MVP). US2–US5 each depend only on Foundational + US1's orchestrator/web surface; otherwise independently testable.
- **Polish (Phase 8)**: after the desired stories are complete.

### Cross-story notes

- US2 hooks into `RunSearch` (T024) and the job-detail view; US3/US4 extend web handlers and views; US5 adds a gated source. None require another story's *business logic* to be tested independently.

### Within each story

- Tests written and failing before implementation (Article V).
- Models → services → handlers → views → wiring.

### Parallel opportunities

- Setup: T002–T005 in parallel.
- Foundational: T007, T009, T011, T011a, T012 in parallel (T006→T007/T008; T010 after T009). T011a (mockable LLM client) is a prerequisite for the <10ms unit tests in T014/T015/T029.
- US1 tests T014–T018 in parallel; then T019/T021/T023/T026/T027 in parallel, converging on T024→T025→T028.
- Different stories can be staffed in parallel once Foundational is done.

---

## Parallel Example: User Story 1

```bash
# Failing-first tests together:
Task: "Gate unit tests in internal/scoring/gate_test.go"
Task: "Resume parser unit tests in internal/resume/parser_test.go"
Task: "Adzuna mapping test in internal/jobsource/aggregator_test.go"
Task: "RunSearch integration test in internal/orchestrator/orchestrator_test.go"
Task: "Route test in internal/web/server_test.go"

# Then independent implementations together:
Task: "Resume extraction in internal/resume/parser.go"
Task: "Gate in internal/scoring/gate.go"
Task: "Adzuna adapter in internal/jobsource/aggregator.go"
Task: "Results view in internal/web/results.templ"
Task: "Settings view in internal/web/settings.templ"
```

---

## Implementation Strategy

### MVP first (US1 only)

1. Phase 1 Setup → 2. Phase 2 Foundational → 3. Phase 3 US1 → 4. **STOP & validate** US1 via quickstart scenarios 1–4 → 5. Demo.

### Incremental delivery

Foundation → US1 (MVP: discover+score) → US2 (documents) → US3 (select/open) → US4 (paste URL) → US5 (browser opt-in). Each adds value without breaking prior stories.
