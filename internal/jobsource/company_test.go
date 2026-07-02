package jobsource

import (
	"context"
	"testing"
)

func TestNormalizeCompany(t *testing.T) {
	cases := map[string]string{"Stripe": "stripe", "Match Group": "matchgroup", "IBM": "ibm", "1Password": "1password"}
	for in, want := range cases {
		if got := normalizeCompany(in); got != want {
			t.Errorf("normalizeCompany(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompanyGreenhouseSuccess(t *testing.T) {
	rt := urlRoundTripper{byURL: map[string]urlResp{
		"https://boards-api.greenhouse.io/v1/boards/stripe/jobs?content=true": {body: `{"jobs":[
		  {"title":"Backend Engineer","absolute_url":"https://boards.greenhouse.io/stripe/jobs/1","content":"&lt;p&gt;Go&lt;/p&gt;","location":{"name":"Remote US"}}
		]}`},
	}}
	src := NewCompanyCareers([]string{"Stripe"})
	src.http = rt

	out, err := src.Discover(context.Background(), Query{Keywords: []string{"engineer"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d openings, want 1", len(out))
	}
	got := out[0]
	if got.Title != "Backend Engineer" || got.Employer != "Stripe" {
		t.Errorf("unexpected mapping: %+v", got)
	}
	if got.SourceName != "company" || got.WorkLocationType != "remote" {
		t.Errorf("source/worklocation: %+v", got)
	}
}

func TestCompanyFallsBackToLever(t *testing.T) {
	rt := urlRoundTripper{byURL: map[string]urlResp{
		// Greenhouse 404s for this token...
		"https://boards-api.greenhouse.io/v1/boards/acme/jobs?content=true": {status: 404},
		// ...so Lever answers.
		"https://api.lever.co/v0/postings/acme?mode=json": {body: `[
		  {"text":"Staff Engineer","hostedUrl":"https://jobs.lever.co/acme/1","descriptionPlain":"Go and gRPC","workplaceType":"remote","categories":{"location":"San Francisco"}}
		]`},
	}}
	src := NewCompanyCareers([]string{"Acme"})
	src.http = rt

	out, err := src.Discover(context.Background(), Query{Keywords: []string{"engineer"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(out) != 1 || out[0].Title != "Staff Engineer" || out[0].OriginalURL != "https://jobs.lever.co/acme/1" {
		t.Fatalf("Lever fallback mapping wrong: %+v", out)
	}
}

func TestCompanyIsolatesUnresolvable(t *testing.T) {
	rt := urlRoundTripper{byURL: map[string]urlResp{
		"https://boards-api.greenhouse.io/v1/boards/stripe/jobs?content=true": {body: `{"jobs":[
		  {"title":"Engineer","absolute_url":"https://x","content":"c","location":{"name":"Remote"}}]}`},
		// "nope" resolves on neither ATS (both 404) — must not abort the batch.
		"https://boards-api.greenhouse.io/v1/boards/nope/jobs?content=true": {status: 404},
		"https://api.lever.co/v0/postings/nope?mode=json":                   {status: 404},
	}}
	src := NewCompanyCareers([]string{"nope", "Stripe"})
	src.http = rt

	out, err := src.Discover(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Discover should not fail when one company resolves: %v", err)
	}
	if len(out) != 1 || out[0].Employer != "Stripe" {
		t.Errorf("expected only the resolvable company, got %+v", out)
	}
}
