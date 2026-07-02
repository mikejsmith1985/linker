# Phase 1 Data Model: Resume-Driven Job Matcher

Persisted in PostgreSQL via the existing `internal/store` (pgx). All results survive restarts (FR-024). Single-user, so no per-user tenancy column. Timestamps are `timestamptz`. New tables extend `internal/store/schema.sql`.

## Entity overview

```
Resume (single active) ──1:N──▶ Search ──1:N──▶ MatchResult ──1:1──▶ JobOpening
                                                     │
                                                     ├──0:2──▶ GeneratedDocument (tailored resume + cover letter)
                                                     └──0:1──▶ Selection
SearchPreferences (single active) ─── used by ──▶ Search
```

## Resume

The user's single active resume and its extracted fact set (matching baseline + no-fabrication source of truth).

| Field | Type | Rules |
|-------|------|-------|
| `id` | uuid PK | |
| `original_filename` | text | |
| `format` | text | one of `pdf`,`docx`,`txt` (FR-018) |
| `raw_text` | text | deterministically extracted text; the fact set the no-fabrication check runs against (FR-007a) |
| `structured_profile` | jsonb | LLM-structured skills/roles/dates/credentials |
| `is_active` | boolean | exactly one row is active; upload replaces (FR-001a) |
| `created_at` | timestamptz | |

**Rules**: Uploading a new resume inserts a new row and clears `is_active` on the prior one; prior Searches keep pointing at the resume row that produced them (FR-001a). Empty/unreadable input is rejected before insert (FR-018).

## SearchPreferences

The scoring inputs. One active row; editable.

| Field | Type | Rules |
|-------|------|-------|
| `id` | uuid PK | |
| `required_salary_min` | integer (nullable) | minimum acceptable salary; null = unset |
| `salary_currency` | text | default `USD` |
| `work_location_pref` | text | one of `onsite`,`hybrid`,`remote` |
| `willing_to_travel` | boolean | soft factor |
| `willing_to_relocate` | boolean | soft factor |
| `browser_automation_ack` | boolean | true only after explicit risk acknowledgment (FR-022/023); gates the browser source |
| `enabled_sources` | text[] | which `jobsource` adapters are active (browser excluded unless `browser_automation_ack`) |
| `updated_at` | timestamptz | |

## Search

One on-demand discovery+scoring run (FR-017, FR-024). Reruns create new Searches; prior results remain (FR-024/025).

| Field | Type | Rules |
|-------|------|-------|
| `id` | uuid PK | |
| `resume_id` | uuid FK→Resume | |
| `preferences_snapshot` | jsonb | preferences as they were at run time (SC-005 reproducibility) |
| `status` | text | `running`,`completed`,`failed` |
| `source_health` | jsonb | per-source `succeeded`/`failed`/`no_results` (FR-016, SC-008) |
| `started_at` / `finished_at` | timestamptz | |

## JobOpening

A discovered posting (de-duplicated, FR-014).

| Field | Type | Rules |
|-------|------|-------|
| `id` | uuid PK | |
| `canonical_key` | text | normalized employer+title+location; unique per active dataset for de-dup (FR-014, SC-007) |
| `title` | text | |
| `employer` | text | |
| `location` | text | |
| `work_location_type` | text | `onsite`,`hybrid`,`remote`,`unknown` |
| `salary_min` / `salary_max` | integer (nullable) | null when posting omits salary (edge case: uncertainty disclosed) |
| `description` | text | |
| `source_names` | text[] | all boards this opening was seen on (dedup keeps extras here) |
| `original_url` | text | opened for manual submission (FR-012) |
| `discovered_at` | timestamptz | |

## MatchResult

Pairing of one JobOpening with the resume+preferences of a Search — the score and its explanation.

| Field | Type | Rules |
|-------|------|-------|
| `id` | uuid PK | |
| `search_id` | uuid FK→Search | |
| `job_opening_id` | uuid FK→JobOpening | |
| `score` | integer | 1–100 (FR-004) |
| `score_explanation` | text | human-readable evidence for the score (FR-015) |
| `gate_penalties` | jsonb | which gates fired (salary/work-location) and magnitude (FR-005a) |
| `is_qualifying` | boolean | true iff `score >= 70`; only qualifying results are shown (FR-006, SC-002) |
| `rank` | integer | position within the search by descending score (drives top-3 eager gen) |

**Rules**: Results with `score < 70` are persisted for audit but never presented (FR-006). The three qualifying results with `rank` 1–3 trigger eager document generation (FR-007).

## GeneratedDocument

A tailored resume or cover letter for one MatchResult (FR-007, FR-007a, FR-010).

| Field | Type | Rules |
|-------|------|-------|
| `id` | uuid PK | |
| `match_result_id` | uuid FK→MatchResult | |
| `doc_type` | text | `tailored_resume` or `cover_letter` |
| `content_markdown` | text | editable source (FR-010) |
| `fabrication_flags` | jsonb | entities/skills the verification pass found NOT in the resume fact set (FR-007a); empty = clean |
| `was_edited_by_user` | boolean | |
| `generated_at` | timestamptz | |

**Rules**: Only created for qualifying results (never for score<70, FR-008). Eager for `rank` 1–3; created on first open otherwise, then cached (FR-007). Content is derived solely from the Resume fact set; additions are flagged, not silently included (FR-007a).

## Selection

The user's decision to pursue an opening and the record its posting was opened (FR-011, FR-012, SC-006).

| Field | Type | Rules |
|-------|------|-------|
| `id` | uuid PK | |
| `match_result_id` | uuid FK→MatchResult | unique — one selection per result |
| `was_posting_opened` | boolean | set when the original URL is opened for manual submission |
| `selected_at` | timestamptz | |

**Rules**: The system records selection and that the posting was opened; it performs **zero** automatic submission (FR-013, SC-006).

## State transitions

- **Resume**: `uploaded → active` (prior active → inactive on new upload).
- **Search**: `running → completed` (all enabled sources returned or failed) or `running → failed` (fatal error). Partial source failure keeps `completed` with `source_health` recording the failed source (SC-008).
- **GeneratedDocument**: `absent → generated → (optionally) edited`. Ranks 1–3 auto-transition to `generated` during the Search; others transition on first open.
- **Selection**: `absent → selected → posting_opened`.
