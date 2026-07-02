package jobsource

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mikejsmith1985/linker/internal/claude"
)

// maxPageBytes bounds how much of a fetched page we read before parsing.
const maxPageBytes = 512 << 10 // 512 KiB

// URLPaste discovers openings from user-supplied posting URLs (FR-021). It
// fetches each URL and asks the LLM to extract the posting fields. A URL that
// cannot be fetched or is not a job posting fails on its own without aborting
// the batch.
type URLPaste struct {
	urls []string
	http httpDoer
	llm  claude.LLM
}

// NewURLPaste builds a URLPaste source for the given URLs.
func NewURLPaste(urls []string, llm claude.LLM) *URLPaste {
	return &URLPaste{urls: urls, http: &http.Client{}, llm: llm}
}

// Name identifies this source in health reporting.
func (u *URLPaste) Name() string { return "pasted-url" }

// Discover fetches and parses each URL. Per-URL errors are isolated; the batch
// returns whatever parsed successfully. It only returns an error when every URL
// failed, so the registry can mark the source failed rather than empty.
func (u *URLPaste) Discover(ctx context.Context, _ Query) ([]RawOpening, error) {
	var out []RawOpening
	var lastErr error
	for _, raw := range u.urls {
		opening, err := u.fetchOne(ctx, raw)
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, opening)
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

const urlExtractSystemPrompt = `You extract structured fields from the text of a web page that may be a job posting. Respond with a single JSON object and nothing else:
{"is_job_posting": <true|false>, "title": "", "employer": "", "location": "", "work_location_type": "onsite|hybrid|remote|unknown", "salary_min": <int or 0>, "salary_max": <int or 0>}
Use 0 for salaries that are not stated. Set is_job_posting to false if the page is not a single job posting.`

func (u *URLPaste) fetchOne(ctx context.Context, rawURL string) (RawOpening, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return RawOpening{}, fmt.Errorf("bad url %q: %w", rawURL, err)
	}
	resp, err := u.http.Do(req)
	if err != nil {
		return RawOpening{}, fmt.Errorf("fetch %q: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RawOpening{}, fmt.Errorf("fetch %q: status %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return RawOpening{}, fmt.Errorf("read %q: %w", rawURL, err)
	}

	text := htmlToText(string(body))
	parsed, err := u.extractPosting(ctx, text)
	if err != nil {
		return RawOpening{}, fmt.Errorf("parse %q: %w", rawURL, err)
	}
	if !parsed.IsJobPosting || strings.TrimSpace(parsed.Title) == "" {
		return RawOpening{}, fmt.Errorf("%q does not look like a job posting", rawURL)
	}

	return RawOpening{
		Title:            parsed.Title,
		Employer:         parsed.Employer,
		Location:         parsed.Location,
		Description:      truncate(text, 4000),
		OriginalURL:      rawURL,
		WorkLocationType: parsed.WorkLocationType,
		SalaryMin:        parsed.SalaryMin,
		SalaryMax:        parsed.SalaryMax,
		SourceName:       u.Name(),
	}, nil
}

// urlPosting mirrors the LLM's structured extraction.
type urlPosting struct {
	IsJobPosting     bool   `json:"is_job_posting"`
	Title            string `json:"title"`
	Employer         string `json:"employer"`
	Location         string `json:"location"`
	WorkLocationType string `json:"work_location_type"`
	SalaryMin        int    `json:"salary_min"`
	SalaryMax        int    `json:"salary_max"`
}

func (u *URLPaste) extractPosting(ctx context.Context, pageText string) (urlPosting, error) {
	raw, err := u.llm.Complete(ctx, urlExtractSystemPrompt, "PAGE TEXT:\n"+truncate(pageText, 8000))
	if err != nil {
		return urlPosting{}, err
	}
	obj := extractJSONObject(raw)
	if obj == "" {
		return urlPosting{}, fmt.Errorf("no JSON in extraction response")
	}
	var parsed urlPosting
	if err := json.Unmarshal([]byte(obj), &parsed); err != nil {
		return urlPosting{}, fmt.Errorf("decode extraction: %w", err)
	}
	return parsed, nil
}

var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)
var scriptStylePattern = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
var whitespacePattern = regexp.MustCompile(`\s+`)

// htmlToText strips scripts, styles, and tags, leaving readable text.
func htmlToText(page string) string {
	page = scriptStylePattern.ReplaceAllString(page, " ")
	page = htmlTagPattern.ReplaceAllString(page, " ")
	page = html.UnescapeString(page)
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(page, " "))
}

func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return ""
}

// truncate limits s to at most n bytes without splitting a multi-byte UTF-8
// character (which would produce an invalid byte sequence Postgres rejects).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	trimmed := s[:n]
	for len(trimmed) > 0 && !utf8.ValidString(trimmed) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed
}
