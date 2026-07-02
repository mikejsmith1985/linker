package documents

import (
	"context"
	"testing"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/store"
)

func TestGenerateFlagsSkillNotInResume(t *testing.T) {
	// The model claims Kubernetes, which the job wants but the resume never mentions.
	llm := &claude.Fake{Text: "Jane is an expert in Go and Kubernetes orchestration."}
	gen := NewGenerator(llm)

	opening := store.JobOpening{
		Title:       "Platform Engineer",
		Employer:    "Acme",
		Description: "We need Go and Kubernetes experience.",
	}
	got, err := gen.Generate(context.Background(), store.TailoredResume, "Skills: Go, Postgres", opening)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !containsFold(got.FabricationFlags, "Kubernetes") {
		t.Errorf("FabricationFlags = %v, want it to include Kubernetes", got.FabricationFlags)
	}
}

func TestGenerateCleanRewordHasNoFlags(t *testing.T) {
	// The model only reuses facts already in the resume.
	llm := &claude.Fake{Text: "Jane brings strong Go and Postgres experience to backend systems."}
	gen := NewGenerator(llm)

	opening := store.JobOpening{
		Title:       "Backend Engineer",
		Employer:    "Acme",
		Description: "Go and Kubernetes required.",
	}
	got, err := gen.Generate(context.Background(), store.TailoredResume, "Skills: Go, Postgres, backend systems", opening)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(got.FabricationFlags) != 0 {
		t.Errorf("FabricationFlags = %v, want empty for a clean reword", got.FabricationFlags)
	}
}

func TestGenerateRejectsEmptyResumeFacts(t *testing.T) {
	gen := NewGenerator(&claude.Fake{Text: "x"})
	if _, err := gen.Generate(context.Background(), store.CoverLetter, "  ", store.JobOpening{}); err == nil {
		t.Error("expected error when resume facts are empty")
	}
}

func TestGenerateUsesNoFabricationSystemPrompt(t *testing.T) {
	llm := &claude.Fake{Text: "content"}
	gen := NewGenerator(llm)
	_, _ = gen.Generate(context.Background(), store.TailoredResume, "Skills: Go", store.JobOpening{Title: "Eng"})
	sys := llm.Calls[0].System
	if !containsFoldStr(sys, "ONLY facts") {
		t.Errorf("system prompt should constrain to resume facts, got %q", sys)
	}
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if equalFold(s, want) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool { return len(a) == len(b) && containsFoldStr(a, b) }

func containsFoldStr(haystack, needle string) bool {
	return len(needle) == 0 || indexFold(haystack, needle) >= 0
}

func indexFold(s, sub string) int {
	ls, lsub := toLower(s), toLower(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if ls[i:i+len(lsub)] == lsub {
			return i
		}
	}
	return -1
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
