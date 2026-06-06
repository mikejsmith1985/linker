// Package buffer publishes drafted posts to a LinkedIn channel via the Buffer
// API. The Publisher interface lets the rest of the app stay agnostic: a live
// Buffer client is used when credentials are present, otherwise a stub that
// records the post locally so the whole app works end-to-end without Buffer.
package buffer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mikejsmith1985/linker/internal/store"
)

// Publisher queues a post to the configured LinkedIn channel and returns an
// external identifier for it (a Buffer update id, or a synthetic id in stub
// mode).
type Publisher interface {
	Queue(ctx context.Context, p store.Post) (externalID string, err error)
}

// ComposeText renders a post's body and hashtags into the single text blob
// Buffer expects.
func ComposeText(p store.Post) string {
	text := strings.TrimSpace(p.Content)
	if tags := strings.TrimSpace(p.Hashtags); tags != "" {
		text = text + "\n\n" + tags
	}
	return text
}

// ---- Live Buffer client ----

const defaultBaseURL = "https://api.bufferapp.com/1"

// LiveClient publishes to Buffer's update-create endpoint.
type LiveClient struct {
	httpClient  *http.Client
	baseURL     string
	accessToken string
	profileID   string
}

// NewLiveClient builds a live Buffer publisher.
func NewLiveClient(accessToken, profileID string) *LiveClient {
	return &LiveClient{
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		baseURL:     defaultBaseURL,
		accessToken: accessToken,
		profileID:   profileID,
	}
}

type bufferResponse struct {
	Success bool `json:"success"`
	Updates []struct {
		ID string `json:"id"`
	} `json:"updates"`
	Message string `json:"message"`
}

// Queue posts the rendered text to Buffer for the configured profile.
func (c *LiveClient) Queue(ctx context.Context, p store.Post) (string, error) {
	form := url.Values{}
	form.Set("text", ComposeText(p))
	form.Add("profile_ids[]", c.profileID)
	form.Set("access_token", c.accessToken)

	endpoint := strings.TrimRight(c.baseURL, "/") + "/updates/create.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build buffer request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call buffer: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("buffer returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed bufferResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode buffer response: %w", err)
	}
	if !parsed.Success {
		return "", fmt.Errorf("buffer rejected post: %s", parsed.Message)
	}
	if len(parsed.Updates) == 0 || parsed.Updates[0].ID == "" {
		return "", fmt.Errorf("buffer response contained no update id")
	}
	return parsed.Updates[0].ID, nil
}

// ---- Stub publisher ----

// Stub records the post it would have published and returns a synthetic id.
// Used when Buffer is not configured so the dashboard flow works end-to-end.
type Stub struct {
	log *slog.Logger
	seq atomic.Int64
}

// NewStub builds a stub publisher. A nil logger falls back to slog.Default().
func NewStub(log *slog.Logger) *Stub {
	if log == nil {
		log = slog.Default()
	}
	return &Stub{log: log}
}

// Queue logs the would-be post and returns a unique synthetic id.
func (s *Stub) Queue(_ context.Context, p store.Post) (string, error) {
	id := fmt.Sprintf("stub-%d", s.seq.Add(1))
	s.log.Info("buffer stub: queued post (no external call)",
		"external_id", id, "post_id", p.ID, "text", ComposeText(p))
	return id, nil
}
