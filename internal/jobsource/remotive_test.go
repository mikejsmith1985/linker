package jobsource

import (
	"context"
	"strings"
	"testing"
)

const remotiveFixture = `{
  "job-count": 1,
  "jobs": [
    {
      "url": "https://remotive.com/remote-jobs/software-dev/senior-go-engineer-123",
      "title": "Senior Go Engineer",
      "company_name": "Acme Remote",
      "candidate_required_location": "USA, UK",
      "salary": "$140k - $180k",
      "description": "<p>Build <strong>distributed systems</strong> in Go.</p><script>track()</script>"
    }
  ]
}`

func TestRemotiveMapsResults(t *testing.T) {
	rt := &fakeRoundTripper{body: remotiveFixture}
	src := NewRemotive()
	src.http = rt

	out, err := src.Discover(context.Background(), Query{Keywords: []string{"go", "engineer"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d openings, want 1", len(out))
	}
	got := out[0]
	if got.Title != "Senior Go Engineer" || got.Employer != "Acme Remote" {
		t.Errorf("unexpected mapping: %+v", got)
	}
	if got.WorkLocationType != "remote" {
		t.Errorf("WorkLocationType = %q, want remote (all Remotive jobs are remote)", got.WorkLocationType)
	}
	if got.SalaryMin != 140000 || got.SalaryMax != 180000 {
		t.Errorf("salary = %d-%d, want 140000-180000", got.SalaryMin, got.SalaryMax)
	}
	if strings.Contains(got.Description, "<") || strings.Contains(got.Description, "track()") {
		t.Errorf("description not stripped of HTML/scripts: %q", got.Description)
	}
	if got.SourceName != "remotive" {
		t.Errorf("SourceName = %q, want remotive", got.SourceName)
	}
	// The search keyword must be forwarded.
	if !strings.Contains(rt.lastURL, "search=go+engineer") {
		t.Errorf("request URL missing search param: %s", rt.lastURL)
	}
}

func TestParseSalary(t *testing.T) {
	cases := []struct {
		in       string
		min, max int
	}{
		{"$140k - $180k", 140000, 180000},
		{"$120,000", 120000, 0},
		{"USD 100000", 0, 0}, // no leading $, conservatively ignored
		{"", 0, 0},
		{"Competitive", 0, 0},
	}
	for _, c := range cases {
		gotMin, gotMax := parseSalary(c.in)
		if gotMin != c.min || gotMax != c.max {
			t.Errorf("parseSalary(%q) = %d,%d; want %d,%d", c.in, gotMin, gotMax, c.min, c.max)
		}
	}
}

func TestRemotiveNonOKStatusIsError(t *testing.T) {
	src := NewRemotive()
	src.http = &fakeRoundTripper{body: "oops", status: 503}
	if _, err := src.Discover(context.Background(), Query{}); err == nil {
		t.Error("expected error on non-200 status")
	}
}
