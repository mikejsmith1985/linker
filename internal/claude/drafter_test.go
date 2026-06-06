package claude

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mikejsmith1985/linker/internal/store"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type fakeMsgs struct {
	resp    *anthropic.Message
	err     error
	gotBody anthropic.MessageNewParams
	calls   int
}

func (f *fakeMsgs) New(_ context.Context, body anthropic.MessageNewParams, _ ...option.RequestOption) (*anthropic.Message, error) {
	f.calls++
	f.gotBody = body
	return f.resp, f.err
}

func textMessage(s string) *anthropic.Message {
	return &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{{Type: "text", Text: s}},
	}
}

func TestParseDraft(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		wantContent  string
		wantHashtags string
	}{
		{
			name:         "with hashtags",
			raw:          "Shipped a thing.\n\nIt was great.\n\nHASHTAGS: #Agile #AI #Go",
			wantContent:  "Shipped a thing.\n\nIt was great.",
			wantHashtags: "#Agile #AI #Go",
		},
		{
			name:        "no hashtags line",
			raw:         "Just a post body with no tags.",
			wantContent: "Just a post body with no tags.",
		},
		{
			name:         "case-insensitive and trailing blank lines",
			raw:          "Body here.\n\nhashtags: #One #Two\n\n  \n",
			wantContent:  "Body here.",
			wantHashtags: "#One #Two",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseDraft(c.raw)
			if got.Content != c.wantContent {
				t.Errorf("Content = %q, want %q", got.Content, c.wantContent)
			}
			if got.Hashtags != c.wantHashtags {
				t.Errorf("Hashtags = %q, want %q", got.Hashtags, c.wantHashtags)
			}
		})
	}
}

func TestBuildUserPrompt(t *testing.T) {
	p := BuildUserPrompt(DraftInput{
		Repo:          "me/cool",
		EventType:     store.EventRelease,
		Title:         "v1.0",
		ReadmeExcerpt: "An AI delivery tool",
		URL:           "http://example.com/r",
	})
	for _, want := range []string{"me/cool", "release", "v1.0", "An AI delivery tool", "http://example.com/r", "HASHTAGS:"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, p)
		}
	}
}

func TestDraftSuccess(t *testing.T) {
	f := &fakeMsgs{resp: textMessage("Great post.\n\nHASHTAGS: #Agile #AI")}
	c := NewClient(f, "claude-opus-4-8", "persona system prompt")

	out, err := c.Draft(context.Background(), DraftInput{Repo: "me/x", EventType: store.EventCommit})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if out.Content != "Great post." || out.Hashtags != "#Agile #AI" {
		t.Fatalf("unexpected output: %+v", out)
	}
	if f.gotBody.Model != anthropic.Model("claude-opus-4-8") {
		t.Errorf("model = %q", f.gotBody.Model)
	}
	if f.gotBody.Thinking.OfAdaptive == nil {
		t.Error("adaptive thinking not set on request")
	}
	if len(f.gotBody.System) != 1 || f.gotBody.System[0].Text != "persona system prompt" {
		t.Errorf("system prompt not wired through: %+v", f.gotBody.System)
	}
}

func TestDraftError(t *testing.T) {
	f := &fakeMsgs{err: errors.New("boom")}
	c := NewClient(f, "claude-opus-4-8", "sys")
	if _, err := c.Draft(context.Background(), DraftInput{Repo: "x"}); err == nil {
		t.Fatal("expected error from Draft")
	}
}

func TestDraftEmptyContent(t *testing.T) {
	f := &fakeMsgs{resp: textMessage("   ")}
	c := NewClient(f, "claude-opus-4-8", "sys")
	if _, err := c.Draft(context.Background(), DraftInput{Repo: "x"}); err == nil {
		t.Fatal("expected error for empty content")
	}
}
