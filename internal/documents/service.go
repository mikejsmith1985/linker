package documents

import (
	"context"
	"errors"
	"fmt"

	"github.com/mikejsmith1985/linker/internal/store"
)

// DocStore is the slice of the store the service needs to cache documents.
type DocStore interface {
	SaveDocument(ctx context.Context, d store.GeneratedDocument) (int64, error)
	GetDocument(ctx context.Context, matchID int64, docType store.DocType) (store.GeneratedDocument, error)
}

// Service generates and persists documents, caching them so a document is
// produced at most once per match+type.
type Service struct {
	gen   *Generator
	store DocStore
}

// NewService builds the document service.
func NewService(gen *Generator, st DocStore) *Service {
	return &Service{gen: gen, store: st}
}

// GenerateFor eagerly generates and stores both documents for a match. It
// satisfies orchestrator.DocGenerator (used for the top-N qualifying openings).
func (s *Service) GenerateFor(ctx context.Context, matchID int64, opening store.JobOpening, resumeFacts string) error {
	for _, docType := range []store.DocType{store.TailoredResume, store.CoverLetter} {
		if _, err := s.EnsureDocument(ctx, matchID, docType, opening, resumeFacts); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDocument returns the cached document for a match+type, generating and
// persisting it on first request (FR-007 on-demand + cache).
func (s *Service) EnsureDocument(ctx context.Context, matchID int64, docType store.DocType, opening store.JobOpening, resumeFacts string) (store.GeneratedDocument, error) {
	existing, err := s.store.GetDocument(ctx, matchID, docType)
	if err == nil {
		return existing, nil // cached
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.GeneratedDocument{}, fmt.Errorf("lookup document: %w", err)
	}

	generated, err := s.gen.Generate(ctx, docType, resumeFacts, opening)
	if err != nil {
		return store.GeneratedDocument{}, err
	}
	doc := store.GeneratedDocument{
		MatchResultID:    matchID,
		Type:             docType,
		ContentMarkdown:  generated.ContentMarkdown,
		FabricationFlags: generated.FabricationFlags,
	}
	id, err := s.store.SaveDocument(ctx, doc)
	if err != nil {
		return store.GeneratedDocument{}, fmt.Errorf("save document: %w", err)
	}
	doc.ID = id
	return doc, nil
}
