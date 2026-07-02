package jobsource

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const jsearchFixture = `{
  "status": "OK",
  "data": [
    {
      "job_title": "Senior Backend Engineer",
      "employer_name": "Acme",
      "job_apply_link": "https://www.linkedin.com/jobs/view/123",
      "job_description": "Build Go services.",
      "job_is_remote": true,
      "job_city": "New York",
      "job_state": "NY",
      "job_country": "US",
      "job_min_salary": 150000,
      "job_max_salary": 200000,
      "job_salary_period": "YEAR"
    },
    {
      "job_title": "Contractor",
      "employer_name": "Beta",
      "job_apply_link": "https://x",
      "job_is_remote": false,
      "job_min_salary": 60,
      "job_max_salary": 80,
      "job_salary_period": "HOUR"
    }
  ]
}`

func TestJSearchMapsResults(t *testing.T) {
	rt := &fakeRoundTripper{body: jsearchFixture}
	j := NewJSearch("test-key")
	j.http = rt

	out, err := j.Discover(context.Background(), Query{Keywords: []string{"backend", "engineer"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d openings, want 2", len(out))
	}

	remote := out[0]
	if remote.Title != "Senior Backend Engineer" || remote.Employer != "Acme" {
		t.Errorf("unexpected mapping: %+v", remote)
	}
	if remote.WorkLocationType != "remote" {
		t.Errorf("WorkLocationType = %q, want remote", remote.WorkLocationType)
	}
	if remote.Location != "New York, NY, US" {
		t.Errorf("Location = %q, want joined city/state/country", remote.Location)
	}
	if remote.SalaryMin != 150000 || remote.SalaryMax != 200000 {
		t.Errorf("annual salary = %d-%d, want 150000-200000", remote.SalaryMin, remote.SalaryMax)
	}
	if remote.SourceName != "jsearch" {
		t.Errorf("SourceName = %q, want jsearch", remote.SourceName)
	}

	// Hourly rates must NOT be reported as an annual salary.
	if out[1].SalaryMin != 0 || out[1].SalaryMax != 0 {
		t.Errorf("hourly salary leaked as annual: %+v", out[1])
	}

	// The API key + host headers must be sent.
	if rt.lastKey != "test-key" || rt.lastHost != "jsearch.p.rapidapi.com" {
		t.Errorf("auth headers not sent: key=%q host=%q", rt.lastKey, rt.lastHost)
	}
	// Must hit the versioned search endpoint.
	if !strings.Contains(rt.lastURL, "/search-v2") {
		t.Errorf("request URL = %q, want the /search-v2 endpoint", rt.lastURL)
	}
}

func TestJSearchNonOKStatusIsError(t *testing.T) {
	j := NewJSearch("k")
	j.http = &fakeRoundTripper{body: "forbidden", status: http.StatusForbidden}
	if _, err := j.Discover(context.Background(), Query{}); err == nil {
		t.Error("expected error on non-200 status")
	}
}
