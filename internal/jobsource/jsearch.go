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
		numPages: 1,
	}
}

// Name identifies this source in health reporting.
func (j *JSearch) Name() string { return "jsearch" }

type jsearchResponse struct {
	Data []struct {
		JobTitle        string  `json:"job_title"`
		EmployerName    string  `json:"employer_name"`
		JobApplyLink    string  `json:"job_apply_link"`
		JobDescription  string  `json:"job_description"`
		JobIsRemote     bool    `json:"job_is_remote"`
		JobCity         string  `json:"job_city"`
		JobState        string  `json:"job_state"`
		JobCountry      string  `json:"job_country"`
		JobMinSalary    float64 `json:"job_min_salary"`
		JobMaxSalary    float64 `json:"job_max_salary"`
		JobSalaryPeriod string  `json:"job_salary_period"`
	} `json:"data"`
}

// Discover queries JSearch and maps its results into RawOpenings.
func (j *JSearch) Discover(ctx context.Context, query Query) ([]RawOpening, error) {
	params := url.Values{}
	params.Set("query", strings.TrimSpace(strings.Join(query.Keywords, " ")))
	params.Set("page", "1")
	params.Set("num_pages", strconv.Itoa(j.numPages))
	params.Set("country", j.country)

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

	openings := make([]RawOpening, 0, len(parsed.Data))
	for _, job := range parsed.Data {
		workLocation := "unknown"
		if job.JobIsRemote {
			workLocation = "remote"
		}
		salaryMin, salaryMax := annualSalary(job.JobMinSalary, job.JobMaxSalary, job.JobSalaryPeriod)
		openings = append(openings, RawOpening{
			Title:            job.JobTitle,
			Employer:         job.EmployerName,
			Location:         joinLocation(job.JobCity, job.JobState, job.JobCountry),
			Description:      truncate(job.JobDescription, 4000),
			OriginalURL:      job.JobApplyLink,
			WorkLocationType: workLocation,
			SalaryMin:        salaryMin,
			SalaryMax:        salaryMax,
			SourceName:       j.Name(),
		})
	}
	return openings, nil
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
