package jobsource

import (
	"context"
	"testing"
)

func TestArbeitnowMapsRemoteFlag(t *testing.T) {
	body := `{"data":[
	  {"title":"Backend Engineer","company_name":"Acme","location":"Berlin","description":"<p>Go and Kubernetes</p>","remote":true,"url":"https://arbeitnow.com/1"},
	  {"title":"Office Manager","company_name":"Beta","location":"Berlin","description":"<p>Admin</p>","remote":false,"url":"https://arbeitnow.com/2"}
	]}`
	src := NewArbeitnow()
	src.http = &fakeRoundTripper{body: body}

	out, err := src.Discover(context.Background(), Query{Keywords: []string{"engineer", "kubernetes"}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(out) != 1 || out[0].Title != "Backend Engineer" {
		t.Fatalf("expected the engineering role only, got %+v", out)
	}
	if out[0].WorkLocationType != "remote" {
		t.Errorf("remote flag not mapped: %+v", out[0])
	}
}
