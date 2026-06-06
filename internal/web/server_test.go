package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mikejsmith1985/linker/internal/store"
)

// ---- fakes ----

type fakeStore struct {
	drafts      []store.PostWithEvent
	history     []store.PostWithEvent
	lastQueued  *time.Time
	posts       map[int64]store.Post
	events      map[int64]store.Event
	updated     map[int64][2]string // id -> {content, hashtags}
	statusSet   map[int64]store.PostStatus
	markedExtID map[int64]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		posts:       map[int64]store.Post{},
		events:      map[int64]store.Event{},
		updated:     map[int64][2]string{},
		statusSet:   map[int64]store.PostStatus{},
		markedExtID: map[int64]string{},
	}
}

func (f *fakeStore) GetCursor(context.Context, string) (store.Cursor, error) {
	return store.Cursor{}, nil
}
func (f *fakeStore) SaveCursor(context.Context, store.Cursor) error { return nil }
func (f *fakeStore) InsertEvent(context.Context, store.Event) (int64, bool, error) {
	return 0, false, nil
}
func (f *fakeStore) GetEvent(_ context.Context, id int64) (store.Event, error) {
	return f.events[id], nil
}
func (f *fakeStore) CreatePost(context.Context, store.Post) (int64, error)   { return 0, nil }
func (f *fakeStore) GetPost(_ context.Context, id int64) (store.Post, error) { return f.posts[id], nil }
func (f *fakeStore) UpdatePostContent(_ context.Context, id int64, content, hashtags string) error {
	f.updated[id] = [2]string{content, hashtags}
	p := f.posts[id]
	p.Content, p.Hashtags = content, hashtags
	f.posts[id] = p
	return nil
}
func (f *fakeStore) SetPostStatus(_ context.Context, id int64, s store.PostStatus) error {
	f.statusSet[id] = s
	return nil
}
func (f *fakeStore) MarkQueued(_ context.Context, id int64, extID string) error {
	f.markedExtID[id] = extID
	return nil
}
func (f *fakeStore) ListDrafts(context.Context) ([]store.PostWithEvent, error) { return f.drafts, nil }
func (f *fakeStore) ListHistory(context.Context) ([]store.PostWithEvent, error) {
	return f.history, nil
}
func (f *fakeStore) LastQueuedAt(context.Context) (*time.Time, error) { return f.lastQueued, nil }

type fakePublisher struct {
	called bool
	id     string
}

func (p *fakePublisher) Queue(context.Context, store.Post) (string, error) {
	p.called = true
	return p.id, nil
}

type fakeActions struct {
	ticked      bool
	regenerated int64
}

func (a *fakeActions) Tick(context.Context) error { a.ticked = true; return nil }
func (a *fakeActions) Regenerate(_ context.Context, id int64) error {
	a.regenerated = id
	return nil
}

func do(t *testing.T, h http.Handler, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ---- tests ----

func TestDashboardRendersDrafts(t *testing.T) {
	st := newFakeStore()
	st.drafts = []store.PostWithEvent{{
		Post: store.Post{ID: 1, Content: "Shipped a delivery tool"},
		Repo: "me/linker", EventType: store.EventCommit, EventTitle: "feat",
	}}
	srv := NewServer(st, &fakePublisher{}, &fakeActions{}, nil)
	rec := do(t, srv.Routes(), http.MethodGet, "/", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Shipped a delivery tool", "me/linker", "Approve", "post-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestSaveUpdatesContent(t *testing.T) {
	st := newFakeStore()
	st.posts[1] = store.Post{ID: 1, EventID: 1}
	st.events[1] = store.Event{Repo: "me/r", Type: store.EventCommit}
	srv := NewServer(st, &fakePublisher{}, &fakeActions{}, nil)

	form := url.Values{"content": {"edited body"}, "hashtags": {"#Edited"}}
	rec := do(t, srv.Routes(), http.MethodPost, "/posts/1/save", form)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := st.updated[1]; got[0] != "edited body" || got[1] != "#Edited" {
		t.Errorf("UpdatePostContent got %v", got)
	}
	if !strings.Contains(rec.Body.String(), "edited body") {
		t.Error("response should contain edited content")
	}
}

func TestApproveQueues(t *testing.T) {
	st := newFakeStore()
	st.posts[2] = store.Post{ID: 2, EventID: 7, Content: "post"}
	st.events[7] = store.Event{Repo: "me/r", Type: store.EventRelease}
	pub := &fakePublisher{id: "buf_99"}
	srv := NewServer(st, pub, &fakeActions{}, nil)

	rec := do(t, srv.Routes(), http.MethodPost, "/posts/2/approve", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !pub.called {
		t.Error("publisher.Queue not called")
	}
	if st.markedExtID[2] != "buf_99" {
		t.Errorf("MarkQueued external id = %q, want buf_99", st.markedExtID[2])
	}
	if !strings.Contains(rec.Body.String(), "queued") {
		t.Error("response should show queued state")
	}
}

func TestRejectSetsStatus(t *testing.T) {
	st := newFakeStore()
	srv := NewServer(st, &fakePublisher{}, &fakeActions{}, nil)
	rec := do(t, srv.Routes(), http.MethodPost, "/posts/5/reject", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if st.statusSet[5] != store.StatusRejected {
		t.Errorf("status = %q, want rejected", st.statusSet[5])
	}
}

func TestRegenerateInvokesAction(t *testing.T) {
	st := newFakeStore()
	st.posts[3] = store.Post{ID: 3, EventID: 1}
	st.events[1] = store.Event{Repo: "me/r"}
	act := &fakeActions{}
	srv := NewServer(st, &fakePublisher{}, act, nil)
	rec := do(t, srv.Routes(), http.MethodPost, "/posts/3/regenerate", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if act.regenerated != 3 {
		t.Errorf("Regenerate called with %d, want 3", act.regenerated)
	}
}

func TestPollInvokesTick(t *testing.T) {
	st := newFakeStore()
	act := &fakeActions{}
	srv := NewServer(st, &fakePublisher{}, act, nil)
	rec := do(t, srv.Routes(), http.MethodPost, "/poll", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !act.ticked {
		t.Error("Tick not called")
	}
}

func TestHistoryRenders(t *testing.T) {
	st := newFakeStore()
	now := time.Now()
	st.history = []store.PostWithEvent{{
		Post: store.Post{ID: 1, Content: "Queued post body", Status: store.StatusQueued, QueuedAt: &now},
		Repo: "me/r",
	}}
	srv := NewServer(st, &fakePublisher{}, &fakeActions{}, nil)
	rec := do(t, srv.Routes(), http.MethodGet, "/history", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Queued post body") {
		t.Error("history missing post body")
	}
}

func TestBadIDReturns400(t *testing.T) {
	srv := NewServer(newFakeStore(), &fakePublisher{}, &fakeActions{}, nil)
	rec := do(t, srv.Routes(), http.MethodPost, "/posts/abc/save", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
