package jobsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Jobicy is a key-free source backed by the public Jobicy API
// (https://jobicy.com/api/v2/remote-jobs). All listings are remote. It has no
// reliable server-side search, so results are keyword-filtered client-side.
type Jobicy struct {
	http    httpDoer
	baseURL string
}

// NewJobicy builds the Jobicy source.
func NewJobicy() *Jobicy {
	return &Jobicy{http: &http.Client{}, baseURL: "https://jobicy.com/api/v2/remote-jobs"}
}

// Name identifies this source in health reporting.
func (j *Jobicy) Name() string { return "jobicy" }

type jobicyResponse struct {
	Jobs []struct {
		JobTitle       string `json:"jobTitle"`
		CompanyName    string `json:"companyName"`
		JobGeo         string `json:"jobGeo"`
		JobDescription string `json:"jobDescription"`
		JobExcerpt     string `json:"jobExcerpt"`
		URL            string `json:"url"`
	} `json:"jobs"`
}

// Discover fetches Jobicy, maps and keyword-filters the results.
func (j *Jobicy) Discover(ctx context.Context, query Query) ([]RawOpening, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("jobicy request: %w", err)
	}
	resp, err := j.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jobicy call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("jobicy status %d: %s", resp.StatusCode, string(body))
	}

	var parsed jobicyResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("jobicy decode: %w", err)
	}

	openings := make([]RawOpening, 0, len(parsed.Jobs))
	for _, job := range parsed.Jobs {
		description := job.JobDescription
		if description == "" {
			description = job.JobExcerpt
		}
		openings = append(openings, RawOpening{
			Title:            job.JobTitle,
			Employer:         job.CompanyName,
			Location:         job.JobGeo,
			Description:      truncate(htmlToText(description), 4000),
			OriginalURL:      job.URL,
			WorkLocationType: "remote",
			SourceName:       j.Name(),
		})
	}
	return filterAndCap(openings, query.Keywords, defaultSourceCap), nil
}
