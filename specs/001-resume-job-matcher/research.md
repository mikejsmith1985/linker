# Phase 0 Research: Resume-Driven Job Matcher

All Technical Context unknowns are resolved below. Each entry records the decision, rationale, and rejected alternatives.

## 1. Primary job-discovery source (compliant API)

**Decision**: Use a pluggable `jobsource.Source` interface with **Adzuna** as the default aggregator adapter. Additional adapters (Jooble, USAJobs, Careerjet, JSearch/RapidAPI) can be registered later without touching the scoring core.

**Rationale**: Adzuna offers a documented, ToS-permitted JSON REST API with a free tier, salary data, and location/keyword filters — a clean fit for FR-003 and for the salary/work-location gating. An adapter interface keeps FR-003 satisfied while leaving room for more sources per the single-user's coverage needs.

**Alternatives considered**:
- *JSearch (RapidAPI)* aggregates LinkedIn/Indeed/Glassdoor results — broadest coverage but paid and its data provenance is less transparent; kept as an optional adapter, not the default.
- *Direct LinkedIn/Indeed scraping* — rejected as the primary path; LinkedIn's ToS prohibits it. This is exactly what the opt-in browser mode (item 3) isolates behind a risk acknowledgment.
- *USAJobs* — excellent for US federal roles only; too narrow to be the default.

## 2. Resume ingestion & parsing (PDF / DOCX / TXT)

**Decision**: Two-stage. Stage 1 deterministically extracts plain text: `github.com/ledongthuc/pdf` for PDF, stdlib `archive/zip` + `encoding/xml` over `word/document.xml` for DOCX, direct read for TXT. Stage 2 hands the extracted text to Claude to produce a structured profile (skills, roles, dates, credentials) used for matching.

**Rationale**: Deterministic text extraction keeps parsing testable and cheap, and — critically — becomes the **source-of-truth fact set** that the no-fabrication rule (FR-007a) checks generated documents against. LLM structuring only organizes text already present; it never invents. DOCX-as-zip needs no third-party dependency.

**Alternatives considered**:
- *Send raw file bytes to an LLM for parsing* — rejected: less testable, higher token cost, and blurs the fact-source needed to enforce no-fabrication.
- *`unioffice`/`unidoc`* — heavier (some commercial licensing); the zip+xml approach covers the needed text extraction.
- *Shell out to `pdftotext` (poppler)* — adds a system binary dependency to the container; the pure-Go lib avoids it.

## 3. Opt-in browser-automation discovery (Playwright)

**Decision**: `github.com/playwright-community/playwright-go`, wired into a `jobsource.browser` adapter that is **disabled by default** and only runs after the user records an explicit risk acknowledgment (persisted flag). Browser binaries are installed as a separate, documented step — not part of the default image.

**Rationale**: Honors the user's explicit Playwright request (Q1). Playwright's stealth/robustness beats hand-rolled HTTP scraping for JS-heavy sources like LinkedIn. Making binary install a separate step reinforces the default-off gating (FR-023) — the capability literally cannot run until deliberately enabled.

**Alternatives considered**:
- *`chromedp` (pure-Go CDP)* — no Node dependency, but the user specifically asked for Playwright; Playwright also handles auth/anti-bot flows more robustly.
- *Building automation into the default image* — rejected: contradicts default-off gating and bloats the base container.

## 4. Scoring model: 1–100 with hybrid gating

**Decision**: Compute the score in two layers.
1. **Deterministic gate** (`scoring.gate`): compare stated required salary and work-location preference against the opening. A salary miss or work-location conflict applies a large fixed penalty (a named constant) engineered so gated openings normally fall below 70. Travel/relocate apply smaller soft adjustments.
2. **LLM skill-fit** (`scoring.scorer`): Claude rates resume-to-opening fit on a bounded sub-scale and returns a short, evidence-citing explanation (FR-015). Final score = blended base fit minus gate penalties, clamped to 1–100.

**Rationale**: Matches the clarified hybrid semantics (Q1: salary + work-location gate; travel/relocate soft). Keeping gating deterministic makes FR-005a unit-testable in <10ms without an LLM, and keeps the 70 threshold meaningful. The LLM handles nuanced skill fit that keyword matching cannot (addresses the "keyword overlap ≠ fit" edge case).

**Alternatives considered**:
- *Pure-LLM single score* — rejected: non-deterministic gating makes salary/work-location behavior untestable and the threshold unstable.
- *Pure keyword/TF-IDF scoring* — rejected: floods results with weak keyword matches (the spec's stated failure mode).

## 5. De-duplication across sources

**Decision**: Canonical opening key = normalized(employer) + normalized(title) + normalized(location). On collision, keep the record with the richest data (prefer one with a stated salary and a live URL) and record the extra source names on it. Persisted so re-runs (FR-025) reuse prior scoring.

**Rationale**: Satisfies FR-014 and SC-007 deterministically and testably without needing an LLM. Normalizing employer/title/location catches the common cross-board duplicate.

**Alternatives considered**:
- *URL-only dedup* — rejected: the same role has different URLs per board.
- *LLM semantic dedup* — overkill and non-deterministic for the common case; can be a later enhancement for near-duplicates.

## 6. No-fabrication enforcement (FR-007a)

**Decision**: Generation is constrained by (a) a system prompt that forbids introducing any employer, date, title, credential, or skill absent from the extracted resume fact set, and (b) a post-generation verification pass that checks the tailored resume's named entities/skills against the Stage-1 extracted fact set, flagging any additions for the user rather than silently shipping them.

**Rationale**: Prompt-only guardrails are not proof (Article X). The deterministic fact set from item 2 gives a checkable baseline, making the no-fabrication guarantee verifiable, not merely requested.

**Alternatives considered**:
- *Prompt instruction only* — rejected as unverifiable.
- *Fully templated resume (no LLM)* — rejected: loses the per-job tailoring that is the feature's value.

## 7. Search cadence: on-demand now, scheduled-ready (FR-020)

**Decision**: Expose search as an orchestrator entrypoint invoked by an HTTP action (on-demand). The orchestrator takes the resume, preferences, and enabled sources as inputs and returns/persists a search result — with no dependency on a scheduler. A future scheduled trigger simply calls the same entrypoint on an interval, reusing the existing config's interval pattern (the repo already has a poll-interval concept).

**Rationale**: Satisfies FR-020's "on-demand now, structured for scheduled later" without building the scheduler now. The existing poller pattern is the proven insertion point.

**Alternatives considered**:
- *Build the scheduler now* — rejected: out of initial scope; adds moving parts before the core is validated.

## 8. Document format for tailored resume & cover letter

**Decision**: Generate and store as Markdown; render in-browser for review/edit; offer plain-text and PDF download (PDF via a lightweight HTML→PDF step). Editing happens on the Markdown in the dashboard before download.

**Rationale**: Markdown is easy to edit inline (FR-010), diff, and store; PDF is what employers expect at submission time. Keeps the editable source and the deliverable distinct.

**Alternatives considered**:
- *DOCX output* — heavier to generate faithfully; deferred as a possible later export format.

---

**All NEEDS CLARIFICATION resolved.** Ready for Phase 1 design.
