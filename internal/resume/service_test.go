package resume

import (
	"context"
	"testing"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/store"
)

// fakeSaver records the resume passed to SaveResume.
type fakeSaver struct{ saved store.Resume }

func (f *fakeSaver) SaveResume(_ context.Context, r store.Resume) (int64, error) {
	f.saved = r
	return 42, nil
}

func TestIngestExtractsStructuresAndSaves(t *testing.T) {
	saver := &fakeSaver{}
	llm := &claude.Fake{Text: "Skills: Go"}
	svc := NewService(llm, saver)

	got, err := svc.Ingest(context.Background(), "cv.txt", FormatTXT, []byte("Jane Doe — Go engineer"))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got.ID != 42 || !got.IsActive {
		t.Errorf("returned resume = %+v, want id 42 active", got)
	}
	if saver.saved.RawText == "" || saver.saved.StructuredProfile != "Skills: Go" {
		t.Errorf("saved resume missing extracted fields: %+v", saver.saved)
	}
}

func TestIngestRejectsUnreadable(t *testing.T) {
	svc := NewService(&claude.Fake{}, &fakeSaver{})
	if _, err := svc.Ingest(context.Background(), "cv.txt", FormatTXT, []byte("   ")); err != ErrUnreadable {
		t.Errorf("err = %v, want ErrUnreadable", err)
	}
}
