package assistant

import (
	"context"
	"testing"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/store"
)

// fakeStore implements the assistant's Store.
type fakeStore struct {
	prefs   store.Preferences
	saved   *store.Preferences
	matches []store.MatchWithOpening
	chat    []store.ChatMessage
}

func (f *fakeStore) GetPreferences(context.Context) (store.Preferences, error) { return f.prefs, nil }
func (f *fakeStore) SavePreferences(_ context.Context, p store.Preferences) (int64, error) {
	f.saved = &p
	f.prefs = p
	return 1, nil
}
func (f *fakeStore) LatestCompletedSearchID(context.Context) (int64, error) {
	if len(f.matches) == 0 {
		return 0, store.ErrNotFound
	}
	return 7, nil
}
func (f *fakeStore) ListQualifying(context.Context, int64) ([]store.MatchWithOpening, error) {
	return f.matches, nil
}
func (f *fakeStore) AppendChatMessage(_ context.Context, role, content string) error {
	f.chat = append(f.chat, store.ChatMessage{Role: role, Content: content})
	return nil
}
func (f *fakeStore) ListChatMessages(context.Context, int) ([]store.ChatMessage, error) {
	return f.chat, nil
}

// fakeSearcher records StartSearch calls.
type fakeSearcher struct{ started bool }

func (s *fakeSearcher) StartSearch(context.Context) (int64, error) {
	s.started = true
	return 42, nil
}

func TestHandleUpdatesPreferencesAndSearches(t *testing.T) {
	st := &fakeStore{prefs: store.Preferences{WorkLocationPref: store.WorkHybrid}}
	searcher := &fakeSearcher{}
	llm := &claude.Fake{Text: `{"reply":"Set to remote-only and searching.","update_preferences":{"work_location":"remote","strict_work_location":true},"start_search":true}`}
	a := New(llm, st, searcher)

	reply, err := a.Handle(context.Background(), "only remote roles please, then search")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if st.saved == nil || st.saved.WorkLocationPref != store.WorkRemote || !st.saved.StrictWorkLocation {
		t.Errorf("preferences not applied: %+v", st.saved)
	}
	if !searcher.started {
		t.Error("search was not started")
	}
	if reply == "" || len(st.chat) != 2 {
		t.Errorf("expected reply + 2 persisted messages, got reply=%q chat=%d", reply, len(st.chat))
	}
}

func TestHandleAddsTargetRolesDeduped(t *testing.T) {
	st := &fakeStore{prefs: store.Preferences{TargetRoles: []string{"Scrum Master"}}}
	llm := &claude.Fake{Text: `{"reply":"Added.","update_preferences":{"add_target_roles":["AI Delivery Lead","Scrum Master"]},"start_search":false}`}
	a := New(llm, st, &fakeSearcher{})

	if _, err := a.Handle(context.Background(), "also look for AI Delivery Lead"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(st.saved.TargetRoles) != 2 {
		t.Errorf("target roles = %v, want deduped [Scrum Master, AI Delivery Lead]", st.saved.TargetRoles)
	}
}

func TestHandleAnswersWithoutActions(t *testing.T) {
	st := &fakeStore{
		prefs:   store.Preferences{WorkLocationPref: store.WorkRemote},
		matches: []store.MatchWithOpening{{MatchResult: store.MatchResult{Score: 90}, Opening: store.JobOpening{Title: "Scrum Master", Employer: "Acme"}}},
	}
	searcher := &fakeSearcher{}
	llm := &claude.Fake{Text: `{"reply":"Your top match is Scrum Master at Acme (90).","update_preferences":null,"start_search":false}`}
	a := New(llm, st, searcher)

	reply, err := a.Handle(context.Background(), "what's my best match?")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if st.saved != nil || searcher.started {
		t.Error("a question should not change settings or start a search")
	}
	if reply == "" {
		t.Error("expected a reply")
	}
	// The prompt must have included the match so the model can answer.
	if len(llm.Calls) != 1 || !contains(llm.Calls[0].Prompt, "Scrum Master") {
		t.Error("prompt should include the latest matches")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
