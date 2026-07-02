# Contract: `internal/jobsource`

Discovery adapters behind one interface. The orchestrator depends only on this interface, so sources plug in without touching scoring (FR-003, FR-021, FR-022).

## Interface

```go
// Source discovers job openings for a given query. Implementations must be
// safe to call concurrently and must never submit anything to an employer.
type Source interface {
    // Name returns the stable adapter identifier recorded in source_health.
    Name() string

    // Discover returns raw openings matching the query. It must return a
    // typed error rather than panicking on network/rate-limit failure, so the
    // orchestrator can record per-source health (FR-016, SC-008).
    Discover(ctx context.Context, query Query) ([]RawOpening, error)
}

type Query struct {
    Keywords          []string // derived from the resume's structured profile
    Location          string
    WorkLocationPref  string   // onsite | hybrid | remote
    RequiredSalaryMin int      // 0 = unset
}

type RawOpening struct {
    Title, Employer, Location, Description, OriginalURL string
    WorkLocationType                                    string // onsite|hybrid|remote|unknown
    SalaryMin, SalaryMax                                int    // 0 = unstated
    SourceName                                          string
}
```

## Adapters

| Adapter | Behavior | Contract notes |
|---------|----------|----------------|
| `aggregator` (Adzuna default) | Calls compliant JSON API | API key from env/vault; maps API salary/location into `RawOpening`; returns `no_results` not an error when the API responds with an empty set |
| `urlpaste` | `Discover` ignores `Query`; instead fetches user-supplied URLs and parses each into one `RawOpening` (FR-021) | A non-job or unfetchable URL yields a per-URL error surfaced to the user; other URLs still return (edge case) |
| `browser` (Playwright) | Automates non-API sources incl. LinkedIn (FR-022) | MUST refuse to run unless `SearchPreferences.browser_automation_ack` is true (FR-023); returns an `ErrAcknowledgmentRequired` sentinel otherwise |

## De-duplication (registry responsibility, not per-adapter)

After collecting `[]RawOpening` from all enabled sources, the registry computes `canonical_key = normalize(employer)+"|"+normalize(title)+"|"+normalize(location)` and merges duplicates, keeping the richest record and appending extra `source_names` (FR-014, SC-007).

## Contract tests

- `aggregator.Discover` maps a known API JSON fixture to expected `RawOpening` fields (integration, real or recorded HTTP).
- `browser.Discover` returns `ErrAcknowledgmentRequired` when ack flag is false (unit, <10ms).
- Registry de-dup: two `RawOpening`s with same employer/title/location collapse to one with both source names (unit).
- A source returning an error is recorded as `failed` in health and does not abort the search (unit).
