// Package github polls GitHub repositories for post-worthy activity — new
// commits, releases, and README changes — and turns them into linker events.
//
// The GitHub REST calls sit behind the small ghAPI interface so the diffing
// logic (what counts as "new", how the cursor advances, first-run baselining)
// is unit-tested with a fake and no network.
package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mikejsmith1985/linker/internal/store"

	gh "github.com/google/go-github/v66/github"
	"golang.org/x/oauth2"
)

// Result is everything Poll learned about a repo in one tick.
type Result struct {
	Events          []store.Event
	Cursor          store.Cursor
	RepoDescription string
	ReadmeExcerpt   string
}

// ActivitySource polls a single repo given its last-seen cursor.
type ActivitySource interface {
	Poll(ctx context.Context, repo string, cur store.Cursor) (Result, error)
}

// ---- internal GitHub API surface (faked in tests) ----

type commitInfo struct {
	SHA     string
	Message string
	URL     string
}

type releaseInfo struct {
	Tag  string
	Name string
	Body string
	URL  string
}

type ghAPI interface {
	listCommits(ctx context.Context, owner, repo string) ([]commitInfo, error)
	latestRelease(ctx context.Context, owner, repo string) (*releaseInfo, error)
	readme(ctx context.Context, owner, repo string) (string, error)
	description(ctx context.Context, owner, repo string) (string, error)
}

// Source is the GitHub-backed ActivitySource.
type Source struct {
	api            ghAPI
	maxNewCommits  int
	readmeExcerpts int
}

const (
	defaultMaxNewCommits  = 5
	defaultReadmeExcerpt  = 600
	readmeHashPlaceholder = ""
)

// New builds a Source from a GitHub token (may be empty for public,
// rate-limited access).
func New(token string) *Source {
	return &Source{
		api:            newRESTClient(token),
		maxNewCommits:  defaultMaxNewCommits,
		readmeExcerpts: defaultReadmeExcerpt,
	}
}

// Poll diffs a repo against its cursor and returns new events plus the advanced
// cursor. On the first observation of a repo (zero cursor) it establishes a
// baseline and emits no events, so booting linker doesn't post about old work.
func (s *Source) Poll(ctx context.Context, repo string, cur store.Cursor) (Result, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return Result{}, err
	}

	res := Result{Cursor: cur}
	res.Cursor.Repo = repo

	// Repo description (best-effort; non-fatal).
	if desc, err := s.api.description(ctx, owner, name); err == nil {
		res.RepoDescription = desc
	}

	// README: fetch once, use for both excerpt and change detection.
	readme, err := s.api.readme(ctx, owner, name)
	if err == nil && readme != "" {
		res.ReadmeExcerpt = excerpt(readme, s.readmeExcerpts)
		hash := hashString(readme)
		switch {
		case cur.ReadmeHash == "": // baseline
			res.Cursor.ReadmeHash = hash
		case cur.ReadmeHash != hash:
			res.Events = append(res.Events, store.Event{
				Repo: repo, Type: store.EventReadme, Ref: hash,
				Title: "Updated the project README",
				Body:  res.ReadmeExcerpt,
				URL:   fmt.Sprintf("https://github.com/%s/%s#readme", owner, name),
			})
			res.Cursor.ReadmeHash = hash
		}
	}

	// Commits.
	commits, err := s.api.listCommits(ctx, owner, name)
	if err != nil {
		return Result{}, fmt.Errorf("list commits for %s: %w", repo, err)
	}
	if len(commits) > 0 {
		if cur.LastCommitSHA == "" {
			res.Cursor.LastCommitSHA = commits[0].SHA // baseline only
		} else {
			res.Events = append(res.Events, s.newCommitEvents(repo, commits, cur.LastCommitSHA)...)
			res.Cursor.LastCommitSHA = commits[0].SHA
		}
	}

	// Latest release.
	rel, err := s.api.latestRelease(ctx, owner, name)
	if err != nil {
		return Result{}, fmt.Errorf("latest release for %s: %w", repo, err)
	}
	if rel != nil && rel.Tag != "" {
		switch {
		case cur.LastReleaseTag == "":
			res.Cursor.LastReleaseTag = rel.Tag // baseline
		case cur.LastReleaseTag != rel.Tag:
			res.Events = append(res.Events, store.Event{
				Repo: repo, Type: store.EventRelease, Ref: rel.Tag,
				Title: releaseTitle(rel),
				Body:  rel.Body,
				URL:   rel.URL,
			})
			res.Cursor.LastReleaseTag = rel.Tag
		}
	}

	return res, nil
}

// newCommitEvents walks commits (newest first) until it reaches the last-seen
// sha, skipping merge commits, capped at maxNewCommits.
func (s *Source) newCommitEvents(repo string, commits []commitInfo, lastSHA string) []store.Event {
	var events []store.Event
	for _, c := range commits {
		if c.SHA == lastSHA {
			break
		}
		if isMergeCommit(c.Message) {
			continue
		}
		events = append(events, store.Event{
			Repo: repo, Type: store.EventCommit, Ref: c.SHA,
			Title: firstLine(c.Message),
			Body:  c.Message,
			URL:   c.URL,
		})
		if len(events) >= s.maxNewCommits {
			break
		}
	}
	return events
}

func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(strings.TrimSpace(repo), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo %q, want owner/name", repo)
	}
	return parts[0], parts[1], nil
}

func firstLine(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return strings.TrimSpace(msg[:i])
	}
	return strings.TrimSpace(msg)
}

func isMergeCommit(msg string) bool {
	return strings.HasPrefix(strings.TrimSpace(msg), "Merge ")
}

func releaseTitle(r *releaseInfo) string {
	if strings.TrimSpace(r.Name) != "" {
		return "Released " + r.Name
	}
	return "Released " + r.Tag
}

func excerpt(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---- go-github REST implementation ----

type restClient struct {
	c *gh.Client
}

func newRESTClient(token string) *restClient {
	var httpClient *http.Client
	if token != "" {
		httpClient = oauth2.NewClient(context.Background(),
			oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
	} else {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &restClient{c: gh.NewClient(httpClient)}
}

func (r *restClient) listCommits(ctx context.Context, owner, repo string) ([]commitInfo, error) {
	commits, _, err := r.c.Repositories.ListCommits(ctx, owner, repo,
		&gh.CommitsListOptions{ListOptions: gh.ListOptions{PerPage: 30}})
	if err != nil {
		return nil, err
	}
	out := make([]commitInfo, 0, len(commits))
	for _, c := range commits {
		out = append(out, commitInfo{
			SHA:     c.GetSHA(),
			Message: c.GetCommit().GetMessage(),
			URL:     c.GetHTMLURL(),
		})
	}
	return out, nil
}

func (r *restClient) latestRelease(ctx context.Context, owner, repo string) (*releaseInfo, error) {
	rel, resp, err := r.c.Repositories.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, nil // no releases yet
		}
		return nil, err
	}
	return &releaseInfo{
		Tag:  rel.GetTagName(),
		Name: rel.GetName(),
		Body: rel.GetBody(),
		URL:  rel.GetHTMLURL(),
	}, nil
}

func (r *restClient) readme(ctx context.Context, owner, repo string) (string, error) {
	rc, resp, err := r.c.Repositories.GetReadme(ctx, owner, repo, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	return rc.GetContent()
}

func (r *restClient) description(ctx context.Context, owner, repo string) (string, error) {
	repository, _, err := r.c.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	return repository.GetDescription(), nil
}
