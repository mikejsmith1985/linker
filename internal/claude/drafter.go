// Package claude turns GitHub activity into a drafted LinkedIn post using the
// Claude Messages API. The Claude call sits behind a small interface so the
// drafting logic (prompt assembly + response parsing) is unit-testable without
// network access or an API key.
package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/mikejsmith1985/linker/internal/store"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DraftInput is the context handed to Claude to write a single post.
type DraftInput struct {
	Repo            string
	RepoDescription string
	EventType       store.EventType
	Title           string
	Body            string
	URL             string
	ReadmeExcerpt   string
}

// DraftOutput is the parsed result of a drafting call.
type DraftOutput struct {
	Content  string
	Hashtags string
}

// Drafter produces a LinkedIn post from GitHub activity.
type Drafter interface {
	Draft(ctx context.Context, in DraftInput) (DraftOutput, error)
}

// messageCreator is the slice of the Anthropic SDK the drafter depends on.
// *anthropic.MessageService satisfies it (pass &client.Messages).
type messageCreator interface {
	New(ctx context.Context, body anthropic.MessageNewParams, opts ...option.RequestOption) (*anthropic.Message, error)
}

// Client is the live Claude-backed Drafter.
type Client struct {
	msgs      messageCreator
	model     anthropic.Model
	system    string
	maxTokens int64
}

const defaultMaxTokens = 1500

// NewClient builds a drafter from a message creator, model id, and the persona
// system prompt. Pass &anthropic.NewClient(...).Messages as msgs in production.
func NewClient(msgs messageCreator, model string, system string) *Client {
	return &Client{
		msgs:      msgs,
		model:     anthropic.Model(model),
		system:    system,
		maxTokens: defaultMaxTokens,
	}
}

// Draft calls Claude and parses the response into post content + hashtags.
func (c *Client) Draft(ctx context.Context, in DraftInput) (DraftOutput, error) {
	resp, err := c.msgs.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    []anthropic.TextBlockParam{{Text: c.system}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(BuildUserPrompt(in))),
		},
	})
	if err != nil {
		return DraftOutput{}, fmt.Errorf("claude draft: %w", err)
	}

	text := extractText(resp)
	if strings.TrimSpace(text) == "" {
		return DraftOutput{}, fmt.Errorf("claude returned no text content")
	}
	return ParseDraft(text), nil
}

// BuildUserPrompt assembles the user-turn context for a draft request. It is
// exported so tests can assert on the prompt without invoking the model.
func BuildUserPrompt(in DraftInput) string {
	var b strings.Builder
	b.WriteString("Write a LinkedIn post about this GitHub activity. The goal is to make recruiters and teams building with AI want to hire me.\n\n")
	fmt.Fprintf(&b, "Repository: %s\n", in.Repo)
	if in.RepoDescription != "" {
		fmt.Fprintf(&b, "Repository description: %s\n", in.RepoDescription)
	}
	fmt.Fprintf(&b, "Activity type: %s\n", describeEvent(in.EventType))
	if in.Title != "" {
		fmt.Fprintf(&b, "Headline: %s\n", in.Title)
	}
	if in.Body != "" {
		fmt.Fprintf(&b, "Details:\n%s\n", strings.TrimSpace(in.Body))
	}
	if in.ReadmeExcerpt != "" {
		fmt.Fprintf(&b, "README excerpt:\n%s\n", strings.TrimSpace(in.ReadmeExcerpt))
	}
	if in.URL != "" {
		fmt.Fprintf(&b, "Link: %s\n", in.URL)
	}
	b.WriteString("\nRemember the output contract: the post, then a final HASHTAGS: line.")
	return b.String()
}

func describeEvent(t store.EventType) string {
	switch t {
	case store.EventRelease:
		return "a new release/version was shipped"
	case store.EventReadme:
		return "the project README was meaningfully updated"
	case store.EventCommit:
		return "a notable commit was pushed"
	default:
		return string(t)
	}
}

// ParseDraft splits raw model text into post body and a hashtags string,
// honoring the "HASHTAGS:" final-line contract. If no such line exists, the
// whole text is treated as the body and hashtags is empty.
func ParseDraft(raw string) DraftOutput {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if rest, ok := cutPrefixFold(trimmed, "HASHTAGS:"); ok {
			body := strings.TrimSpace(strings.Join(lines[:i], "\n"))
			return DraftOutput{Content: body, Hashtags: strings.TrimSpace(rest)}
		}
		break // last non-empty line is not a hashtags line
	}
	return DraftOutput{Content: strings.TrimSpace(raw)}
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return "", false
}

func extractText(resp *anthropic.Message) string {
	if resp == nil {
		return ""
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}
