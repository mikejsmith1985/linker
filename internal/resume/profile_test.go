package resume

import (
	"context"
	"strings"
	"testing"

	"github.com/mikejsmith1985/linker/internal/claude"
)

func TestExtractProfileReturnsResult(t *testing.T) {
	llm := &claude.Fake{Text: "Skills: Go, Postgres"}
	got, err := ExtractProfile(context.Background(), llm, "Jane Doe — Go and Postgres experience")
	if err != nil {
		t.Fatalf("ExtractProfile: %v", err)
	}
	if got != "Skills: Go, Postgres" {
		t.Errorf("profile = %q", got)
	}
}

func TestExtractProfileForbidsFabricationInPrompt(t *testing.T) {
	llm := &claude.Fake{Text: "Skills: Go"}
	_, _ = ExtractProfile(context.Background(), llm, "text")
	if !strings.Contains(strings.ToLower(llm.Calls[0].System), "never invent") {
		t.Error("profile system prompt should forbid fabrication")
	}
}

func TestExtractProfileRejectsEmptyInput(t *testing.T) {
	llm := &claude.Fake{Text: "x"}
	if _, err := ExtractProfile(context.Background(), llm, "   "); err != ErrUnreadable {
		t.Errorf("err = %v, want ErrUnreadable", err)
	}
}
