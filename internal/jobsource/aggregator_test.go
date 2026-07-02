package jobsource

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeRoundTripper returns a canned response for any request and records what it
// received.
type fakeRoundTripper struct {
	body     string
	status   int
	lastURL  string
	lastKey  string
	lastHost string
}

func (f *fakeRoundTripper) Do(req *http.Request) (*http.Response, error) {
	f.lastURL = req.URL.String()
	f.lastKey = req.Header.Get("X-RapidAPI-Key")
	f.lastHost = req.Header.Get("X-RapidAPI-Host")
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

const adzunaFixture = `{
  "results": [
    {
      "title": "Senior Go Engineer (Remote)",
      "description": "Build distributed systems. Work from home.",
      "redirect_url": "https://adzuna.example/job/1",
      "salary_min": 140000,
      "salary_max": 180000,
      "company": {"display_name": "Acme Corp"},
      "location": {"display_name": "Anywhere, US"}
    }
  ]
}`

func TestAdzunaMapsResults(t *testing.T) {
	rt := &fakeRoundTripper{body: adzunaFixture}
	a := NewAdzuna("id", "key")
	a.http = rt

	openings, err := a.Discover(context.Background(), Query{Keywords: []string{"go", "engineer"}, Location: "NYC", RequiredSalaryMin: 150000})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(openings) != 1 {
		t.Fatalf("got %d openings, want 1", len(openings))
	}
	got := openings[0]
	if got.Title != "Senior Go Engineer (Remote)" || got.Employer != "Acme Corp" {
		t.Errorf("unexpected mapping: %+v", got)
	}
	if got.SalaryMin != 140000 || got.SalaryMax != 180000 {
		t.Errorf("salary = %d-%d, want 140000-180000", got.SalaryMin, got.SalaryMax)
	}
	if got.WorkLocationType != "remote" {
		t.Errorf("WorkLocationType = %q, want remote (inferred)", got.WorkLocationType)
	}
	if got.SourceName != "adzuna" {
		t.Errorf("SourceName = %q, want adzuna", got.SourceName)
	}
	// The query params must be forwarded.
	if !strings.Contains(rt.lastURL, "salary_min=150000") || !strings.Contains(rt.lastURL, "where=NYC") {
		t.Errorf("request URL missing params: %s", rt.lastURL)
	}
}

func TestAdzunaNonOKStatusIsError(t *testing.T) {
	a := NewAdzuna("id", "key")
	a.http = &fakeRoundTripper{body: "rate limited", status: http.StatusTooManyRequests}
	if _, err := a.Discover(context.Background(), Query{Keywords: []string{"engineer"}}); err == nil {
		t.Error("expected error on non-200 status")
	}
}
