-- linker (resume-driven job matcher) schema. Idempotent: safe to run on startup.

CREATE TABLE IF NOT EXISTS resumes (
    id                 BIGSERIAL PRIMARY KEY,
    original_filename  TEXT        NOT NULL DEFAULT '',
    format             TEXT        NOT NULL DEFAULT 'txt',
    raw_text           TEXT        NOT NULL DEFAULT '',
    structured_profile TEXT        NOT NULL DEFAULT '',
    is_active          BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_resumes_active ON resumes(is_active);

CREATE TABLE IF NOT EXISTS preferences (
    id                     BIGSERIAL PRIMARY KEY,
    required_salary_min    INTEGER     NOT NULL DEFAULT 0,
    salary_currency        TEXT        NOT NULL DEFAULT 'USD',
    work_location_pref     TEXT        NOT NULL DEFAULT 'remote',
    location               TEXT        NOT NULL DEFAULT 'United States',
    willing_to_travel      BOOLEAN     NOT NULL DEFAULT FALSE,
    willing_to_relocate    BOOLEAN     NOT NULL DEFAULT FALSE,
    browser_automation_ack BOOLEAN     NOT NULL DEFAULT FALSE,
    enabled_sources        JSONB       NOT NULL DEFAULT '[]',
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Add the location column to preferences tables created before it existed.
ALTER TABLE preferences ADD COLUMN IF NOT EXISTS location TEXT NOT NULL DEFAULT 'United States';

CREATE TABLE IF NOT EXISTS searches (
    id                   BIGSERIAL PRIMARY KEY,
    resume_id            BIGINT      NOT NULL REFERENCES resumes(id) ON DELETE CASCADE,
    preferences_snapshot JSONB       NOT NULL DEFAULT '{}',
    status               TEXT        NOT NULL DEFAULT 'running',
    source_health        JSONB       NOT NULL DEFAULT '{}',
    started_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at          TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_searches_resume ON searches(resume_id);

CREATE TABLE IF NOT EXISTS job_openings (
    id                 BIGSERIAL PRIMARY KEY,
    canonical_key      TEXT        NOT NULL UNIQUE,
    title              TEXT        NOT NULL DEFAULT '',
    employer           TEXT        NOT NULL DEFAULT '',
    location           TEXT        NOT NULL DEFAULT '',
    work_location_type TEXT        NOT NULL DEFAULT 'unknown',
    salary_min         INTEGER     NOT NULL DEFAULT 0,
    salary_max         INTEGER     NOT NULL DEFAULT 0,
    description        TEXT        NOT NULL DEFAULT '',
    source_names       JSONB       NOT NULL DEFAULT '[]',
    original_url       TEXT        NOT NULL DEFAULT '',
    review_status      TEXT        NOT NULL DEFAULT 'new',
    discovered_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Persist review state on the opening (not the per-search row) so a Pass/Interested
-- mark survives re-runs. Add the column to tables created before it existed.
ALTER TABLE job_openings ADD COLUMN IF NOT EXISTS review_status TEXT NOT NULL DEFAULT 'new';

CREATE TABLE IF NOT EXISTS match_results (
    id                BIGSERIAL PRIMARY KEY,
    search_id         BIGINT      NOT NULL REFERENCES searches(id) ON DELETE CASCADE,
    job_opening_id    BIGINT      NOT NULL REFERENCES job_openings(id) ON DELETE CASCADE,
    score             INTEGER     NOT NULL DEFAULT 0,
    score_explanation TEXT        NOT NULL DEFAULT '',
    gate_penalties    JSONB       NOT NULL DEFAULT '{}',
    is_qualifying     BOOLEAN     NOT NULL DEFAULT FALSE,
    rank              INTEGER     NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_match_results_search ON match_results(search_id);
CREATE INDEX IF NOT EXISTS idx_match_results_qualifying ON match_results(search_id, is_qualifying);

CREATE TABLE IF NOT EXISTS generated_documents (
    id                BIGSERIAL PRIMARY KEY,
    match_result_id   BIGINT      NOT NULL REFERENCES match_results(id) ON DELETE CASCADE,
    doc_type          TEXT        NOT NULL,
    content_markdown  TEXT        NOT NULL DEFAULT '',
    fabrication_flags JSONB       NOT NULL DEFAULT '[]',
    was_edited        BOOLEAN     NOT NULL DEFAULT FALSE,
    generated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (match_result_id, doc_type)
);

CREATE TABLE IF NOT EXISTS selections (
    id                 BIGSERIAL PRIMARY KEY,
    match_result_id    BIGINT      NOT NULL UNIQUE REFERENCES match_results(id) ON DELETE CASCADE,
    was_posting_opened BOOLEAN     NOT NULL DEFAULT FALSE,
    selected_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
