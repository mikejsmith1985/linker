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

func TestSaveResumeDeactivatesThenInserts(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	mock.ExpectExec("UPDATE resumes SET is_active = FALSE").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectQuery("INSERT INTO resumes").
		WithArgs("cv.pdf", "pdf", "raw text", "profile").
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(int64(3)))

	id, err := s.SaveResume(context.Background(), Resume{
		OriginalFilename: "cv.pdf", Format: "pdf", RawText: "raw text", StructuredProfile: "profile",
	})
	if err != nil {
		t.Fatalf("SaveResume: %v", err)
	}
	if id != 3 {
		t.Errorf("id = %d, want 3", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestGetActiveResumeNotFound(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	mock.ExpectQuery("SELECT id, original_filename").
		WillReturnError(pgx.ErrNoRows)

	if _, err := s.GetActiveResume(context.Background()); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateMatchResult(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	mock.ExpectQuery("INSERT INTO match_results").
		WithArgs(int64(5), int64(9), 82, "great fit", `{"salary":0}`, true, 1).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(int64(11)))

	id, err := s.CreateMatchResult(context.Background(), MatchResult{
		SearchID: 5, JobOpeningID: 9, Score: 82, ScoreExplanation: "great fit",
		GatePenalties: map[string]int{"salary": 0}, IsQualifying: true, Rank: 1,
	})
	if err != nil {
		t.Fatalf("CreateMatchResult: %v", err)
	}
	if id != 11 {
		t.Errorf("id = %d, want 11", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestListQualifyingScansJoin(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	cols := []string{
		"id", "search_id", "job_opening_id", "score", "score_explanation",
		"gate_penalties", "is_qualifying", "rank",
		"o_id", "canonical_key", "title", "employer", "location", "work_location_type",
		"salary_min", "salary_max", "description", "source_names", "original_url", "review_status", "review_reason", "discovered_at",
	}
	now := time.Now()
	mock.ExpectQuery("FROM match_results m JOIN job_openings o").
		WithArgs(int64(5)).
		WillReturnRows(pgxmock.NewRows(cols).AddRow(
			int64(11), int64(5), int64(9), 82, "great fit",
			[]byte(`{"salary":0}`), true, 1,
			int64(9), "acme|engineer|remote", "Engineer", "Acme", "Remote", "remote",
			0, 0, "desc", []byte(`["adzuna"]`), "https://x", "new", "", now,
		))

	out, err := s.ListQualifying(context.Background(), 5)
	if err != nil {
		t.Fatalf("ListQualifying: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d results, want 1", len(out))
	}
	if out[0].Score != 82 || out[0].Opening.Employer != "Acme" {
		t.Errorf("unexpected result: %+v", out[0])
	}
	if len(out[0].Opening.SourceNames) != 1 || out[0].Opening.SourceNames[0] != "adzuna" {
		t.Errorf("SourceNames = %v, want [adzuna]", out[0].Opening.SourceNames)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestGetPreferencesDefaultsWhenEmpty(t *testing.T) {
	mock := newMock(t)
	s := New(mock)

	mock.ExpectQuery("FROM preferences ORDER BY id DESC").
		WillReturnError(pgx.ErrNoRows)

	p, err := s.GetPreferences(context.Background())
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if p.WorkLocationPref != WorkRemote || p.SalaryCurrency != "USD" || p.Location != "United States" {
		t.Errorf("defaults = %+v, want remote/USD/United States", p)
	}
}
