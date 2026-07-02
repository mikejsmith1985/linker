# Quickstart & Validation: Resume-Driven Job Matcher

Runnable end-to-end validation that proves the feature works (Article X). Assumes the repurposed service builds and a Postgres is reachable. Implementation lives in `tasks.md` — this file is the run/verify guide.

## Prerequisites

- Go 1.25+, `make`, Docker (for Postgres via `docker compose`).
- `.env` populated (see `.env.example`, updated for this feature):
  - `DATABASE_URL` — Postgres connection
  - `ANTHROPIC_API_KEY`, `CLAUDE_MODEL=claude-opus-4-8`
  - `ADZUNA_APP_ID`, `ADZUNA_APP_KEY` — default job aggregator (injected via env/Forge Vault, never committed)
  - `HTTP_ADDR=:8080`
- Browser-automation validation (optional): Playwright browsers installed via the documented `playwright install` step. Skip unless testing the opt-in source.

## Setup

```sh
cp .env.example .env      # then fill secrets via Forge Vault injection
make up                   # Postgres + app (docker compose), or:
make run                  # local run against a reachable DATABASE_URL
```

## Validation scenarios

Each maps to acceptance scenarios / success criteria in `spec.md`.

### 1. Upload a resume (FR-001, FR-018)
- Open `http://localhost:8080`, upload a PDF/DOCX/TXT resume.
- **Expect**: resume accepted, skills/experience extracted and shown. An empty or garbage file is rejected with an actionable message (no search runs).

### 2. Set preferences (FR-002)
- In Settings, set required salary, work-location (onsite/hybrid/remote), travel and relocate willingness. Save.
- **Expect**: preferences persist across a restart (FR-024).

### 3. Run an on-demand search (FR-003, FR-004, FR-006)
- Click Search.
- **Expect**: a single consolidated view of openings, each with a 1–100 score and an evidence-citing explanation. **No** opening below 70 appears (SC-002). Per-source health (succeeded/failed/no_results) is shown (SC-008).

### 4. Hybrid gating changes ranking (FR-005a, SC-005)
- Note the ranking. Switch work-location from `remote` to `onsite`, or raise required salary. Re-run.
- **Expect**: a visibly different ranking; onsite-only roles now rank higher (or high-salary-miss roles drop below 70).

### 5. Top-3 documents are ready; others on demand (FR-007, SC-003)
- Open the results.
- **Expect**: the three highest-scoring openings already have a tailored resume + cover letter. Open a lower-ranked qualifying opening → its documents generate within a few seconds and are cached for the next open.

### 6. No fabrication (FR-007a)
- Review a tailored resume for a job demanding a skill absent from your resume.
- **Expect**: the document does not claim that skill as your experience; any introduced skill/entity is flagged for review, not silently embedded.

### 7. Paste a URL (FR-021)
- Paste a single job posting URL and submit.
- **Expect**: it is fetched, scored, and (if ≥70) given documents — same treatment as a discovered opening. A non-job URL is reported as un-assessable without breaking others.

### 8. Select and open — manual submission only (FR-011, FR-012, FR-013, SC-006)
- Select an opening and click to proceed.
- **Expect**: the original posting opens in a new tab for you to submit manually. The system records the open but sends **no** application anywhere.

### 9. Opt-in browser automation is gated (FR-022, FR-023)
- In Settings, try to enable the browser source without checking the risk acknowledgment.
- **Expect**: rejected. After acknowledging, the source runs; with browsers not installed, a clear setup message appears.

## Automated verification

```sh
make test        # unit (mocked, <10ms) + integration; TDD suites for scoring gates,
                 # de-dup, no-fabrication flagging, and route behavior must pass
make vet
```

**Definition of done for validation**: scenarios 1–9 pass by observation, and `make test` is green including the contract tests listed in `contracts/*.md`.
