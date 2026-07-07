# Changelog — linker

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- **PDF downloads now render Markdown as real formatting** instead of dumping raw
  Markdown. Headings, **bold**, and bullet lists become proper typography, a
  Windows-1252 translator fixes the `Â·` / `â€"` mojibake on middots and em
  dashes, and symbols outside that encoding (arrows, ellipsis) fold to ASCII
  rather than vanishing. Previously the PDF showed literal `#`, `**`, and garbled
  punctuation.
- Download filenames no longer leak a resume file's dedup suffix (e.g. the `(1)`
  in `Michael_Smith_Resume (1).docx`), which previously surfaced as a stray
  `_1` in the saved document name.

### Changed
- **Downloaded documents now have self-describing filenames** of the form
  `Name_Job-Title_Resume.pdf` / `Name_Job-Title_Cover-Letter.txt` (e.g.
  `Michael_Smith_Staff_Software_Engineer_Resume.pdf`) instead of
  `tailored_resume-9.txt`, so a saved file is immediately identifiable without
  renaming. The candidate name is inferred from the active resume (its leading
  name line, falling back to the uploaded file's base name).

### Added
- Passed roles are now **hidden from the Matches page by default**, with a
  "Show N passed" toggle to bring them back — the list stays focused on live
  prospects.
- The **Matches page now accumulates results across all searches** (de-duplicated
  to each opening's most recent match, ranked by score) instead of showing only
  the latest search. A later search that finds nothing — or a narrow company/URL
  search — no longer hides matches an earlier search found.
- **Recent searches** activity list on the home page: every search (discovery,
  URL, or company) now shows up with its status, run time, and qualifying-match
  count, auto-refreshing from "running" to "completed in Ns · N matches". This
  restores the feedback that async searches removed — you can see a search
  executed, is still running, or finished with zero matches.
- The assistant now lives in a **persistent side ribbon** visible on every page,
  so you can chat with it while viewing your matches. The layout is wider
  (two-column, uses the full screen) and collapses to a single column on narrow
  windows.
- Tailored resumes and cover letters now show a **rendered Markdown preview**;
  the raw editor is tucked behind an "Edit" toggle.
- **In-app assistant** (`/assistant`): a conversational helper that reads your
  current preferences and latest matches and, in plain English, answers questions
  ("what's my top match?") and takes actions — updating preferences (work location,
  strict, salary, location, new-roles-only, target roles) and starting searches
  ("only remote AI roles, then search"). Conversation is persisted.
- The employer's own website is captured from JSearch and shown as a direct
  "🏢 Company website" link on the job's How-to-apply section when available.
- **Asynchronous searches**: a search now runs in the background and returns
  immediately instead of blocking the browser for minutes. The search forms
  submit without navigating away (so typed input — e.g. a company list — is no
  longer lost), the results page shows a "🔄 running" state that auto-refreshes to
  completion, and a search survives closing the tab. Searches interrupted by a
  restart are marked failed at startup instead of hanging.
- **New roles only** preference: skip any posting already seen in a previous
  search, so each search surfaces only roles you haven't encountered yet
  (a "what's new since last time" mode). Relies on the persisted opening history.
- **How to apply** guidance on each job: a "Find on the company's careers site"
  link (and a LinkedIn search) plus advice that aggregator "Quick Apply" is often
  filtered by ATS — apply on a direct employer's own careers page when possible.
- **Target roles** preference: type job titles to search for (one per line) —
  they're searched first, alongside the roles inferred from your resume. Ideal for
  pivoting toward roles you want (e.g. AI-first / agentic titles). JSearch now
  fetches 5 pages per query and up to 5 role-title queries run per search, and the
  resume parser suggests adjacent AI-first roles when the resume shows AI /
  automation experience.
- **Strict work-location** preference (default on): when set, roles that conflict
  with your work-location choice are **excluded outright** — a remote-only search
  now hides every hybrid/onsite role instead of merely ranking it low. Toggle in
  Preferences.
- **Pass reasons**: the Pass control is now a dropdown (Hybrid/onsite, Location,
  No longer accepting, Compensation, Seniority, Not a fit, Other) — the reason is
  stored with the pass and shown on the card, building richer review feedback.
- JSearch biases toward postings from the last month to reduce expired/closed
  listings.
- **Matches page** (`/matches`): always shows your latest completed search's
  results, so closing the tab never loses your list — no saved URL needed.
- **Review workflow**: each match has **Interested** / **Pass** / **Undo** buttons.
  The mark is stored on the job itself (not the per-search row), so it persists
  across re-runs; passed roles are dimmed and interested roles highlighted. This
  is the feedback signal for improving matching over time.
- **Direct apply links**: JSearch now prefers a direct-to-employer application
  link over an aggregator redirect (Monster/LinkedIn) when one is available.
- Larger, wider dashboard UI (bigger fonts, full-width layout) to use screen space.

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
- Broadened discovery to search by **target job titles**, not just skill keywords.
  Resume parsing now extracts likely role titles (e.g. "Scrum Master, Release
  Train Engineer, Agile Coach"), and each is run as its own query across the
  search-based sources; client-filtered sources match on skills + titles. JSearch
  now fetches 2 result pages per query. This surfaces far more relevant roles for
  non-developer profiles.
- Added an optional **JSearch** source (RapidAPI) that indexes Google for Jobs —
  surfacing LinkedIn, Indeed, Glassdoor, and ZipRecruiter listings — the broadest
  coverage available. Enabled by setting `RAPIDAPI_KEY`; hourly/monthly pay is not
  mistaken for an annual salary (`internal/jobsource` JSearch adapter).
- Broadened job coverage with three more key-free sources — **RemoteOK**,
  **Arbeitnow**, and **Jobicy** — alongside Remotive, all always-on and needing
  no credentials. Their results are keyword-filtered and capped so a search stays
  relevant and bounded.
- Added **company targeting**: name specific employers (e.g. Stripe, Databricks)
  and pull openings straight from their public applicant-tracking feed
  (Greenhouse, with a Lever fallback), scored against your resume
  (`internal/jobsource` company source; `POST /search/companies`). Employers on
  other systems (e.g. Workday) are not covered.
- Added a **geographic eligibility gate**: a new "location/region" preference
  (default "United States") filters out remote roles the user isn't eligible for.
  A posting restricted to e.g. Brazil, Germany, or Europe-only is gated below the
  threshold, while "USA", "Americas", "North America", or "Worldwide" roles are
  kept. Innocent-until-proven-guilty, so plain city names never over-gate
  (`internal/scoring` location gate).
- Tightened job discovery to a focused keyword set so results stay relevant
  instead of returning a broad grab-bag.
- Added **Remotive** as the default, key-free job source (remote roles) and made
  Adzuna optional — the app now discovers jobs out of the box with only an
  `ANTHROPIC_API_KEY` (`internal/jobsource` Remotive adapter).
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
  - Tailored resume + cover letter generation per qualifying opening under a
    no-fabrication guarantee: a verification pass flags any skill/term the draft
    claims that the job wants but the resume never mentions, rather than shipping
    it silently (`internal/documents`). Documents are generated eagerly for the
    top 3 scores and on first open (then cached) for the rest, and are reviewable,
    editable, and downloadable as PDF/TXT/Markdown from the job detail view.
  - Select an opening to pursue and open the original posting for manual
    submission: the app records the selection and that the posting was opened,
    then redirects to the live listing. It never submits an application on the
    user's behalf; an unreachable posting reports "no longer reachable"
    (`internal/web`; FR-011/012/013).
  - Paste-a-URL scoring: paste one or more job posting URLs and each is fetched,
    parsed, scored, and (if it reaches 70) given tailored documents like any
    discovered match. A URL that cannot be fetched or is not a job posting fails
    on its own without breaking the batch (`internal/jobsource` URL source;
    `POST /search/urls`; FR-021).
  - Opt-in Playwright browser source for boards without a permitted API
    (including LinkedIn). It is off by default and refuses to run until the user
    both enables it (`ENABLE_BROWSER_SOURCE`) and records an explicit
    terms-of-service / account-ban risk acknowledgment in preferences; the
    settings form rejects enabling it without the acknowledgment (`internal/jobsource`
    browser adapter; FR-022/023). Requires the Playwright browser binaries.
  - Rewrote the README for the job matcher and documented the browser-automation
    opt-in and `playwright install` step.

### Fixed
- A plain "remote" mention (e.g. a "Remote … Coach" title that is actually
  hybrid) no longer classifies a JSearch role as remote. Remote now requires an
  explicit fully-remote phrase, or JSearch's remote flag together with a
  non-specific location — a remote-titled role tied to a specific city is treated
  as hybrid and excluded under strict remote.
- Company-feed (Greenhouse/Lever) work-location is now inferred from the job
  description text as well as the title/location, so strict work-location gating
  is consistent across the company-targeting search and the other sources.
- A JSearch role flagged not-remote (`job_is_remote=false`) with no recognized
  remote/onsite keyword in its text is now treated as **onsite** rather than
  "unknown", so a remote-only preference correctly filters it out (an "In-person"
  role "in our Winchester, VA office" no longer slips through). Added in-person /
  must-relocate detectors.
- Work-location is now inferred from the posting text (title + description), not
  just a source's remote flag. JSearch marks a role remote if it has any remote
  days, so hybrid/onsite roles (e.g. "Hybrid 3 days on site") slipped past a
  remote-only preference; the text now wins, so the work-location gate correctly
  drops them.
- Job text is truncated on UTF-8 rune boundaries and sanitized before storage, so
  a multi-byte character (em-dash, bullet) split by truncation no longer produces
  an "invalid byte sequence for encoding UTF8" error that failed the whole search.
- A single unpersistable or unscorable opening is now skipped (logged) instead of
  aborting the entire search — one bad listing can't zero out the results.
- JSearch now calls the versioned `/search-v2` endpoint; the old `/search` path
  returns 404 ("endpoint does not exist") and left the source non-functional.
- Parse the `/search-v2` response envelope, whose `data` is an object wrapping a
  `jobs` array (the old `/search` returned `data` as a bare array). Without this
  the source decoded to an error and reported "failed" on every search. Also map
  `job_location` (e.g. "Anywhere") when city/state/country are absent.
- Pass `RAPIDAPI_KEY` through `docker-compose.yml` to the app container so the
  JSearch source activates under docker compose.
- Pre-push hook (`.forge/hooks/pre-push` and `.forge/hooks/pre-push.ps1`) no
  longer false-fails on every push. The scaffold template hardcoded
  `go build ./cmd/forge/` (Forge Terminal's own entrypoint), which does not
  exist here. It now runs `go tool templ generate` first — so the gitignored
  `*_templ.go` files exist — then builds every package with `go build ./...`,
  making the check entrypoint-agnostic.

### Removed
