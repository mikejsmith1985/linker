package jobsource

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const jsearchFixture = `{
  "status": "OK",
  "data": {
    "jobs": [
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
        "job_is_remote": true,
        "job_location": "Anywhere",
        "job_min_salary": 60,
        "job_max_salary": 80,
        "job_salary_period": "HOUR"
      }
    ],
    "cursor": "abc"
  }
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
	// job_location is used when city/state/country are absent (remote roles).
	if out[1].Location != "Anywhere" {
		t.Errorf("Location = %q, want Anywhere (job_location fallback)", out[1].Location)
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
	if _, err := j.Discover(context.Background(), Query{Keywords: []string{"engineer"}}); err == nil {
		t.Error("expected error on non-200 status")
	}
}

func TestJSearchDetectsHybridFromTextOverRemoteFlag(t *testing.T) {
	// JSearch marks this remote (2 days remote), but the text says Hybrid — the
	// text must win so a remote-only preference correctly gates it.
	body := `{"data":{"jobs":[
	  {"job_title":"Senior Scrum Master","employer_name":"Acme","job_apply_link":"https://x",
	   "job_description":"Location: McLean, VA (Hybrid 3 days on site 2 days remote per week)",
	   "job_is_remote":true,"job_city":"McLean","job_state":"VA","job_country":"US"}
	]}}`
	j := NewJSearch("k")
	j.http = &fakeRoundTripper{body: body}

	out, err := j.Discover(context.Background(), Query{Keywords: []string{"scrum"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d openings, want 1", len(out))
	}
	if out[0].WorkLocationType != "hybrid" {
		t.Errorf("WorkLocationType = %q, want hybrid (text over job_is_remote flag)", out[0].WorkLocationType)
	}
}

func TestJSearchPrefersDirectApplyLink(t *testing.T) {
	body := `{"data":{"jobs":[
	  {"job_title":"Scrum Master","employer_name":"Acme","job_apply_link":"https://monster.com/redirect",
	   "job_apply_is_direct":false,
	   "apply_options":[{"publisher":"Monster","apply_link":"https://monster.com/redirect","is_direct":false},
	                    {"publisher":"Acme Careers","apply_link":"https://acme.com/careers/1","is_direct":true}],
	   "job_is_remote":true}
	]}}`
	j := NewJSearch("k")
	j.http = &fakeRoundTripper{body: body}
	out, err := j.Discover(context.Background(), Query{Keywords: []string{"scrum"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if out[0].OriginalURL != "https://acme.com/careers/1" {
		t.Errorf("OriginalURL = %q, want the direct company link", out[0].OriginalURL)
	}
}

func TestJSearchNotRemoteDefaultsToOnsite(t *testing.T) {
	// job_is_remote=false and the description has no remote/onsite keyword the
	// detector recognizes ("work in our Winchester, VA office") — must NOT be
	// left "unknown" (which a remote-only filter would let through).
	body := `{"data":{"jobs":[
	  {"job_title":"Release Train Engineer Senior","employer_name":"ECS","job_apply_link":"https://x",
	   "job_description":"ECS is seeking a Release Train Engineer Senior to work in our Winchester, VA office. Manages the flow of value through the ART.",
	   "job_is_remote":false,"job_city":"Winchester","job_state":"VA","job_country":"US"}
	]}}`
	j := NewJSearch("k")
	j.http = &fakeRoundTripper{body: body}
	out, err := j.Discover(context.Background(), Query{Keywords: []string{"engineer"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if out[0].WorkLocationType != "onsite" {
		t.Errorf("WorkLocationType = %q, want onsite (not-remote must not stay unknown)", out[0].WorkLocationType)
	}
}
