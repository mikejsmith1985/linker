package github

import (
	"context"
	"testing"

	"github.com/mikejsmith1985/linker/internal/store"
)

type fakeAPI struct {
	commits       []commitInfo
	release       *releaseInfo
	readmeContent string
	desc          string
}

func (f *fakeAPI) listCommits(_ context.Context, _, _ string) ([]commitInfo, error) {
	return f.commits, nil
}
func (f *fakeAPI) latestRelease(_ context.Context, _, _ string) (*releaseInfo, error) {
	return f.release, nil
}
func (f *fakeAPI) readme(_ context.Context, _, _ string) (string, error) {
	return f.readmeContent, nil
}
func (f *fakeAPI) description(_ context.Context, _, _ string) (string, error) {
	return f.desc, nil
}

func newSource(api ghAPI) *Source {
	return &Source{api: api, maxNewCommits: 5, readmeExcerpts: 600}
}

func TestPollFirstRunBaselinesNoEvents(t *testing.T) {
	api := &fakeAPI{
		commits:       []commitInfo{{SHA: "c2", Message: "newer"}, {SHA: "c1", Message: "older"}},
		release:       &releaseInfo{Tag: "v1.0", Name: "First"},
		readmeContent: "# Project\nDescription here",
		desc:          "an AI delivery tool",
	}
	res, err := newSource(api).Poll(context.Background(), "me/repo", store.Cursor{})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(res.Events) != 0 {
		t.Fatalf("first run should emit no events, got %d", len(res.Events))
	}
	if res.Cursor.LastCommitSHA != "c2" {
		t.Errorf("commit baseline = %q, want c2", res.Cursor.LastCommitSHA)
	}
	if res.Cursor.LastReleaseTag != "v1.0" {
		t.Errorf("release baseline = %q, want v1.0", res.Cursor.LastReleaseTag)
	}
	if res.Cursor.ReadmeHash == "" {
		t.Error("readme hash baseline not set")
	}
	if res.RepoDescription != "an AI delivery tool" {
		t.Errorf("description = %q", res.RepoDescription)
	}
}

func TestPollNewCommitsSkipMergeAndCap(t *testing.T) {
	api := &fakeAPI{
		commits: []commitInfo{
			{SHA: "c5", Message: "feat: add dashboard\n\nbody"},
			{SHA: "c4", Message: "Merge pull request #3"},
			{SHA: "c3", Message: "fix: cadence guard"},
			{SHA: "old", Message: "older"},
		},
	}
	cur := store.Cursor{LastCommitSHA: "old", LastReleaseTag: "v1", ReadmeHash: "h"}
	res, err := newSource(api).Poll(context.Background(), "me/repo", cur)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("got %d events, want 2 (merge skipped, stop at old)", len(res.Events))
	}
	if res.Events[0].Ref != "c5" || res.Events[0].Title != "feat: add dashboard" {
		t.Errorf("first event unexpected: %+v", res.Events[0])
	}
	if res.Events[1].Ref != "c3" {
		t.Errorf("second event ref = %q, want c3", res.Events[1].Ref)
	}
	if res.Cursor.LastCommitSHA != "c5" {
		t.Errorf("cursor advanced to %q, want c5", res.Cursor.LastCommitSHA)
	}
}

func TestPollMaxNewCommitsCap(t *testing.T) {
	var commits []commitInfo
	for i := 0; i < 10; i++ {
		commits = append(commits, commitInfo{SHA: string(rune('a' + i)), Message: "feat: x"})
	}
	api := &fakeAPI{commits: commits}
	s := &Source{api: api, maxNewCommits: 3, readmeExcerpts: 600}
	// cursor sha that never matches -> all are "new", but capped at 3
	res, err := s.Poll(context.Background(), "me/repo", store.Cursor{LastCommitSHA: "zzz", ReadmeHash: "h", LastReleaseTag: "v1"})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(res.Events) != 3 {
		t.Fatalf("got %d events, want cap of 3", len(res.Events))
	}
}

func TestPollReadmeChange(t *testing.T) {
	api := &fakeAPI{readmeContent: "new readme content"}
	cur := store.Cursor{LastCommitSHA: "x", LastReleaseTag: "v1", ReadmeHash: "stalehash"}
	res, err := newSource(api).Poll(context.Background(), "me/repo", cur)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(res.Events) != 1 || res.Events[0].Type != store.EventReadme {
		t.Fatalf("expected one readme event, got %+v", res.Events)
	}
	if res.Cursor.ReadmeHash == "stalehash" {
		t.Error("readme hash not advanced")
	}
}

func TestPollReleaseChange(t *testing.T) {
	api := &fakeAPI{release: &releaseInfo{Tag: "v2.0", Name: "Big", Body: "notes", URL: "http://x"}}
	cur := store.Cursor{LastCommitSHA: "x", LastReleaseTag: "v1.0", ReadmeHash: "h"}
	res, err := newSource(api).Poll(context.Background(), "me/repo", cur)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(res.Events) != 1 || res.Events[0].Type != store.EventRelease {
		t.Fatalf("expected one release event, got %+v", res.Events)
	}
	if res.Events[0].Title != "Released Big" {
		t.Errorf("title = %q", res.Events[0].Title)
	}
	if res.Cursor.LastReleaseTag != "v2.0" {
		t.Errorf("release cursor = %q, want v2.0", res.Cursor.LastReleaseTag)
	}
}

func TestPollInvalidRepo(t *testing.T) {
	if _, err := newSource(&fakeAPI{}).Poll(context.Background(), "noslash", store.Cursor{}); err == nil {
		t.Fatal("expected error for invalid repo")
	}
}
