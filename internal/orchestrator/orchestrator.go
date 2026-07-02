// Package orchestrator runs a search end to end: discover openings from the
// enabled sources, de-duplicate them, score each against the active resume and
// preferences, persist the results, and eagerly generate documents for the
// top-scoring qualifying openings. Searches are on-demand; the same entrypoint
// can later be driven on a schedule without changing this core.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/mikejsmith1985/linker/internal/jobsource"
	"github.com/mikejsmith1985/linker/internal/scoring"
	"github.com/mikejsmith1985/linker/internal/store"
)

// Discoverer runs the enabled sources and returns merged, de-duplicated openings
// plus per-source health. *jobsource.Registry satisfies it.
type Discoverer interface {
	Discover(ctx context.Context, query jobsource.Query) ([]store.JobOpening, map[string]string)
}

// Scorer rates one opening against the resume profile and preferences.
type Scorer interface {
	Score(ctx context.Context, profile string, opening store.JobOpening, prefs store.Preferences) (scoring.Score, error)
}

// DocGenerator eagerly generates documents for a qualifying match. It is
// optional: when nil, the orchestrator skips eager generation (documents are
// then produced on demand by the web layer).
type DocGenerator interface {
	GenerateFor(ctx context.Context, matchID int64, opening store.JobOpening, resumeFacts string) error
}

// URLDiscovererFactory builds a discoverer for a set of user-pasted posting URLs.
type URLDiscovererFactory func(urls []string) Discoverer

// Orchestrator wires discovery, scoring, persistence, and document generation.
type Orchestrator struct {
	store   store.Store
	disc    Discoverer
	score   Scorer
	docs    DocGenerator
	urlDisc URLDiscovererFactory
	log     *slog.Logger
}

// New builds an orchestrator. docs may be nil to disable eager generation;
// urlDisc may be nil to disable paste-a-URL search.
func New(st store.Store, disc Discoverer, score Scorer, docs DocGenerator, urlDisc URLDiscovererFactory, log *slog.Logger) *Orchestrator {
	if log == nil {
		log = slog.Default()
	}
	return &Orchestrator{store: st, disc: disc, score: score, docs: docs, urlDisc: urlDisc, log: log}
}

// ErrNoResume is returned when a search is started without an active resume.
var ErrNoResume = errors.New("no active resume; upload a resume first")

// scored pairs an opening (already persisted) with its computed score.
type scored struct {
	openingID int64
	opening   store.JobOpening
	result    scoring.Score
}

// ErrURLSearchUnavailable is returned when paste-a-URL search is not configured.
var ErrURLSearchUnavailable = errors.New("paste-a-URL search is not available")

// RunSearch performs one on-demand search over the configured sources.
func (o *Orchestrator) RunSearch(ctx context.Context) (int64, error) {
	return o.runWith(ctx, o.disc)
}

// RunSearchURLs scores a set of user-pasted posting URLs like a discovered
// search (FR-021).
func (o *Orchestrator) RunSearchURLs(ctx context.Context, urls []string) (int64, error) {
	if o.urlDisc == nil {
		return 0, ErrURLSearchUnavailable
	}
	return o.runWith(ctx, o.urlDisc(urls))
}

// runWith performs one on-demand search using the given discoverer and returns
// the new search id.
func (o *Orchestrator) runWith(ctx context.Context, disc Discoverer) (int64, error) {
	resume, err := o.store.GetActiveResume(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return 0, ErrNoResume
	}
	if err != nil {
		return 0, fmt.Errorf("load resume: %w", err)
	}
	prefs, err := o.store.GetPreferences(ctx)
	if err != nil {
		return 0, fmt.Errorf("load preferences: %w", err)
	}

	searchID, err := o.store.CreateSearch(ctx, resume.ID, prefs)
	if err != nil {
		return 0, fmt.Errorf("create search: %w", err)
	}

	openings, health := disc.Discover(ctx, buildQuery(resume, prefs))

	scoredList, err := o.scoreAll(ctx, openings, resume, prefs)
	if err != nil {
		_ = o.store.FinishSearch(ctx, searchID, store.SearchFailed, health)
		return 0, err
	}

	// Rank by descending score, then persist match results with their rank.
	sort.SliceStable(scoredList, func(i, j int) bool {
		return scoredList[i].result.Value > scoredList[j].result.Value
	})
	if err := o.persistResults(ctx, searchID, scoredList, resume); err != nil {
		_ = o.store.FinishSearch(ctx, searchID, store.SearchFailed, health)
		return 0, err
	}

	if err := o.store.FinishSearch(ctx, searchID, store.SearchCompleted, health); err != nil {
		return 0, fmt.Errorf("finish search: %w", err)
	}
	return searchID, nil
}

// scoreAll persists each opening and computes its score, reusing a prior score
// for an identical posting (same canonical key) rather than re-scoring (FR-025).
func (o *Orchestrator) scoreAll(ctx context.Context, openings []store.JobOpening, resume store.Resume, prefs store.Preferences) ([]scored, error) {
	out := make([]scored, 0, len(openings))
	for _, opening := range openings {
		openingID, err := o.store.UpsertOpening(ctx, opening)
		if err != nil {
			return nil, fmt.Errorf("persist opening: %w", err)
		}

		result, reused := o.reusePriorScore(ctx, opening)
		if !reused {
			result, err = o.score.Score(ctx, resume.StructuredProfile, opening, prefs)
			if err != nil {
				return nil, fmt.Errorf("score opening %q: %w", opening.Title, err)
			}
		}
		out = append(out, scored{openingID: openingID, opening: opening, result: result})
	}
	return out, nil
}

// reusePriorScore returns a previously computed score for the same posting, if
// one exists, so re-runs do not re-score identical openings (FR-025).
func (o *Orchestrator) reusePriorScore(ctx context.Context, opening store.JobOpening) (scoring.Score, bool) {
	prior, err := o.store.FindScoredOpening(ctx, opening.CanonicalKey)
	if err != nil {
		return scoring.Score{}, false
	}
	return scoring.Score{
		Value:         prior.Score,
		Explanation:   prior.ScoreExplanation,
		GatePenalties: prior.GatePenalties,
		IsQualifying:  prior.IsQualifying,
	}, true
}

// persistResults writes a match result per opening and eagerly generates
// documents for the top-N qualifying openings.
func (o *Orchestrator) persistResults(ctx context.Context, searchID int64, scoredList []scored, resume store.Resume) error {
	qualifyingRank := 0
	for i, sc := range scoredList {
		matchID, err := o.store.CreateMatchResult(ctx, store.MatchResult{
			SearchID:         searchID,
			JobOpeningID:     sc.openingID,
			Score:            sc.result.Value,
			ScoreExplanation: sc.result.Explanation,
			GatePenalties:    sc.result.GatePenalties,
			IsQualifying:     sc.result.IsQualifying,
			Rank:             i + 1,
		})
		if err != nil {
			return fmt.Errorf("persist match result: %w", err)
		}
		if sc.result.IsQualifying {
			qualifyingRank++
			o.maybeEagerGenerate(ctx, qualifyingRank, matchID, sc.opening, resume.RawText)
		}
	}
	return nil
}

// maybeEagerGenerate generates documents for the top-N qualifying openings when
// a document generator is configured. Generation failure is logged, not fatal.
func (o *Orchestrator) maybeEagerGenerate(ctx context.Context, qualifyingRank int, matchID int64, opening store.JobOpening, resumeFacts string) {
	if o.docs == nil || qualifyingRank > scoring.EagerDocumentTopN {
		return
	}
	if err := o.docs.GenerateFor(ctx, matchID, opening, resumeFacts); err != nil {
		o.log.Error("eager document generation failed", "match_id", matchID, "err", err)
	}
}

// buildQuery derives a discovery query from the resume profile and preferences.
func buildQuery(resume store.Resume, prefs store.Preferences) jobsource.Query {
	return jobsource.Query{
		Keywords:          extractKeywords(resume.StructuredProfile),
		WorkLocationPref:  string(prefs.WorkLocationPref),
		RequiredSalaryMin: prefs.RequiredSalaryMin,
	}
}

// extractKeywords pulls skill keywords from the structured profile's "Skills:"
// line, falling back to the first several words of the profile. A tight set
// keeps the source query focused so results stay relevant rather than a broad
// grab-bag.
func extractKeywords(profile string) []string {
	const maxKeywords = 4
	for _, line := range strings.Split(profile, "\n") {
		if rest, ok := cutFold(strings.TrimSpace(line), "skills:"); ok {
			return topN(splitAndTrim(rest, ","), maxKeywords)
		}
	}
	return topN(strings.Fields(profile), maxKeywords)
}

func cutFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return strings.TrimSpace(s[len(prefix):]), true
	}
	return "", false
}

func splitAndTrim(s, sep string) []string {
	var out []string
	for _, part := range strings.Split(s, sep) {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func topN(in []string, n int) []string {
	if len(in) > n {
		return in[:n]
	}
	return in
}
