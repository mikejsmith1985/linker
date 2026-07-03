package jobsource

import (
	"context"
	"sort"
	"strings"

	"github.com/mikejsmith1985/linker/internal/store"
)

// Registry runs a set of sources and merges their openings, de-duplicating the
// same posting seen on more than one board.
type Registry struct {
	sources []Source
}

// NewRegistry builds a registry from the given sources, in priority order.
func NewRegistry(sources ...Source) *Registry {
	return &Registry{sources: sources}
}

// Discover runs every source, merges and de-duplicates their openings by
// canonical key, and returns the merged openings alongside a per-source health
// map (succeeded / failed / no_results). A failing source never aborts the run.
func (r *Registry) Discover(ctx context.Context, query Query) ([]store.JobOpening, map[string]string) {
	health := make(map[string]string, len(r.sources))
	byKey := make(map[string]*store.JobOpening)
	var order []string // canonical keys in first-seen order

	for _, src := range r.sources {
		raws, err := src.Discover(ctx, query)
		switch {
		case err != nil:
			health[src.Name()] = HealthFailed
			continue
		case len(raws) == 0:
			health[src.Name()] = HealthNoResults
			continue
		default:
			health[src.Name()] = HealthSucceeded
		}
		for _, raw := range raws {
			key := CanonicalKey(raw.Employer, raw.Title, raw.Location)
			if existing, ok := byKey[key]; ok {
				mergeInto(existing, raw)
				continue
			}
			opening := toOpening(raw, key)
			byKey[key] = &opening
			order = append(order, key)
		}
	}

	out := make([]store.JobOpening, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out, health
}

// CanonicalKey builds the de-duplication key for an opening from its employer,
// title, and location, normalized so trivial formatting differences collapse.
func CanonicalKey(employer, title, location string) string {
	return normalize(employer) + "|" + normalize(title) + "|" + normalize(location)
}

func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// toOpening converts a raw opening into a domain opening with its canonical key.
// All text is sanitized to valid UTF-8 so a stray byte from a source never
// breaks the (UTF8) database insert.
func toOpening(raw RawOpening, key string) store.JobOpening {
	return store.JobOpening{
		CanonicalKey:     key,
		Title:            cleanUTF8(raw.Title),
		Employer:         cleanUTF8(raw.Employer),
		Location:         cleanUTF8(raw.Location),
		WorkLocationType: workLocation(raw.WorkLocationType),
		SalaryMin:        raw.SalaryMin,
		SalaryMax:        raw.SalaryMax,
		Description:      cleanUTF8(raw.Description),
		SourceNames:      []string{raw.SourceName},
		OriginalURL:      cleanUTF8(raw.OriginalURL),
		EmployerWebsite:  cleanUTF8(raw.EmployerWebsite),
	}
}

// cleanUTF8 strips invalid UTF-8 byte sequences and null bytes that Postgres
// text columns reject.
func cleanUTF8(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, ""), "\x00", "")
}

// mergeInto folds a duplicate raw opening into an already-seen opening, keeping
// the richer data (a stated salary and a URL win) and recording the extra
// source name.
func mergeInto(existing *store.JobOpening, raw RawOpening) {
	if !containsString(existing.SourceNames, raw.SourceName) && raw.SourceName != "" {
		existing.SourceNames = append(existing.SourceNames, raw.SourceName)
		sort.Strings(existing.SourceNames)
	}
	if existing.SalaryMax == 0 && raw.SalaryMax > 0 {
		existing.SalaryMin, existing.SalaryMax = raw.SalaryMin, raw.SalaryMax
	}
	if existing.OriginalURL == "" && raw.OriginalURL != "" {
		existing.OriginalURL = raw.OriginalURL
	}
	if existing.Description == "" && raw.Description != "" {
		existing.Description = raw.Description
	}
	if existing.WorkLocationType == store.WorkUnknown && raw.WorkLocationType != "" {
		existing.WorkLocationType = workLocation(raw.WorkLocationType)
	}
}

func workLocation(v string) store.WorkLocation {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "onsite":
		return store.WorkOnsite
	case "hybrid":
		return store.WorkHybrid
	case "remote":
		return store.WorkRemote
	default:
		return store.WorkUnknown
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
