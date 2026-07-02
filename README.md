# linker

Find jobs that fit your resume — scored, tailored, and ready to apply to yourself.

`linker` takes your resume and your preferences (required salary, work location,
willingness to travel/relocate) and searches job sources for openings that match your
skills. Each opening is scored **1–100**. Anything below **70** is dropped. For the
matches that qualify, `linker` asks Claude to write a **tailored resume and a cover
letter** for that specific posting — using only facts already in your resume — and
flags anything that looks invented. You review the score and the documents in one
dashboard, pick the jobs worth pursuing, and open each posting to submit your
application **yourself**. `linker` never submits anything on your behalf.

It's a single standalone self-hosted service. It runs end-to-end with
`docker compose up`.

## How it works

```
resume + preferences
      │
      ▼
 job sources ──▶ discover ──▶ de-duplicate ──▶ score 1–100 (skill fit + preference gate)
 (Adzuna API,                                        │
  pasted URLs,                          drop < 70 ───┤
  opt-in browser)                                    ▼
                                    qualifying matches ──▶ tailored resume + cover letter
                                                                    │ review / edit / download
                                                                    ▼
                                              open the original posting → you apply manually
```

- **Scoring** blends an LLM skill-fit judgment with a deterministic gate: required
  salary and work location are strong gates (a mismatch normally drops the job below
  70), while travel/relocation are softer factors.
- **Documents** are generated for the top 3 scores up front and on first open for the
  rest (then cached). A verification pass flags any skill or term the draft claims that
  your resume never mentions — so you never unknowingly submit a fabricated claim.
- **Sources** are pluggable: the Adzuna aggregator API by default, one or more
  posting URLs you paste, and an opt-in Playwright browser source for boards without a
  permitted API.

## Quick start

```sh
cp .env.example .env
# Edit .env: set ANTHROPIC_API_KEY, and ADZUNA_APP_ID / ADZUNA_APP_KEY for discovery.
docker compose up --build
```

Then open <http://localhost:8080>: upload your resume, set your preferences, and click
**Run search**. You can also paste specific posting URLs to score them directly.

### Minimum configuration

| Variable | What it's for |
|---|---|
| `DATABASE_URL` | Postgres connection (set for you under docker compose) |
| `ANTHROPIC_API_KEY` | Scoring and document generation with Claude |
| `ADZUNA_APP_ID` / `ADZUNA_APP_KEY` | The default Adzuna job source (free at developer.adzuna.com) |

Without Adzuna credentials the aggregator source is skipped and reported as
unavailable — you can still paste posting URLs to score them. See `.env.example` for
all options.

## Opt-in browser automation (advanced)

`linker` can drive a headless browser (via Playwright) to search boards that do not
offer a permitted API, **including LinkedIn**.

> ⚠️ Automating LinkedIn and similar sites may violate their terms of service and can
> get your account restricted or banned. This is entirely your choice. The source is
> **off by default** and refuses to run until you explicitly acknowledge the risk.

To use it you must opt in on **both** layers, and install the browser binaries:

1. Set `ENABLE_BROWSER_SOURCE=1` in `.env` (makes the source available).
2. Install the Playwright browser binaries:
   ```sh
   go run github.com/playwright-community/playwright-go/cmd/playwright install --with-deps chromium
   ```
3. In the dashboard **Preferences**, tick the acknowledgment checkbox and enable the
   browser source. Until you do, the source returns an "acknowledgment required" error
   and runs nothing.

The LinkedIn selectors in `internal/jobsource/browser_playwright.go` are a best-effort
starting point and may need tuning; LinkedIn changes its markup often and may require
an authenticated session.

## Local development

Requires Go 1.25+. Templates are written in [templ](https://templ.guide); generated
`*_templ.go` files are not committed, so run `make generate` before building locally.

```sh
make generate   # regenerate Go from .templ files
make test       # go test ./...
make vet        # go vet ./...
make run        # run locally (needs a reachable DATABASE_URL + .env)
make up         # docker compose up --build (Postgres + app)
```

## Layout

```
cmd/linker             entrypoint
internal/config        env-only configuration
internal/store         Postgres persistence (resumes, preferences, searches,
                       openings, match results, documents, selections)
internal/resume        PDF/DOCX/TXT extraction + LLM profile structuring
internal/jobsource     source interface, de-dup registry, Adzuna / pasted-URL /
                       opt-in browser adapters
internal/scoring       deterministic preference gate + LLM skill-fit → 1–100
internal/documents     no-fabrication tailored resume + cover letter generation
internal/claude        Claude-backed LLM interface (+ fake for tests)
internal/orchestrator  discover → dedup → gate → score → persist → rank
internal/web           templ + HTMX dashboard
internal/app           wiring + HTTP server
```
