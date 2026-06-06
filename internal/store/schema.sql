-- linker schema. Idempotent: safe to run on every startup.

CREATE TABLE IF NOT EXISTS activity_events (
    id          BIGSERIAL PRIMARY KEY,
    repo        TEXT        NOT NULL,
    event_type  TEXT        NOT NULL,
    ref         TEXT        NOT NULL,
    title       TEXT        NOT NULL DEFAULT '',
    body        TEXT        NOT NULL DEFAULT '',
    url         TEXT        NOT NULL DEFAULT '',
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo, event_type, ref)
);

CREATE TABLE IF NOT EXISTS repo_cursors (
    repo             TEXT PRIMARY KEY,
    last_commit_sha  TEXT NOT NULL DEFAULT '',
    last_release_tag TEXT NOT NULL DEFAULT '',
    readme_hash      TEXT NOT NULL DEFAULT '',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS posts (
    id          BIGSERIAL PRIMARY KEY,
    event_id    BIGINT      NOT NULL REFERENCES activity_events(id) ON DELETE CASCADE,
    content     TEXT        NOT NULL DEFAULT '',
    hashtags    TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'draft',
    external_id TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    queued_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_posts_status ON posts(status);
CREATE INDEX IF NOT EXISTS idx_posts_queued_at ON posts(queued_at);
