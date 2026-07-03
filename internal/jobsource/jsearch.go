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

// JSearch is a source backed by the JSearch API on RapidAPI
// (https://rapidapi.com/letscrape-6bRBa3QguO5/api/jsearch). It indexes Google
// for Jobs, which surfaces listings from LinkedIn, Indeed, Glassdoor,
// ZipRecruiter and more — the broadest coverage available here. It requires a
// RapidAPI key.
type JSearch struct {
	apiKey   string
	http     httpDoer
	baseURL  string
	host     string
	country  string
	numPages int
}

// NewJSearch builds the JSearch source from a RapidAPI key.
func NewJSearch(apiKey string) *JSearch {
	return &JSearch{
		apiKey: apiKey,
		http:   &http.Client{},
		// JSearch's search endpoint is versioned as /search-v2; the older /search
		// path now returns 404 "endpoint does not exist".
		baseURL:  "https://jsearch.p.rapidapi.com/search-v2",
		host:     "jsearch.p.rapidapi.com",
		country:  "us",
		numPages: 2,
	}
}

// Name identifies this source in health reporting.
func (j *JSearch) Name() string { return "jsearch" }

// jsearchJob is one posting in the JSearch response.
type jsearchJob struct {
	JobTitle         string `json:"job_title"`
	EmployerName     string `json:"employer_name"`
	JobApplyLink     string `json:"job_apply_link"`
	JobApplyIsDirect bool   `json:"job_apply_is_direct"`
	ApplyOptions     []struct {
		Publisher string `json:"publisher"`
		ApplyLink string `json:"apply_link"`
		IsDirect  bool   `json:"is_direct"`
	} `json:"apply_options"`
	JobDescription  string  `json:"job_description"`
	JobIsRemote     bool    `json:"job_is_remote"`
	JobLocation     string  `json:"job_location"`
	JobCity         string  `json:"job_city"`
	JobState        string  `json:"job_state"`
	JobCountry      string  `json:"job_country"`
	JobMinSalary    float64 `json:"job_min_salary"`
	JobMaxSalary    float64 `json:"job_max_salary"`
	JobSalaryPeriod string  `json:"job_salary_period"`
}

// jsearchResponse mirrors the /search-v2 envelope, whose data is an object
// wrapping the jobs array (the older /search returned data as a bare array).
type jsearchResponse struct {
	Data struct {
		Jobs []jsearchJob `json:"jobs"`
	} `json:"data"`
}

// Discover runs each target-role query against JSearch and merges the mapped
// results. Duplicates across queries are collapsed later by the registry.
func (j *JSearch) Discover(ctx context.Context, query Query) ([]RawOpening, error) {
	var openings []RawOpening
	var lastErr error
	for _, term := range query.SearchTerms() {
		batch, err := j.searchOne(ctx, term)
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

// searchOne runs a single query term against JSearch.
func (j *JSearch) searchOne(ctx context.Context, term string) ([]RawOpening, error) {
	params := url.Values{}
	params.Set("query", term)
	params.Set("page", "1")
	params.Set("num_pages", strconv.Itoa(j.numPages))
	params.Set("country", j.country)
	// Bias toward fresher postings to cut down on expired/closed listings.
	params.Set("date_posted", "month")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("jsearch request: %w", err)
	}
	req.Header.Set("X-RapidAPI-Key", j.apiKey)
	req.Header.Set("X-RapidAPI-Host", j.host)

	resp, err := j.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jsearch call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("jsearch status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed jsearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("jsearch decode: %w", err)
	}

	openings := make([]RawOpening, 0, len(parsed.Data.Jobs))
	for _, job := range parsed.Data.Jobs {
		location := joinLocation(job.JobCity, job.JobState, job.JobCountry)
		if location == "" {
			location = job.JobLocation // e.g. "Anywhere" for remote roles
		}
		// Prefer the text ("Hybrid 3 days on site") over JSearch's job_is_remote
		// flag, which marks a role remote if it has any remote days. When the text
		// is silent, trust the flag: remote if set, otherwise onsite — a not-remote
		// role must not be left "unknown", or it slips past a remote-only filter.
		workLocation := inferWorkLocation(job.JobTitle, job.JobDescription, location)
		if workLocation == "unknown" {
			if job.JobIsRemote {
				workLocation = "remote"
			} else {
				workLocation = "onsite"
			}
		}
		salaryMin, salaryMax := annualSalary(job.JobMinSalary, job.JobMaxSalary, job.JobSalaryPeriod)
		openings = append(openings, RawOpening{
			Title:            job.JobTitle,
			Employer:         job.EmployerName,
			Location:         location,
			Description:      truncate(job.JobDescription, 4000),
			OriginalURL:      job.bestApplyLink(),
			WorkLocationType: workLocation,
			SalaryMin:        salaryMin,
			SalaryMax:        salaryMax,
			SourceName:       j.Name(),
		})
	}
	return openings, nil
}

// bestApplyLink prefers a direct-to-employer application link over an aggregator
// redirect (Monster, LinkedIn, etc.).
func (j jsearchJob) bestApplyLink() string {
	if j.JobApplyIsDirect && j.JobApplyLink != "" {
		return j.JobApplyLink
	}
	for _, opt := range j.ApplyOptions {
		if opt.IsDirect && opt.ApplyLink != "" {
			return opt.ApplyLink
		}
	}
	return j.JobApplyLink
}

// annualSalary returns min/max only when the figures are annual, so hourly or
// monthly rates don't get mistaken for a yearly salary by the salary gate.
func annualSalary(min, max float64, period string) (int, int) {
	if !strings.EqualFold(period, "YEAR") {
		return 0, 0
	}
	return int(min), int(max)
}

func joinLocation(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, strings.TrimSpace(p))
		}
	}
	return strings.Join(kept, ", ")
}
