package jobsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// RemoteOK is a key-free source backed by the public RemoteOK API
// (https://remoteok.com/api). All listings are remote. The API has no reliable
// server-side search, so results are keyword-filtered client-side.
type RemoteOK struct {
	http    httpDoer
	baseURL string
}

// NewRemoteOK builds the RemoteOK source.
func NewRemoteOK() *RemoteOK {
	return &RemoteOK{http: &http.Client{}, baseURL: "https://remoteok.com/api"}
}

// Name identifies this source in health reporting.
func (r *RemoteOK) Name() string { return "remoteok" }

// remoteOKJob mirrors the fields we use. The API returns an array whose first
// element is a legal notice (no position), which we skip.
type remoteOKJob struct {
	Position    string `json:"position"`
	Company     string `json:"company"`
	Location    string `json:"location"`
	Description string `json:"description"`
	URL         string `json:"url"`
	ApplyURL    string `json:"apply_url"`
	SalaryMin   int    `json:"salary_min"`
	SalaryMax   int    `json:"salary_max"`
}

// Discover fetches RemoteOK, maps and keyword-filters the results.
func (r *RemoteOK) Discover(ctx context.Context, query Query) ([]RawOpening, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("remoteok request: %w", err)
	}
	req.Header.Set("User-Agent", "linker-job-matcher")
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remoteok call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("remoteok status %d: %s", resp.StatusCode, string(body))
	}

	var jobs []remoteOKJob
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, fmt.Errorf("remoteok decode: %w", err)
	}

	openings := make([]RawOpening, 0, len(jobs))
	for _, job := range jobs {
		if job.Position == "" { // legal-notice element or malformed
			continue
		}
		url := job.URL
		if url == "" {
			url = job.ApplyURL
		}
		openings = append(openings, RawOpening{
			Title:            job.Position,
			Employer:         job.Company,
			Location:         job.Location,
			Description:      truncate(htmlToText(job.Description), 4000),
			OriginalURL:      url,
			WorkLocationType: "remote",
			SalaryMin:        job.SalaryMin,
			SalaryMax:        job.SalaryMax,
			SourceName:       r.Name(),
		})
	}
	return filterAndCap(openings, query.Keywords, defaultSourceCap), nil
}
