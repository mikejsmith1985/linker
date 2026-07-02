package orchestrator

import (
	"context"
	"testing"

	"github.com/mikejsmith1985/linker/internal/jobsource"
	"github.com/mikejsmith1985/linker/internal/scoring"
	"github.com/mikejsmith1985/linker/internal/store"
)

// fakeStore implements store.Store with just enough behavior for RunSearch.
type fakeStore struct {
	resume        store.Resume
	resumeMissing bool
	prefs         store.Preferences
	created       []store.MatchResult
	finishedWith  map[string]string
	finishStatus  store.SearchStatus
	openingSeq    int64
}

func (f *fakeStore) RunMigrations(context.Context) error                     { return nil }
func (f *fakeStore) SaveResume(context.Context, store.Resume) (int64, error) { return 1, nil }
func (f *fakeStore) GetActiveResume(context.Context) (store.Resume, error) {
	if f.resumeMissing {
		return store.Resume{}, store.ErrNotFound
	}
	return f.resume, nil
}
func (f *fakeStore) SavePreferences(context.Context, store.Preferences) (int64, error) { return 1, nil }
func (f *fakeStore) GetPreferences(context.Context) (store.Preferences, error)         { return f.prefs, nil }
func (f *fakeStore) CreateSearch(context.Context, int64, store.Preferences) (int64, error) {
	return 100, nil
}
func (f *fakeStore) FinishSearch(_ context.Context, _ int64, status store.SearchStatus, health map[string]string) error {
	f.finishStatus = status
	f.finishedWith = health
	return nil
}
func (f *fakeStore) GetSearch(context.Context, int64) (store.Search, error) {
	return store.Search{}, nil
}
func (f *fakeStore) UpsertOpening(context.Context, store.JobOpening) (int64, error) {
	f.openingSeq++
	return f.openingSeq, nil
}
func (f *fakeStore) FindScoredOpening(context.Context, string) (store.MatchResult, error) {
	return store.MatchResult{}, store.ErrNotFound
}
func (f *fakeStore) CreateMatchResult(_ context.Context, m store.MatchResult) (int64, error) {
	f.created = append(f.created, m)
	return int64(len(f.created)), nil
}
func (f *fakeStore) ListQualifying(context.Context, int64) ([]store.MatchWithOpening, error) {
	return nil, nil
}
func (f *fakeStore) GetMatchWithOpening(context.Context, int64) (store.MatchWithOpening, error) {
	return store.MatchWithOpening{}, nil
}
func (f *fakeStore) SaveDocument(context.Context, store.GeneratedDocument) (int64, error) {
	return 1, nil
}
func (f *fakeStore) GetDocument(context.Context, int64, store.DocType) (store.GeneratedDocument, error) {
	return store.GeneratedDocument{}, store.ErrNotFound
}
func (f *fakeStore) UpdateDocumentContent(context.Context, int64, string) error { return nil }
func (f *fakeStore) UpsertSelection(context.Context, int64, bool) error         { return nil }

// fakeDiscoverer returns canned openings and health.
type fakeDiscoverer struct {
	openings []store.JobOpening
	health   map[string]string
}

func (d fakeDiscoverer) Discover(context.Context, jobsource.Query) ([]store.JobOpening, map[string]string) {
	return d.openings, d.health
}

// fakeScorer scores by a lookup keyed on opening title.
type fakeScorer struct{ byTitle map[string]int }

func (s fakeScorer) Score(_ context.Context, _ string, opening store.JobOpening, _ store.Preferences) (scoring.Score, error) {
	v := s.byTitle[opening.Title]
	return scoring.Score{Value: v, IsQualifying: v >= scoring.QualifyingScoreThreshold, GatePenalties: map[string]int{}}, nil
}

func TestRunSearchRanksAndFlagsQualifying(t *testing.T) {
	st := &fakeStore{
		resume: store.Resume{ID: 1, StructuredProfile: "Skills: Go, Postgres", RawText: "facts"},
		prefs:  store.Preferences{WorkLocationPref: store.WorkRemote},
	}
	disc := fakeDiscoverer{
		openings: []store.JobOpening{
			{Title: "Weak", CanonicalKey: "k-weak"},
			{Title: "Strong", CanonicalKey: "k-strong"},
		},
		health: map[string]string{"adzuna": jobsource.HealthSucceeded, "broken": jobsource.HealthFailed},
	}
	scorer := fakeScorer{byTitle: map[string]int{"Weak": 40, "Strong": 90}}

	orch := New(st, disc, scorer, nil, nil)
	searchID, err := orch.RunSearch(context.Background())
	if err != nil {
		t.Fatalf("RunSearch: %v", err)
	}
	if searchID != 100 {
		t.Errorf("searchID = %d, want 100", searchID)
	}
	if len(st.created) != 2 {
		t.Fatalf("created %d match results, want 2", len(st.created))
	}
	if st.created[0].Rank != 1 || st.created[0].Score != 90 {
		t.Errorf("rank 1 = score %d, want 90", st.created[0].Score)
	}
	for _, m := range st.created {
		if m.Score < scoring.QualifyingScoreThreshold && m.IsQualifying {
			t.Errorf("score %d marked qualifying, want false", m.Score)
		}
		if m.Score >= scoring.QualifyingScoreThreshold && !m.IsQualifying {
			t.Errorf("score %d not marked qualifying, want true", m.Score)
		}
	}
	if st.finishStatus != store.SearchCompleted {
		t.Errorf("status = %q, want completed", st.finishStatus)
	}
	if st.finishedWith["broken"] != jobsource.HealthFailed {
		t.Errorf("health not recorded: %v", st.finishedWith)
	}
}

func TestRunSearchErrorsWithoutResume(t *testing.T) {
	st := &fakeStore{resumeMissing: true}
	orch := New(st, fakeDiscoverer{}, fakeScorer{}, nil, nil)
	if _, err := orch.RunSearch(context.Background()); err != ErrNoResume {
		t.Errorf("err = %v, want ErrNoResume", err)
	}
}

// compile-time check that fakeStore satisfies the full Store interface.
var _ store.Store = (*fakeStore)(nil)
