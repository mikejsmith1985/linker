package resume

import (
	"context"
	"fmt"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/store"
)

// saver is the slice of the store the ingest service needs.
type saver interface {
	SaveResume(ctx context.Context, r store.Resume) (int64, error)
}

// Service ingests an uploaded resume: extract text, structure a profile, and
// persist it as the single active resume.
type Service struct {
	llm   claude.LLM
	store saver
}

// NewService builds the ingest service.
func NewService(llm claude.LLM, st saver) *Service {
	return &Service{llm: llm, store: st}
}

// Ingest validates, extracts, structures, and stores an uploaded resume,
// returning the saved resume. It rejects unreadable input (FR-018).
func (s *Service) Ingest(ctx context.Context, filename, format string, data []byte) (store.Resume, error) {
	text, err := ExtractText(format, data)
	if err != nil {
		return store.Resume{}, err
	}
	profile, err := ExtractProfile(ctx, s.llm, text)
	if err != nil {
		return store.Resume{}, fmt.Errorf("structure profile: %w", err)
	}
	resume := store.Resume{
		OriginalFilename:  filename,
		Format:            format,
		RawText:           text,
		StructuredProfile: profile,
	}
	id, err := s.store.SaveResume(ctx, resume)
	if err != nil {
		return store.Resume{}, err
	}
	resume.ID = id
	resume.IsActive = true
	return resume, nil
}
