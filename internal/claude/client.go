// Package claude wraps the Claude Messages API behind a tiny LLM interface so
// the scoring, resume-parsing, and document-generation packages can call the
// model in production and swap in a fake for unit tests — 100% mocked, <10ms.
package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// LLM is the minimal text-completion capability the matcher needs from Claude.
// Every LLM-dependent package depends on this interface, never the SDK directly,
// which keeps their unit tests fast and offline (Article V).
type LLM interface {
	// Complete sends a system prompt and a user prompt and returns the model's
	// text response.
	Complete(ctx context.Context, system, prompt string) (string, error)
}

// messageCreator is the slice of the Anthropic SDK the client depends on.
// *anthropic.MessageService satisfies it (pass &client.Messages).
type messageCreator interface {
	New(ctx context.Context, body anthropic.MessageNewParams, opts ...option.RequestOption) (*anthropic.Message, error)
}

// Client is the live Claude-backed LLM.
type Client struct {
	msgs      messageCreator
	model     anthropic.Model
	maxTokens int64
}

// defaultMaxTokens is generous enough for a tailored resume or cover letter.
const defaultMaxTokens = 4096

// NewClient builds an LLM from a message creator and model id. Pass
// &anthropic.NewClient(...).Messages as msgs in production.
func NewClient(msgs messageCreator, model string) *Client {
	return &Client{
		msgs:      msgs,
		model:     anthropic.Model(model),
		maxTokens: defaultMaxTokens,
	}
}

// Complete calls Claude once and returns the concatenated text of the response.
func (c *Client) Complete(ctx context.Context, system, prompt string) (string, error) {
	resp, err := c.msgs.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("claude complete: %w", err)
	}
	text := extractText(resp)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("claude returned no text content")
	}
	return text, nil
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
