package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
)

func newMock(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(mock.Close)
	return mock
}

func TestInsertEventInserted(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	mock.ExpectQuery("INSERT INTO activity_events").
		WithArgs("o/r", "commit", "abc", "t", "b", "u").
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(int64(7)))

	id, inserted, err := s.InsertEvent(context.Background(), Event{
		Repo: "o/r", Type: EventCommit, Ref: "abc", Title: "t", Body: "b", URL: "u",
	})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if !inserted || id != 7 {
		t.Fatalf("got id=%d inserted=%v, want 7,true", id, inserted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestInsertEventDuplicate(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	mock.ExpectQuery("INSERT INTO activity_events").
		WithArgs("o/r", "commit", "abc", "", "", "").
		WillReturnError(pgx.ErrNoRows)

	id, inserted, err := s.InsertEvent(context.Background(), Event{
		Repo: "o/r", Type: EventCommit, Ref: "abc",
	})
	if err != nil {
		t.Fatalf("InsertEvent returned error on duplicate: %v", err)
	}
	if inserted || id != 0 {
		t.Fatalf("got id=%d inserted=%v, want 0,false", id, inserted)
	}
}

func TestMarkQueued(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	mock.ExpectExec("UPDATE posts SET status = 'queued'").
		WithArgs(int64(3), "buf_1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	if err := s.MarkQueued(context.Background(), 3, "buf_1"); err != nil {
		t.Fatalf("MarkQueued: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestGetCursorNoRows(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	mock.ExpectQuery("FROM repo_cursors").
		WithArgs("o/r").
		WillReturnError(pgx.ErrNoRows)

	c, err := s.GetCursor(context.Background(), "o/r")
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if c.Repo != "o/r" || c.LastCommitSHA != "" {
		t.Fatalf("got %+v, want zero cursor for o/r", c)
	}
}

func TestLastQueuedAtNull(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	mock.ExpectQuery("SELECT max\\(queued_at\\) FROM posts").
		WillReturnRows(pgxmock.NewRows([]string{"max"}).AddRow((*time.Time)(nil)))

	got, err := s.LastQueuedAt(context.Background())
	if err != nil {
		t.Fatalf("LastQueuedAt: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestLastQueuedAtValue(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	tm := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT max\\(queued_at\\) FROM posts").
		WillReturnRows(pgxmock.NewRows([]string{"max"}).AddRow(&tm))

	got, err := s.LastQueuedAt(context.Background())
	if err != nil {
		t.Fatalf("LastQueuedAt: %v", err)
	}
	if got == nil || !got.Equal(tm) {
		t.Fatalf("got %v, want %v", got, tm)
	}
}

func TestListDrafts(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	now := time.Now()
	mock.ExpectQuery("FROM posts p JOIN activity_events e").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "event_id", "content", "hashtags", "status", "external_id",
			"created_at", "updated_at", "queued_at",
			"repo", "event_type", "title", "url",
		}).AddRow(
			int64(1), int64(2), "hello", "#go", "draft", "",
			now, now, (*time.Time)(nil),
			"o/r", "commit", "Fix bug", "http://x",
		))

	drafts, err := s.ListDrafts(context.Background())
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("got %d drafts, want 1", len(drafts))
	}
	d := drafts[0]
	if d.ID != 1 || d.Content != "hello" || d.Repo != "o/r" || d.EventType != EventCommit {
		t.Fatalf("unexpected draft: %+v", d)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}
