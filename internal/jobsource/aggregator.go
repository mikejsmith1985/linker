package jobsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// httpDoer is the slice of *http.Client the adapter needs, so tests can inject a
// fake transport.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adzuna is the default compliant aggregator source, backed by the Adzuna Jobs
// API (https://developer.adzuna.com).
type Adzuna struct {
	appID   string
	appKey  string
	country string
	perPage int
	http    httpDoer
	baseURL string
}

// NewAdzuna builds an Adzuna source from API credentials.
func NewAdzuna(appID, appKey string) *Adzuna {
	return &Adzuna{
		appID:   appID,
		appKey:  appKey,
		country: "us",
		perPage: 25,
		http:    &http.Client{},
		baseURL: "https://api.adzuna.com/v1/api",
	}
}

// Name identifies this source in health reporting.
func (a *Adzuna) Name() string { return "adzuna" }

// adzunaResponse mirrors the fields of the Adzuna search response we use.
type adzunaResponse struct {
	Results []struct {
		Title       string  `json:"title"`
		Description string  `json:"description"`
		RedirectURL string  `json:"redirect_url"`
		SalaryMin   float64 `json:"salary_min"`
		SalaryMax   float64 `json:"salary_max"`
		Company     struct {
			DisplayName string `json:"display_name"`
		} `json:"company"`
		Location struct {
			DisplayName string `json:"display_name"`
		} `json:"location"`
	} `json:"results"`
}

// Discover runs each target-role query against Adzuna and merges the results.
// Duplicates are collapsed later by the registry.
func (a *Adzuna) Discover(ctx context.Context, query Query) ([]RawOpening, error) {
	var openings []RawOpening
	var lastErr error
	for _, term := range query.SearchTerms() {
		batch, err := a.searchOne(ctx, term, query)
		if err != nil {
			lastErr = err
			continue
		}
		openings = append(openings, batch...)
	}
	if len(openings) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return openings, nil
}

func (a *Adzuna) searchOne(ctx context.Context, term string, query Query) ([]RawOpening, error) {
	endpoint := fmt.Sprintf("%s/jobs/%s/search/1", a.baseURL, a.country)
	params := url.Values{}
	params.Set("app_id", a.appID)
	params.Set("app_key", a.appKey)
	params.Set("results_per_page", strconv.Itoa(a.perPage))
	params.Set("what", term)
	if query.Location != "" {
		params.Set("where", query.Location)
	}
	if query.RequiredSalaryMin > 0 {
		params.Set("salary_min", strconv.Itoa(query.RequiredSalaryMin))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("adzuna request: %w", err)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adzuna call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("adzuna status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed adzunaResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("adzuna decode: %w", err)
	}

	openings := make([]RawOpening, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		openings = append(openings, RawOpening{
			Title:            r.Title,
			Employer:         r.Company.DisplayName,
			Location:         r.Location.DisplayName,
			Description:      r.Description,
			OriginalURL:      r.RedirectURL,
			WorkLocationType: inferWorkLocation(r.Title, r.Description, r.Location.DisplayName),
			SalaryMin:        int(r.SalaryMin),
			SalaryMax:        int(r.SalaryMax),
			SourceName:       a.Name(),
		})
	}
	return openings, nil
}

// inferWorkLocation guesses onsite/hybrid/remote from free text. It is layered so
// clear signals win: hybrid first (a hybrid posting mentions both remote and on
// site), then an explicit fully-remote marker, then on-site markers, then a plain
// "remote" mention. Unknown when no signal is present.
func inferWorkLocation(parts ...string) string {
	blob := strings.ToLower(strings.Join(parts, " "))
	switch {
	case strings.Contains(blob, "hybrid"):
		return "hybrid"
	case anyContains(blob, "fully remote", "100% remote", "fully-remote", "remote-first",
		"remote position", "remote role", "work from home", "work from anywhere"):
		return "remote"
	case anyContains(blob, "on-site", "onsite", "on site", "in office", "in-office",
		"days on site", "days in the office", "days per week in", "on premise", "on-premise",
		"in person", "in-person", "must be located", "must reside", "relocate to"):
		return "onsite"
	case strings.Contains(blob, "remote"):
		return "remote"
	default:
		return "unknown"
	}
}
