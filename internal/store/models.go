// Package store defines linker's domain types and the Postgres-backed
// persistence layer for activity events, repo cursors, and drafted posts.
package store

import "time"

// EventType enumerates the kinds of GitHub activity linker reacts to.
type EventType string

const (
	EventCommit  EventType = "commit"
	EventRelease EventType = "release"
	EventReadme  EventType = "readme"
)

// PostStatus is the lifecycle state of a drafted LinkedIn post.
type PostStatus string

const (
	StatusDraft     PostStatus = "draft"     // generated, awaiting review
	StatusQueued    PostStatus = "queued"    // sent to Buffer (or stub)
	StatusPublished PostStatus = "published" // confirmed live on LinkedIn
	StatusRejected  PostStatus = "rejected"  // dismissed in the dashboard
)

// Event is a single piece of post-worthy GitHub activity. The triple
// (Repo, Type, Ref) is unique and is what de-duplicates re-polled activity.
type Event struct {
	ID         int64
	Repo       string
	Type       EventType
	Ref        string // commit sha, release tag, or README content hash
	Title      string
	Body       string
	URL        string
	DetectedAt time.Time
}

// Post is a Claude-drafted LinkedIn post tied to the event that triggered it.
type Post struct {
	ID         int64
	EventID    int64
	Content    string
	Hashtags   string
	Status     PostStatus
	ExternalID string // Buffer update id once queued
	CreatedAt  time.Time
	UpdatedAt  time.Time
	QueuedAt   *time.Time
}

// PostWithEvent joins a post to its source event for display in the dashboard.
type PostWithEvent struct {
	Post
	Repo       string
	EventType  EventType
	EventTitle string
	EventURL   string
}

// Cursor records, per repo, the last activity linker has already seen so the
// poller only asks GitHub for what is new.
type Cursor struct {
	Repo           string
	LastCommitSHA  string
	LastReleaseTag string
	ReadmeHash     string
}
