package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed schema.sql
var schemaSQL string

// DB is the subset of the pgx pool API that the store needs. Both
// *pgxpool.Pool and pgxmock's pool interface satisfy it, so the store can be
// unit-tested without a real database.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Store is the persistence interface consumed by the orchestrator and web
// layers. Defining it here keeps a single source of truth; consumers accept
// the interface so they can be tested with fakes.
type Store interface {
	GetCursor(ctx context.Context, repo string) (Cursor, error)
	SaveCursor(ctx context.Context, c Cursor) error
	InsertEvent(ctx context.Context, e Event) (id int64, inserted bool, err error)
	GetEvent(ctx context.Context, id int64) (Event, error)
	CreatePost(ctx context.Context, p Post) (int64, error)
	GetPost(ctx context.Context, id int64) (Post, error)
	UpdatePostContent(ctx context.Context, id int64, content, hashtags string) error
	SetPostStatus(ctx context.Context, id int64, status PostStatus) error
	MarkQueued(ctx context.Context, id int64, externalID string) error
	ListDrafts(ctx context.Context) ([]PostWithEvent, error)
	ListHistory(ctx context.Context) ([]PostWithEvent, error)
	LastQueuedAt(ctx context.Context) (*time.Time, error)
}

// PG is the Postgres-backed Store implementation.
type PG struct {
	db DB
}

// New returns a Store backed by the given DB handle.
func New(db DB) *PG { return &PG{db: db} }

// RunMigrations applies the embedded schema. It is idempotent.
func (s *PG) RunMigrations(ctx context.Context) error {
	if _, err := s.db.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// GetCursor returns the stored cursor for repo, or a zero-value cursor (with
// Repo set) when none exists yet.
func (s *PG) GetCursor(ctx context.Context, repo string) (Cursor, error) {
	c := Cursor{Repo: repo}
	err := s.db.QueryRow(ctx,
		`SELECT last_commit_sha, last_release_tag, readme_hash
		   FROM repo_cursors WHERE repo = $1`, repo,
	).Scan(&c.LastCommitSHA, &c.LastReleaseTag, &c.ReadmeHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return Cursor{}, fmt.Errorf("get cursor: %w", err)
	}
	return c, nil
}

// SaveCursor upserts a repo cursor.
func (s *PG) SaveCursor(ctx context.Context, c Cursor) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO repo_cursors (repo, last_commit_sha, last_release_tag, readme_hash, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (repo) DO UPDATE SET
		   last_commit_sha = EXCLUDED.last_commit_sha,
		   last_release_tag = EXCLUDED.last_release_tag,
		   readme_hash = EXCLUDED.readme_hash,
		   updated_at = now()`,
		c.Repo, c.LastCommitSHA, c.LastReleaseTag, c.ReadmeHash)
	if err != nil {
		return fmt.Errorf("save cursor: %w", err)
	}
	return nil
}

// InsertEvent inserts an event, ignoring duplicates on (repo, event_type, ref).
// inserted is false when the event already existed.
func (s *PG) InsertEvent(ctx context.Context, e Event) (int64, bool, error) {
	var id int64
	err := s.db.QueryRow(ctx,
		`INSERT INTO activity_events (repo, event_type, ref, title, body, url)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (repo, event_type, ref) DO NOTHING
		 RETURNING id`,
		e.Repo, string(e.Type), e.Ref, e.Title, e.Body, e.URL,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil // duplicate
	}
	if err != nil {
		return 0, false, fmt.Errorf("insert event: %w", err)
	}
	return id, true, nil
}

// GetEvent loads a single event by id.
func (s *PG) GetEvent(ctx context.Context, id int64) (Event, error) {
	var e Event
	var typ string
	err := s.db.QueryRow(ctx,
		`SELECT id, repo, event_type, ref, title, body, url, detected_at
		   FROM activity_events WHERE id = $1`, id,
	).Scan(&e.ID, &e.Repo, &typ, &e.Ref, &e.Title, &e.Body, &e.URL, &e.DetectedAt)
	if err != nil {
		return Event{}, fmt.Errorf("get event: %w", err)
	}
	e.Type = EventType(typ)
	return e, nil
}

// CreatePost inserts a new post and returns its id.
func (s *PG) CreatePost(ctx context.Context, p Post) (int64, error) {
	if p.Status == "" {
		p.Status = StatusDraft
	}
	var id int64
	err := s.db.QueryRow(ctx,
		`INSERT INTO posts (event_id, content, hashtags, status)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		p.EventID, p.Content, p.Hashtags, string(p.Status),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create post: %w", err)
	}
	return id, nil
}

// GetPost loads a single post by id.
func (s *PG) GetPost(ctx context.Context, id int64) (Post, error) {
	var p Post
	var status string
	err := s.db.QueryRow(ctx,
		`SELECT id, event_id, content, hashtags, status, external_id, created_at, updated_at, queued_at
		   FROM posts WHERE id = $1`, id,
	).Scan(&p.ID, &p.EventID, &p.Content, &p.Hashtags, &status, &p.ExternalID, &p.CreatedAt, &p.UpdatedAt, &p.QueuedAt)
	if err != nil {
		return Post{}, fmt.Errorf("get post: %w", err)
	}
	p.Status = PostStatus(status)
	return p, nil
}

// UpdatePostContent saves edited content and hashtags.
func (s *PG) UpdatePostContent(ctx context.Context, id int64, content, hashtags string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE posts SET content = $2, hashtags = $3, updated_at = now() WHERE id = $1`,
		id, content, hashtags)
	if err != nil {
		return fmt.Errorf("update post content: %w", err)
	}
	return nil
}

// SetPostStatus updates only the status (e.g. rejection).
func (s *PG) SetPostStatus(ctx context.Context, id int64, status PostStatus) error {
	_, err := s.db.Exec(ctx,
		`UPDATE posts SET status = $2, updated_at = now() WHERE id = $1`,
		id, string(status))
	if err != nil {
		return fmt.Errorf("set post status: %w", err)
	}
	return nil
}

// MarkQueued records that a post was handed to the publisher.
func (s *PG) MarkQueued(ctx context.Context, id int64, externalID string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE posts SET status = 'queued', external_id = $2, queued_at = now(), updated_at = now()
		   WHERE id = $1`,
		id, externalID)
	if err != nil {
		return fmt.Errorf("mark queued: %w", err)
	}
	return nil
}

// ListDrafts returns draft posts joined to their source events, newest first.
func (s *PG) ListDrafts(ctx context.Context) ([]PostWithEvent, error) {
	return s.listPosts(ctx,
		`WHERE p.status = 'draft' ORDER BY p.created_at DESC`)
}

// ListHistory returns queued/published posts, most recently queued first.
func (s *PG) ListHistory(ctx context.Context) ([]PostWithEvent, error) {
	return s.listPosts(ctx,
		`WHERE p.status IN ('queued','published') ORDER BY p.queued_at DESC NULLS LAST`)
}

func (s *PG) listPosts(ctx context.Context, whereOrder string) ([]PostWithEvent, error) {
	rows, err := s.db.Query(ctx,
		`SELECT p.id, p.event_id, p.content, p.hashtags, p.status, p.external_id,
		        p.created_at, p.updated_at, p.queued_at,
		        e.repo, e.event_type, e.title, e.url
		   FROM posts p JOIN activity_events e ON e.id = p.event_id `+whereOrder)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()

	var out []PostWithEvent
	for rows.Next() {
		var pe PostWithEvent
		var status, etype string
		if err := rows.Scan(
			&pe.ID, &pe.EventID, &pe.Content, &pe.Hashtags, &status, &pe.ExternalID,
			&pe.CreatedAt, &pe.UpdatedAt, &pe.QueuedAt,
			&pe.Repo, &etype, &pe.EventTitle, &pe.EventURL,
		); err != nil {
			return nil, fmt.Errorf("scan post: %w", err)
		}
		pe.Status = PostStatus(status)
		pe.EventType = EventType(etype)
		out = append(out, pe)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate posts: %w", err)
	}
	return out, nil
}

// LastQueuedAt returns the most recent queued_at across all posts, or nil when
// nothing has been queued yet. Used for the cadence guard.
func (s *PG) LastQueuedAt(ctx context.Context) (*time.Time, error) {
	var t *time.Time
	if err := s.db.QueryRow(ctx,
		`SELECT max(queued_at) FROM posts`).Scan(&t); err != nil {
		return nil, fmt.Errorf("last queued at: %w", err)
	}
	return t, nil
}
