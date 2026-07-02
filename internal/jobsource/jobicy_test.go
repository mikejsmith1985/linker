package jobsource

import (
	"context"
	"testing"
)

func TestJobicyMapsResults(t *testing.T) {
	body := `{"jobs":[
	  {"jobTitle":"Senior Go Developer","companyName":"Acme","jobGeo":"USA","jobDescription":"<p>Go microservices</p>","url":"https://jobicy.com/1"}
	]}`
	src := NewJobicy()
	src.http = &fakeRoundTripper{body: body}

	out, err := src.Discover(context.Background(), Query{Keywords: []string{"go"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(out) != 1 || out[0].Employer != "Acme" || out[0].Location != "USA" {
		t.Fatalf("unexpected mapping: %+v", out)
	}
	if out[0].SourceName != "jobicy" || out[0].WorkLocationType != "remote" {
		t.Errorf("source/remote not set: %+v", out[0])
	}
}
