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
	failUpsert    string          // title whose UpsertOpening returns an error
	seenKeys      map[string]bool // canonical keys treated as seen in a prior search
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
func (f *fakeStore) UpsertOpening(_ context.Context, o store.JobOpening) (int64, error) {
	if f.failUpsert != "" && o.Title == f.failUpsert {
		return 0, errFakeUpsert
	}
	f.openingSeq++
	return f.openingSeq, nil
}

var errFakeUpsert = errorString("upsert failed")

type errorString string

func (e errorString) Error() string { return string(e) }
func (f *fakeStore) FindScoredOpening(_ context.Context, key string) (store.MatchResult, error) {
	if f.seenKeys[key] {
		return store.MatchResult{Score: 75, IsQualifying: true}, nil
	}
	return store.MatchResult{}, store.ErrNotFound
}
func (f *fakeStore) SetOpeningReviewStatus(context.Context, int64, string, string) error { return nil }
func (f *fakeStore) LatestCompletedSearchID(context.Context) (int64, error) {
	return 0, store.ErrNotFound
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

	orch := New(st, disc, scorer, nil, nil, nil, nil)
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

func TestRunSearchHardExcludesHybridWhenStrict(t *testing.T) {
	st := &fakeStore{
		resume: store.Resume{ID: 1, StructuredProfile: "Skills: Go", RawText: "facts"},
		prefs:  store.Preferences{WorkLocationPref: store.WorkRemote, StrictWorkLocation: true},
	}
	disc := fakeDiscoverer{
		openings: []store.JobOpening{
			{Title: "Hybrid Role", CanonicalKey: "k-h", WorkLocationType: store.WorkHybrid},
			{Title: "Remote Role", CanonicalKey: "k-r", WorkLocationType: store.WorkRemote},
		},
		health: map[string]string{"src": jobsource.HealthSucceeded},
	}
	scorer := fakeScorer{byTitle: map[string]int{"Hybrid Role": 95, "Remote Role": 80}}

	orch := New(st, disc, scorer, nil, nil, nil, nil)
	if _, err := orch.RunSearch(context.Background()); err != nil {
		t.Fatalf("RunSearch: %v", err)
	}
	// Only the remote role is scored/persisted; the hybrid one is dropped outright.
	if len(st.created) != 1 || st.created[0].Score != 80 {
		t.Errorf("created = %+v, want only the remote role (hybrid hard-excluded)", st.created)
	}
}

func TestRunSearchSkipsUnpersistableOpening(t *testing.T) {
	st := &fakeStore{
		resume:     store.Resume{ID: 1, StructuredProfile: "Skills: Go", RawText: "facts"},
		prefs:      store.Preferences{WorkLocationPref: store.WorkRemote},
		failUpsert: "Bad", // this opening errors on persist (e.g. bad bytes)
	}
	disc := fakeDiscoverer{
		openings: []store.JobOpening{
			{Title: "Bad", CanonicalKey: "k-bad"},
			{Title: "Good", CanonicalKey: "k-good"},
		},
		health: map[string]string{"src": jobsource.HealthSucceeded},
	}
	scorer := fakeScorer{byTitle: map[string]int{"Good": 90}}

	orch := New(st, disc, scorer, nil, nil, nil, nil)
	if _, err := orch.RunSearch(context.Background()); err != nil {
		t.Fatalf("RunSearch should not fail for one bad opening: %v", err)
	}
	// The good opening is still scored; the search completes, not fails.
	if len(st.created) != 1 || st.created[0].Score != 90 {
		t.Errorf("created = %+v, want just the Good opening scored 90", st.created)
	}
	if st.finishStatus != store.SearchCompleted {
		t.Errorf("status = %q, want completed", st.finishStatus)
	}
}

func TestRunSearchURLsUsesFactoryDiscoverer(t *testing.T) {
	st := &fakeStore{
		resume: store.Resume{ID: 1, StructuredProfile: "Skills: Go", RawText: "facts"},
		prefs:  store.Preferences{WorkLocationPref: store.WorkRemote},
	}
	var gotURLs []string
	factory := func(urls []string) Discoverer {
		gotURLs = urls
		return fakeDiscoverer{
			openings: []store.JobOpening{{Title: "Pasted", CanonicalKey: "k-pasted"}},
			health:   map[string]string{"pasted-url": jobsource.HealthSucceeded},
		}
	}
	orch := New(st, fakeDiscoverer{}, fakeScorer{byTitle: map[string]int{"Pasted": 80}}, nil, factory, nil, nil)

	if _, err := orch.RunSearchURLs(context.Background(), []string{"https://x/1"}); err != nil {
		t.Fatalf("RunSearchURLs: %v", err)
	}
	if len(gotURLs) != 1 || gotURLs[0] != "https://x/1" {
		t.Errorf("factory got urls %v, want [https://x/1]", gotURLs)
	}
	if len(st.created) != 1 || st.created[0].Score != 80 {
		t.Errorf("pasted opening not scored: %+v", st.created)
	}
}

func TestRunSearchCompaniesUsesFactory(t *testing.T) {
	st := &fakeStore{
		resume: store.Resume{ID: 1, StructuredProfile: "Skills: Go", RawText: "facts"},
		prefs:  store.Preferences{WorkLocationPref: store.WorkRemote},
	}
	var gotCompanies []string
	factory := func(companies []string) Discoverer {
		gotCompanies = companies
		return fakeDiscoverer{
			openings: []store.JobOpening{{Title: "AtCompany", CanonicalKey: "k-co"}},
			health:   map[string]string{"company": jobsource.HealthSucceeded},
		}
	}
	orch := New(st, fakeDiscoverer{}, fakeScorer{byTitle: map[string]int{"AtCompany": 85}}, nil, nil, factory, nil)

	if _, err := orch.RunSearchCompanies(context.Background(), []string{"Stripe"}); err != nil {
		t.Fatalf("RunSearchCompanies: %v", err)
	}
	if len(gotCompanies) != 1 || gotCompanies[0] != "Stripe" {
		t.Errorf("factory got companies %v, want [Stripe]", gotCompanies)
	}
	if len(st.created) != 1 || st.created[0].Score != 85 {
		t.Errorf("company opening not scored: %+v", st.created)
	}
}

func TestRunSearchCompaniesUnavailableWithoutFactory(t *testing.T) {
	st := &fakeStore{resume: store.Resume{ID: 1}, prefs: store.Preferences{}}
	orch := New(st, fakeDiscoverer{}, fakeScorer{}, nil, nil, nil, nil)
	if _, err := orch.RunSearchCompanies(context.Background(), []string{"x"}); err != ErrCompanySearchUnavailable {
		t.Errorf("err = %v, want ErrCompanySearchUnavailable", err)
	}
}

func TestRunSearchURLsUnavailableWithoutFactory(t *testing.T) {
	st := &fakeStore{resume: store.Resume{ID: 1}, prefs: store.Preferences{}}
	orch := New(st, fakeDiscoverer{}, fakeScorer{}, nil, nil, nil, nil)
	if _, err := orch.RunSearchURLs(context.Background(), []string{"x"}); err != ErrURLSearchUnavailable {
		t.Errorf("err = %v, want ErrURLSearchUnavailable", err)
	}
}

func TestRunSearchErrorsWithoutResume(t *testing.T) {
	st := &fakeStore{resumeMissing: true}
	orch := New(st, fakeDiscoverer{}, fakeScorer{}, nil, nil, nil, nil)
	if _, err := orch.RunSearch(context.Background()); err != ErrNoResume {
		t.Errorf("err = %v, want ErrNoResume", err)
	}
}

// compile-time check that fakeStore satisfies the full Store interface.
var _ store.Store = (*fakeStore)(nil)

func TestCombineRolesUserFirstDeduped(t *testing.T) {
	got := combineRoles([]string{"AI Delivery Lead", "Scrum Master"}, []string{"Scrum Master", "Agile Coach"})
	want := []string{"AI Delivery Lead", "Scrum Master", "Agile Coach"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("combineRoles[%d] = %q, want %q (user roles first, deduped)", i, got[i], want[i])
		}
	}
}

func TestRunSearchNewRolesOnlySkipsSeen(t *testing.T) {
	st := &fakeStore{
		resume:   store.Resume{ID: 1, StructuredProfile: "Skills: Go", RawText: "facts"},
		prefs:    store.Preferences{WorkLocationPref: store.WorkRemote, NewRolesOnly: true},
		seenKeys: map[string]bool{"k-old": true}, // this posting was seen in a prior search
	}
	disc := fakeDiscoverer{
		openings: []store.JobOpening{
			{Title: "Old Role", CanonicalKey: "k-old", WorkLocationType: store.WorkRemote},
			{Title: "New Role", CanonicalKey: "k-new", WorkLocationType: store.WorkRemote},
		},
		health: map[string]string{"src": jobsource.HealthSucceeded},
	}
	scorer := fakeScorer{byTitle: map[string]int{"New Role": 85}}

	orch := New(st, disc, scorer, nil, nil, nil, nil)
	if _, err := orch.RunSearch(context.Background()); err != nil {
		t.Fatalf("RunSearch: %v", err)
	}
	// Only the new role is scored/persisted; the previously-seen one is skipped.
	if len(st.created) != 1 || st.created[0].Score != 85 {
		t.Errorf("created = %+v, want only the new role (seen one skipped)", st.created)
	}
}
