package web

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikejsmith1985/linker/internal/resume"
	"github.com/mikejsmith1985/linker/internal/store"
)

// webFakeStore implements store.Store for handler tests.
type webFakeStore struct {
	search         store.Search
	qualifying     []store.MatchWithOpening
	listCalled     bool
	savedPrefs     store.Preferences
	activeErr      error
	match          store.MatchWithOpening
	doc            store.GeneratedDocument
	savedContent   string
	selectRecorded bool
	openedRecorded bool
	openedFlag     bool
}

func (f *webFakeStore) RunMigrations(context.Context) error                     { return nil }
func (f *webFakeStore) SaveResume(context.Context, store.Resume) (int64, error) { return 1, nil }
func (f *webFakeStore) GetActiveResume(context.Context) (store.Resume, error) {
	if f.activeErr != nil {
		return store.Resume{}, f.activeErr
	}
	return store.Resume{ID: 1, OriginalFilename: "cv.pdf", Format: "pdf", IsActive: true}, nil
}
func (f *webFakeStore) SavePreferences(_ context.Context, p store.Preferences) (int64, error) {
	f.savedPrefs = p
	return 1, nil
}
func (f *webFakeStore) GetPreferences(context.Context) (store.Preferences, error) {
	return store.Preferences{WorkLocationPref: store.WorkRemote, SalaryCurrency: "USD"}, nil
}
func (f *webFakeStore) CreateSearch(context.Context, int64, store.Preferences) (int64, error) {
	return 1, nil
}
func (f *webFakeStore) FinishSearch(context.Context, int64, store.SearchStatus, map[string]string) error {
	return nil
}
func (f *webFakeStore) GetSearch(context.Context, int64) (store.Search, error) { return f.search, nil }
func (f *webFakeStore) UpsertOpening(context.Context, store.JobOpening) (int64, error) {
	return 1, nil
}
func (f *webFakeStore) FindScoredOpening(context.Context, string) (store.MatchResult, error) {
	return store.MatchResult{}, store.ErrNotFound
}
func (f *webFakeStore) CreateMatchResult(context.Context, store.MatchResult) (int64, error) {
	return 1, nil
}
func (f *webFakeStore) ListQualifying(context.Context, int64) ([]store.MatchWithOpening, error) {
	f.listCalled = true
	return f.qualifying, nil
}
func (f *webFakeStore) GetMatchWithOpening(context.Context, int64) (store.MatchWithOpening, error) {
	return f.match, nil
}
func (f *webFakeStore) SaveDocument(context.Context, store.GeneratedDocument) (int64, error) {
	return 1, nil
}
func (f *webFakeStore) GetDocument(context.Context, int64, store.DocType) (store.GeneratedDocument, error) {
	return f.doc, nil
}
func (f *webFakeStore) UpdateDocumentContent(_ context.Context, _ int64, content string) error {
	f.savedContent = content
	return nil
}
func (f *webFakeStore) UpsertSelection(_ context.Context, _ int64, opened bool) error {
	f.openedFlag = opened
	if opened {
		f.openedRecorded = true
	} else {
		f.selectRecorded = true
	}
	return nil
}

// fakeActions records RunSearch/RunSearchURLs calls.
type fakeActions struct {
	id       int64
	gotURLs  []string
	urlsSeen bool
}

func (a *fakeActions) RunSearch(context.Context) (int64, error) { return a.id, nil }
func (a *fakeActions) RunSearchURLs(_ context.Context, urls []string) (int64, error) {
	a.urlsSeen = true
	a.gotURLs = urls
	return a.id, nil
}

// fakeIngestor returns a canned result or error.
type fakeIngestor struct{ err error }

func (i fakeIngestor) Ingest(context.Context, string, string, []byte) (store.Resume, error) {
	return store.Resume{ID: 1}, i.err
}

// fakeDocs records EnsureDocument calls and returns canned documents.
type fakeDocs struct {
	calls int
	flags []string
}

func (d *fakeDocs) EnsureDocument(_ context.Context, matchID int64, docType store.DocType, _ store.JobOpening, _ string) (store.GeneratedDocument, error) {
	d.calls++
	return store.GeneratedDocument{
		ID: matchID, MatchResultID: matchID, Type: docType,
		ContentMarkdown: "## " + string(docType), FabricationFlags: d.flags,
	}, nil
}

func newTestServer(st store.Store) http.Handler {
	return NewServer(st, fakeIngestor{}, &fakeActions{id: 7}, &fakeDocs{}, nil).Routes()
}

func newTestServerWithDocs(st store.Store, docs DocumentService) http.Handler {
	return NewServer(st, fakeIngestor{}, &fakeActions{id: 7}, docs, nil).Routes()
}

func TestSearchResultsShowsQualifyingAndHealth(t *testing.T) {
	st := &webFakeStore{
		search: store.Search{
			ID:           1,
			SourceHealth: map[string]string{"adzuna": "succeeded", "broken": "failed"},
		},
		qualifying: []store.MatchWithOpening{
			{
				MatchResult: store.MatchResult{Score: 85, ScoreExplanation: "great fit", IsQualifying: true, Rank: 1},
				Opening:     store.JobOpening{Title: "Senior Go Engineer", Employer: "Acme", OriginalURL: "https://x"},
			},
		},
	}
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/search/1", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Senior Go Engineer") || !strings.Contains(body, "85") {
		t.Error("qualifying match not rendered")
	}
	if !st.listCalled {
		t.Error("handler did not call ListQualifying (the sub-70 filter)")
	}
	if !strings.Contains(body, "adzuna") || !strings.Contains(body, "broken") || !strings.Contains(body, "failed") {
		t.Error("source health not rendered")
	}
}

func TestSearchResultsEmptyState(t *testing.T) {
	st := &webFakeStore{search: store.Search{ID: 1}, qualifying: nil}
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/search/1", nil))
	if !strings.Contains(rr.Body.String(), "No qualifying matches") {
		t.Error("empty state not rendered")
	}
}

func TestUploadRejectsUnreadableResume(t *testing.T) {
	st := &webFakeStore{}
	server := NewServer(st, fakeIngestor{err: resume.ErrUnreadable}, &fakeActions{}, &fakeDocs{}, nil).Routes()

	body, contentType := multipartResume(t, "cv.txt", "anything")
	req := httptest.NewRequest(http.MethodPost, "/resume", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unreadable resume", rr.Code)
	}
}

func TestUploadRejectsUnsupportedFormat(t *testing.T) {
	st := &webFakeStore{}
	body, contentType := multipartResume(t, "photo.png", "binary")
	req := httptest.NewRequest(http.MethodPost, "/resume", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unsupported format", rr.Code)
	}
}

func TestSearchRedirectsToResults(t *testing.T) {
	st := &webFakeStore{}
	req := httptest.NewRequest(http.MethodPost, "/search", nil)
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/search/7" {
		t.Errorf("Location = %q, want /search/7", loc)
	}
}

func TestSaveSettingsPersistsPreferences(t *testing.T) {
	st := &webFakeStore{}
	form := "required_salary_min=150000&work_location_pref=remote&willing_to_travel=on"
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, req)

	if st.savedPrefs.RequiredSalaryMin != 150000 || st.savedPrefs.WorkLocationPref != store.WorkRemote {
		t.Errorf("saved prefs = %+v", st.savedPrefs)
	}
	if !st.savedPrefs.WillingToTravel {
		t.Error("WillingToTravel not saved")
	}
}

func TestJobGeneratesDocumentsForQualifyingMatch(t *testing.T) {
	st := &webFakeStore{
		match: store.MatchWithOpening{
			MatchResult: store.MatchResult{ID: 5, Score: 88, IsQualifying: true},
			Opening:     store.JobOpening{Title: "Platform Engineer", Employer: "Acme"},
		},
	}
	docs := &fakeDocs{flags: []string{"Kubernetes"}}
	rr := httptest.NewRecorder()
	newTestServerWithDocs(st, docs).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/job/5", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	// Both documents (resume + cover) generated on view.
	if docs.calls != 2 {
		t.Errorf("EnsureDocument calls = %d, want 2 (resume + cover)", docs.calls)
	}
	// The fabrication flag must be surfaced for review, not hidden.
	if !strings.Contains(rr.Body.String(), "Kubernetes") {
		t.Error("fabrication flag not shown to user")
	}
}

func TestJobForbiddenForNonQualifyingMatch(t *testing.T) {
	st := &webFakeStore{
		match: store.MatchWithOpening{
			MatchResult: store.MatchResult{ID: 6, Score: 40, IsQualifying: false},
		},
	}
	docs := &fakeDocs{}
	rr := httptest.NewRecorder()
	newTestServerWithDocs(st, docs).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/job/6", nil))

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for sub-70 match", rr.Code)
	}
	if docs.calls != 0 {
		t.Errorf("EnsureDocument called %d times, want 0 for non-qualifying (FR-008)", docs.calls)
	}
}

func TestSaveDocumentPersistsEdit(t *testing.T) {
	st := &webFakeStore{doc: store.GeneratedDocument{ID: 9, Type: store.TailoredResume}}
	form := "content=" + "my+edited+resume"
	req := httptest.NewRequest(http.MethodPost, "/job/5/documents/tailored_resume", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if st.savedContent != "my edited resume" {
		t.Errorf("saved content = %q, want 'my edited resume'", st.savedContent)
	}
}

func TestDownloadDocumentPDF(t *testing.T) {
	st := &webFakeStore{doc: store.GeneratedDocument{ID: 9, Type: store.CoverLetter, ContentMarkdown: "# Cover\n\nDear team"}}
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/job/5/documents/cover_letter/download?fmt=pdf", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	if !strings.HasPrefix(rr.Body.String(), "%PDF") {
		t.Error("body is not a PDF")
	}
}

func TestSearchURLsParsesAndForwardsURLs(t *testing.T) {
	st := &webFakeStore{}
	actions := &fakeActions{id: 12}
	server := NewServer(st, fakeIngestor{}, actions, &fakeDocs{}, nil).Routes()

	form := "urls=" + "https://a.example/1%0Ahttps://b.example/2%20https://c.example/3"
	req := httptest.NewRequest(http.MethodPost, "/search/urls", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if rr.Header().Get("Location") != "/search/12" {
		t.Errorf("Location = %q, want /search/12", rr.Header().Get("Location"))
	}
	if len(actions.gotURLs) != 3 {
		t.Errorf("parsed %d urls, want 3: %v", len(actions.gotURLs), actions.gotURLs)
	}
}

func TestSearchURLsRejectsEmpty(t *testing.T) {
	st := &webFakeStore{}
	req := httptest.NewRequest(http.MethodPost, "/search/urls", strings.NewReader("urls=   "))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for no URLs", rr.Code)
	}
}

func TestSettingsRejectsBrowserWithoutAcknowledgment(t *testing.T) {
	st := &webFakeStore{}
	form := "work_location_pref=remote&enable_browser=on" // no browser_automation_ack
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when enabling browser without acknowledgment", rr.Code)
	}
	// Nothing should have been persisted.
	if browserEnabled(st.savedPrefs) || st.savedPrefs.BrowserAutomationAck {
		t.Error("browser source should not be saved without acknowledgment")
	}
}

func TestSettingsAllowsBrowserWithAcknowledgment(t *testing.T) {
	st := &webFakeStore{}
	form := "work_location_pref=remote&enable_browser=on&browser_automation_ack=on"
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if !st.savedPrefs.BrowserAutomationAck || !browserEnabled(st.savedPrefs) {
		t.Errorf("browser should be enabled after acknowledgment: %+v", st.savedPrefs)
	}
}

func TestOpenPostingRecordsAndRedirectsToExternalURL(t *testing.T) {
	st := &webFakeStore{
		match: store.MatchWithOpening{
			MatchResult: store.MatchResult{ID: 5, Score: 88, IsQualifying: true},
			Opening:     store.JobOpening{Title: "Engineer", OriginalURL: "https://boards.example/job/5"},
		},
	}
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/job/5/open", nil))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	// The user is taken to the ORIGINAL posting — the system submits nothing.
	if loc := rr.Header().Get("Location"); loc != "https://boards.example/job/5" {
		t.Errorf("Location = %q, want the external posting URL", loc)
	}
	// The open was recorded.
	if !st.openedRecorded {
		t.Error("open was not recorded (was_posting_opened)")
	}
	if st.openedFlag != true {
		t.Error("UpsertSelection should be called with opened=true")
	}
}

func TestSelectRecordsIntentWithoutOpening(t *testing.T) {
	st := &webFakeStore{}
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/job/5/select", nil))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if !st.selectRecorded || st.openedFlag {
		t.Error("select should record intent with opened=false")
	}
}

func TestOpenPostingWithDeadURLReturnsGone(t *testing.T) {
	st := &webFakeStore{
		match: store.MatchWithOpening{
			MatchResult: store.MatchResult{ID: 5, IsQualifying: true},
			Opening:     store.JobOpening{Title: "Engineer", OriginalURL: ""},
		},
	}
	rr := httptest.NewRecorder()
	newTestServer(st).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/job/5/open", nil))
	if rr.Code != http.StatusGone {
		t.Errorf("status = %d, want 410 for an unreachable posting", rr.Code)
	}
}

// multipartResume builds a multipart body carrying one resume file.
func multipartResume(t *testing.T, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("resume", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := io.WriteString(fw, content); err != nil {
		t.Fatalf("write: %v", err)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}
