// Package jobsource discovers job openings from pluggable sources (a compliant
// aggregator API, user-pasted URLs, and an opt-in browser automation source)
// behind a single interface, and merges their results with de-duplication.
package jobsource

import "context"

// Query is the discovery request derived from the resume and preferences.
type Query struct {
	Keywords          []string
	Location          string
	WorkLocationPref  string
	RequiredSalaryMin int
}

// RawOpening is a posting as returned by one source, before de-duplication.
type RawOpening struct {
	Title            string
	Employer         string
	Location         string
	Description      string
	OriginalURL      string
	WorkLocationType string // onsite | hybrid | remote | unknown
	SalaryMin        int    // 0 = unstated
	SalaryMax        int    // 0 = unstated
	SourceName       string
}

// Source discovers openings for a query. Implementations must be safe for
// concurrent use and must never submit anything to an employer. A network or
// rate-limit failure must be returned as an error (not a panic) so the registry
// can record per-source health.
type Source interface {
	// Name returns the stable adapter identifier recorded in source health.
	Name() string
	// Discover returns raw openings matching the query.
	Discover(ctx context.Context, query Query) ([]RawOpening, error)
}

// Source health values recorded per source per search.
const (
	HealthSucceeded = "succeeded"
	HealthFailed    = "failed"
	HealthNoResults = "no_results"
)
