package jobsource

import (
	"context"
	"testing"
)

func TestRemoteOKMapsAndFilters(t *testing.T) {
	body := `[
	  {"legal": "See remoteok.com/api for terms"},
	  {"position":"Senior Go Engineer","company":"Acme","location":"Remote","description":"<p>Build in Go</p>","url":"https://remoteok.com/l/1","salary_min":140000,"salary_max":180000},
	  {"position":"Marketing Manager","company":"Beta","location":"Remote","description":"<p>Campaigns</p>","url":"https://remoteok.com/l/2"}
	]`
	src := NewRemoteOK()
	src.http = &fakeRoundTripper{body: body}

	out, err := src.Discover(context.Background(), Query{Keywords: []string{"go", "engineer"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d openings, want 1 (keyword-filtered, legal element skipped)", len(out))
	}
	got := out[0]
	if got.Title != "Senior Go Engineer" || got.Employer != "Acme" || got.SalaryMax != 180000 {
		t.Errorf("unexpected mapping: %+v", got)
	}
	if got.WorkLocationType != "remote" || got.SourceName != "remoteok" {
		t.Errorf("remote/source not set: %+v", got)
	}
}
