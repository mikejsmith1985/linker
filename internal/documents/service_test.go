package documents

import (
	"context"
	"testing"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/store"
)

// fakeDocStore caches documents in memory and counts saves.
type fakeDocStore struct {
	docs   map[string]store.GeneratedDocument
	saves  int
	nextID int64
}

func newFakeDocStore() *fakeDocStore {
	return &fakeDocStore{docs: map[string]store.GeneratedDocument{}}
}

func key(matchID int64, t store.DocType) string { return string(t) + ":" + itoa(matchID) }

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func (f *fakeDocStore) SaveDocument(_ context.Context, d store.GeneratedDocument) (int64, error) {
	f.saves++
	f.nextID++
	d.ID = f.nextID
	f.docs[key(d.MatchResultID, d.Type)] = d
	return d.ID, nil
}

func (f *fakeDocStore) GetDocument(_ context.Context, matchID int64, docType store.DocType) (store.GeneratedDocument, error) {
	if d, ok := f.docs[key(matchID, docType)]; ok {
		return d, nil
	}
	return store.GeneratedDocument{}, store.ErrNotFound
}

func TestEnsureDocumentGeneratesThenCaches(t *testing.T) {
	st := newFakeDocStore()
	svc := NewService(NewGenerator(&claude.Fake{Text: "Tailored with Go."}), st)

	opening := store.JobOpening{Title: "Engineer", Description: "Go."}
	first, err := svc.EnsureDocument(context.Background(), 7, store.TailoredResume, opening, "Skills: Go")
	if err != nil {
		t.Fatalf("first EnsureDocument: %v", err)
	}
	if first.ContentMarkdown == "" {
		t.Error("expected generated content")
	}

	// Second call must hit the cache — no additional save.
	second, err := svc.EnsureDocument(context.Background(), 7, store.TailoredResume, opening, "Skills: Go")
	if err != nil {
		t.Fatalf("second EnsureDocument: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("second id = %d, want cached %d", second.ID, first.ID)
	}
	if st.saves != 1 {
		t.Errorf("saves = %d, want 1 (cached on second call)", st.saves)
	}
}

func TestGenerateForProducesBothDocuments(t *testing.T) {
	st := newFakeDocStore()
	svc := NewService(NewGenerator(&claude.Fake{Text: "content"}), st)

	if err := svc.GenerateFor(context.Background(), 3, store.JobOpening{Title: "Eng"}, "Skills: Go"); err != nil {
		t.Fatalf("GenerateFor: %v", err)
	}
	if _, err := st.GetDocument(context.Background(), 3, store.TailoredResume); err != nil {
		t.Error("tailored resume not generated")
	}
	if _, err := st.GetDocument(context.Background(), 3, store.CoverLetter); err != nil {
		t.Error("cover letter not generated")
	}
}
