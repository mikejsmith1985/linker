# Contract: `internal/web` HTTP routes

chi routes serving templ/HTMX views for the single-user dashboard. No auth (single-user self-hosted, FR-019). Partial responses return HTML fragments for HTMX swaps.

| Method & Path | Purpose | Spec refs |
|---------------|---------|-----------|
| `GET /` | Dashboard: active resume status, preferences summary, latest search results | FR-009 |
| `POST /resume` | Upload/replace the active resume (multipart); validate & parse; reject empty/unreadable | FR-001, FR-001a, FR-018 |
| `GET /settings` | Preferences + source toggles + browser-automation acknowledgment form | FR-002, FR-022 |
| `POST /settings` | Save preferences; setting browser source requires the ack checkbox | FR-002, FR-023 |
| `POST /search` | Start an on-demand search; returns results view (or progress) | FR-003, FR-017 |
| `POST /search/urls` | Submit one or more pasted posting URLs to score | FR-021 |
| `GET /search/{id}` | Consolidated scored results (qualifying only), with per-source health | FR-006, FR-009, FR-016 |
| `GET /job/{matchId}` | One opening: score, explanation, documents (generating on first open if needed) | FR-007, FR-010, FR-015 |
| `POST /job/{matchId}/documents/{docType}` | Save user edits to a generated document | FR-010 |
| `GET /job/{matchId}/documents/{docType}/download?fmt=pdf\|txt` | Download tailored resume / cover letter | FR-010 |
| `POST /job/{matchId}/select` | Mark opening as one to pursue | FR-011 |
| `POST /job/{matchId}/open` | Record the posting was opened; return the original URL to open for manual submission | FR-012, FR-013 |

## Behavioral contracts

- No route ever submits an application to an employer or job board (FR-013, SC-006). `POST /job/{matchId}/open` only records + returns the external URL.
- `GET /search/{id}` MUST exclude every result with `score < 70` (FR-006, SC-002) and MUST render each source's health (succeeded/failed/no_results), never silently dropping a failed source (FR-016, SC-008).
- `GET /job/{matchId}` for a rank>3 opening with no cached document triggers generation, then caches (FR-007); a rank 1–3 opening already has documents ready (SC-003).
- `POST /settings` MUST reject enabling the browser source unless the acknowledgment checkbox is set (FR-023).
- A search with zero qualifying results renders an explicit "no qualifying matches" state with a prompt to broaden preferences, not a blank page (edge case).

## Route tests

- `POST /resume` with an empty/garbage file returns a 4xx with an actionable message; no search runs (FR-018).
- `GET /search/{id}` never includes a sub-70 result across fixtures (SC-002).
- `POST /settings` enabling browser without ack is rejected (FR-023).
- `POST /job/{matchId}/open` records `was_posting_opened` and returns the URL without any outbound application call (SC-006).
