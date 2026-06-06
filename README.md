# linker

Automated LinkedIn presence from your GitHub work.

`linker` watches your GitHub repositories, and when something post-worthy happens —
a new release, a notable commit, a refreshed README — it asks Claude to draft a
LinkedIn post in your voice: an **agile delivery lead / Scrum Master who ships real
software by directing AI agents**. You review and edit each draft in a simple
dashboard, then approve it to queue to LinkedIn via Buffer. A history log keeps your
cadence honest so you never double-post.

It's a single standalone service. No accounts to wire up beyond your own API keys, and
it runs end-to-end with `docker compose up`.

## How it works

```
GitHub repos ──poll──▶ new activity ──▶ Claude drafts a post ──▶ Drafts dashboard
                                                                    │ you edit / approve
                                                                    ▼
                                                  Buffer ──▶ your LinkedIn channel
                                                  (or a local stub if Buffer isn't set)
```

- **Poller** checks each repo on an interval and records only genuinely new activity
  (de-duplicated by commit SHA / release tag / README hash). On a repo's first sight it
  just establishes a baseline, so booting up doesn't post about old work.
- **Claude** (`claude-opus-4-8`) drafts each post using a persona prompt you can fully
  customize.
- **Dashboard** (templ + HTMX) lets you edit, regenerate, reject, or approve drafts.
- **Buffer** queues approved posts to your single LinkedIn channel. Without Buffer
  credentials it runs in **stub mode**: posts are "queued" locally and logged, so you
  can try the whole flow with zero Buffer setup.

## Quick start

```sh
cp .env.example .env
# Edit .env: set ANTHROPIC_API_KEY, GITHUB_TOKEN, and GITHUB_REPOS at minimum.
docker compose up --build
```

Then open <http://localhost:8080>.

Click **Poll GitHub now** to trigger a check immediately rather than waiting for the
interval. New activity becomes draft posts; edit one and hit **Approve & queue**.

### Minimum configuration

| Variable | What it's for |
|---|---|
| `ANTHROPIC_API_KEY` | Drafting posts with Claude |
| `GITHUB_TOKEN` | Reading your repos (raises rate limits; required for private repos) |
| `GITHUB_REPOS` | Comma-separated `owner/repo` list to watch |

Leave `BUFFER_ACCESS_TOKEN` blank to use stub mode. Set `BUFFER_ACCESS_TOKEN` and
`BUFFER_PROFILE_ID` (your LinkedIn channel's Buffer profile id) to publish for real.

See `.env.example` for all options (poll interval, cadence spacing, persona override,
listen address).

## Customizing your voice

The default LinkedIn persona lives in `internal/persona/persona.md`. To use your own,
point `PERSONA_PROMPT_PATH` at a markdown file. Keep the final-line `HASHTAGS:` output
contract so hashtags are parsed correctly.

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
cmd/linker         entrypoint
internal/config    env-only configuration
internal/store     Postgres persistence (events, cursors, posts)
internal/github    repo activity polling + diffing
internal/claude    Claude-backed post drafting
internal/buffer    Publisher interface + live Buffer client + stub
internal/persona   the LinkedIn voice (overridable)
internal/orchestrator  poll → dedup → draft → store
internal/web       templ + HTMX review dashboard
internal/app       wiring + background poller + HTTP server
```
