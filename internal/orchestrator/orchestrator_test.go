package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/github"
	"github.com/mikejsmith1985/linker/internal/store"
)

// ---- fakes ----

type fakeStore struct {
	cursor       store.Cursor
	savedCursor  store.Cursor
	seen         map[string]bool // dedup keys
	insertCalls  int
	posts        map[int64]store.Post
	events       map[int64]store.Event
	nextPostID   int64
	createdPosts []store.Post
}

func newFakeStore() *fakeStore {
	return &fakeStore{seen: map[string]bool{}, posts: map[int64]store.Post{}, events: map[int64]store.Event{}}
}

func key(e store.Event) string { return e.Repo + "|" + string(e.Type) + "|" + e.Ref }

func (f *fakeStore) GetCursor(context.Context, string) (store.Cursor, error) { return f.cursor, nil }
func (f *fakeStore) SaveCursor(_ context.Context, c store.Cursor) error {
	f.savedCursor = c
	return nil
}
func (f *fakeStore) InsertEvent(_ context.Context, e store.Event) (int64, bool, error) {
	f.insertCalls++
	if f.seen[key(e)] {
		return 0, false, nil
	}
	f.seen[key(e)] = true
	id := int64(len(f.seen))
	f.events[id] = e
	return id, true, nil
}
func (f *fakeStore) GetEvent(_ context.Context, id int64) (store.Event, error) {
	return f.events[id], nil
}
func (f *fakeStore) CreatePost(_ context.Context, p store.Post) (int64, error) {
	f.nextPostID++
	p.ID = f.nextPostID
	f.posts[p.ID] = p
	f.createdPosts = append(f.createdPosts, p)
	return p.ID, nil
}
func (f *fakeStore) GetPost(_ context.Context, id int64) (store.Post, error) { return f.posts[id], nil }
func (f *fakeStore) UpdatePostContent(_ context.Context, id int64, content, hashtags string) error {
	p := f.posts[id]
	p.Content, p.Hashtags = content, hashtags
	f.posts[id] = p
	return nil
}
func (f *fakeStore) SetPostStatus(context.Context, int64, store.PostStatus) error { return nil }
func (f *fakeStore) MarkQueued(context.Context, int64, string) error              { return nil }
func (f *fakeStore) ListDrafts(context.Context) ([]store.PostWithEvent, error)    { return nil, nil }
func (f *fakeStore) ListHistory(context.Context) ([]store.PostWithEvent, error)   { return nil, nil }
func (f *fakeStore) LastQueuedAt(context.Context) (*time.Time, error)             { return nil, nil }

type fakeSource struct {
	res github.Result
}

func (f *fakeSource) Poll(context.Context, string, store.Cursor) (github.Result, error) {
	return f.res, nil
}

type fakeDrafter struct {
	calls    int
	lastIn   claude.DraftInput
	out      claude.DraftOutput
	forceErr error
}

func (d *fakeDrafter) Draft(_ context.Context, in claude.DraftInput) (claude.DraftOutput, error) {
	d.calls++
	d.lastIn = in
	if d.forceErr != nil {
		return claude.DraftOutput{}, d.forceErr
	}
	return d.out, nil
}

// ---- tests ----

func TestTickDraftsNewEvents(t *testing.T) {
	st := newFakeStore()
	src := &fakeSource{res: github.Result{
		Events: []store.Event{
			{Repo: "me/r", Type: store.EventCommit, Ref: "a", Title: "feat"},
			{Repo: "me/r", Type: store.EventRelease, Ref: "v1", Title: "rel"},
		},
		Cursor:          store.Cursor{Repo: "me/r", LastCommitSHA: "a", LastReleaseTag: "v1"},
		RepoDescription: "desc",
		ReadmeExcerpt:   "readme",
	}}
	drafter := &fakeDrafter{out: claude.DraftOutput{Content: "post", Hashtags: "#x"}}
	o := New(st, src, drafter, []string{"me/r"}, nil)

	if err := o.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if drafter.calls != 2 {
		t.Errorf("drafter called %d times, want 2", drafter.calls)
	}
	if len(st.createdPosts) != 2 {
		t.Fatalf("created %d posts, want 2", len(st.createdPosts))
	}
	if st.createdPosts[0].Content != "post" || st.createdPosts[0].Status != store.StatusDraft {
		t.Errorf("unexpected post: %+v", st.createdPosts[0])
	}
	if st.savedCursor.LastCommitSHA != "a" {
		t.Errorf("cursor not saved: %+v", st.savedCursor)
	}
	// repo context flows into the draft input
	if drafter.lastIn.RepoDescription != "desc" || drafter.lastIn.ReadmeExcerpt != "readme" {
		t.Errorf("repo context not passed to drafter: %+v", drafter.lastIn)
	}
}

func TestTickDeduplicates(t *testing.T) {
	st := newFakeStore()
	st.seen[key(store.Event{Repo: "me/r", Type: store.EventCommit, Ref: "a"})] = true // already seen
	src := &fakeSource{res: github.Result{
		Events: []store.Event{{Repo: "me/r", Type: store.EventCommit, Ref: "a", Title: "dup"}},
		Cursor: store.Cursor{Repo: "me/r"},
	}}
	drafter := &fakeDrafter{}
	o := New(st, src, drafter, []string{"me/r"}, nil)

	if err := o.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if drafter.calls != 0 {
		t.Errorf("drafter called %d times on duplicate, want 0", drafter.calls)
	}
	if len(st.createdPosts) != 0 {
		t.Errorf("created %d posts on duplicate, want 0", len(st.createdPosts))
	}
}

func TestTickDraftErrorStillSavesCursor(t *testing.T) {
	st := newFakeStore()
	src := &fakeSource{res: github.Result{
		Events: []store.Event{{Repo: "me/r", Type: store.EventCommit, Ref: "a"}},
		Cursor: store.Cursor{Repo: "me/r", LastCommitSHA: "a"},
	}}
	drafter := &fakeDrafter{forceErr: context.Canceled}
	o := New(st, src, drafter, []string{"me/r"}, nil)

	if err := o.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if st.savedCursor.LastCommitSHA != "a" {
		t.Error("cursor should still advance even if drafting failed")
	}
}

func TestRegenerate(t *testing.T) {
	st := newFakeStore()
	st.events[5] = store.Event{Repo: "me/r", Type: store.EventCommit, Ref: "a", Title: "feat"}
	st.posts[9] = store.Post{ID: 9, EventID: 5, Content: "old"}
	drafter := &fakeDrafter{out: claude.DraftOutput{Content: "fresh", Hashtags: "#new"}}
	o := New(st, nil, drafter, nil, nil)

	if err := o.Regenerate(context.Background(), 9); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if got := st.posts[9]; got.Content != "fresh" || got.Hashtags != "#new" {
		t.Errorf("post not updated: %+v", got)
	}
}
