package jobsource

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mikejsmith1985/linker/internal/claude"
)

// urlRoundTripper serves canned responses keyed by URL.
type urlRoundTripper struct {
	byURL map[string]urlResp
}

type urlResp struct {
	body   string
	status int
	err    bool
}

func (rt urlRoundTripper) Do(req *http.Request) (*http.Response, error) {
	resp, ok := rt.byURL[req.URL.String()]
	if !ok || resp.err {
		return nil, io.ErrUnexpectedEOF
	}
	status := resp.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Header:     make(http.Header),
	}, nil
}

func TestURLPasteParsesPosting(t *testing.T) {
	rt := urlRoundTripper{byURL: map[string]urlResp{
		"https://jobs.example/1": {body: "<html><body><h1>Staff Engineer</h1> at Acme</body></html>"},
	}}
	llm := &claude.Fake{Text: `{"is_job_posting": true, "title": "Staff Engineer", "employer": "Acme", "location": "Remote", "work_location_type": "remote", "salary_min": 160000, "salary_max": 200000}`}

	src := NewURLPaste([]string{"https://jobs.example/1"}, llm)
	src.http = rt

	out, err := src.Discover(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d openings, want 1", len(out))
	}
	got := out[0]
	if got.Title != "Staff Engineer" || got.Employer != "Acme" {
		t.Errorf("unexpected mapping: %+v", got)
	}
	if got.OriginalURL != "https://jobs.example/1" || got.SourceName != "pasted-url" {
		t.Errorf("url/source not set: %+v", got)
	}
	if got.SalaryMax != 200000 {
		t.Errorf("SalaryMax = %d, want 200000", got.SalaryMax)
	}
}

func TestURLPasteIsolatesPerURLFailures(t *testing.T) {
	rt := urlRoundTripper{byURL: map[string]urlResp{
		"https://jobs.example/good": {body: "<h1>Engineer</h1>"},
		"https://jobs.example/dead": {err: true},
	}}
	// The good page parses; the dead one errors at fetch.
	llm := &claude.Fake{Text: `{"is_job_posting": true, "title": "Engineer", "employer": "Acme"}`}

	src := NewURLPaste([]string{"https://jobs.example/dead", "https://jobs.example/good"}, llm)
	src.http = rt

	out, err := src.Discover(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Discover should not fail when one URL succeeds: %v", err)
	}
	if len(out) != 1 || out[0].Title != "Engineer" {
		t.Errorf("expected the good posting only, got %+v", out)
	}
}

func TestURLPasteRejectsNonJobPage(t *testing.T) {
	rt := urlRoundTripper{byURL: map[string]urlResp{
		"https://example.com/about": {body: "<h1>About us</h1>"},
	}}
	llm := &claude.Fake{Text: `{"is_job_posting": false}`}

	src := NewURLPaste([]string{"https://example.com/about"}, llm)
	src.http = rt

	if _, err := src.Discover(context.Background(), Query{}); err == nil {
		t.Error("expected error when the only URL is not a job posting")
	}
}

func TestHTMLToTextStripsTagsAndScripts(t *testing.T) {
	got := htmlToText("<style>.x{}</style><h1>Title</h1><script>alert(1)</script><p>Body&amp;more</p>")
	if strings.Contains(got, "alert") || strings.Contains(got, "<") {
		t.Errorf("scripts/tags not stripped: %q", got)
	}
	if !strings.Contains(got, "Title") || !strings.Contains(got, "Body&more") {
		t.Errorf("text not preserved: %q", got)
	}
}
