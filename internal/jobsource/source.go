// Package jobsource discovers job openings from pluggable sources (a compliant
// aggregator API, user-pasted URLs, and an opt-in browser automation source)
// behind a single interface, and merges their results with de-duplication.
package jobsource

import (
	"context"
	"strings"
)

// Query is the discovery request derived from the resume and preferences.
type Query struct {
	// Keywords are skill terms used for client-side relevance filtering.
	Keywords []string
	// RoleTitles are target job titles used as distinct search queries, which
	// yield far more relevant results than a dump of skills.
	RoleTitles        []string
	Location          string
	WorkLocationPref  string
	RequiredSalaryMin int
}

// maxSearchTerms caps how many role-title queries a search-based source runs, to
// bound API usage.
const maxSearchTerms = 3

// SearchTerms returns the distinct query strings a search-based source should run
// — the target role titles when available, otherwise the joined skill keywords.
func (q Query) SearchTerms() []string {
	if len(q.RoleTitles) > 0 {
		terms := q.RoleTitles
		if len(terms) > maxSearchTerms {
			terms = terms[:maxSearchTerms]
		}
		return terms
	}
	joined := strings.TrimSpace(strings.Join(q.Keywords, " "))
	if joined == "" {
		return nil
	}
	return []string{joined}
}

// FilterKeywords returns the terms a client-filtered source matches against —
// both skills and role titles, so relevant roles are kept.
func (q Query) FilterKeywords() []string {
	return append(append([]string{}, q.Keywords...), q.RoleTitles...)
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

// defaultSourceCap bounds how many openings a keyword-filtered source returns, so
// a single search does not fan out into hundreds of expensive scoring calls.
const defaultSourceCap = 15

// anyContains reports whether the blob contains any of the substrings.
func anyContains(blob string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(blob, sub) {
			return true
		}
	}
	return false
}

// matchesAnyKeyword reports whether any keyword appears in the text
// (case-insensitive). With no keywords it matches everything.
func matchesAnyKeyword(text string, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	lower := strings.ToLower(text)
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

// filterAndCap keeps openings that match the query's keywords, up to cap. Sources
// whose upstream API has no server-side search use this to stay relevant and
// bounded.
func filterAndCap(openings []RawOpening, keywords []string, cap int) []RawOpening {
	out := make([]RawOpening, 0, cap)
	for _, opening := range openings {
		if matchesAnyKeyword(opening.Title+" "+opening.Description, keywords) {
			out = append(out, opening)
			if len(out) >= cap {
				break
			}
		}
	}
	return out
}
