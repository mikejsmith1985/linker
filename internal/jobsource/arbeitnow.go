package jobsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Arbeitnow is a key-free source backed by the public Arbeitnow job board API
// (https://www.arbeitnow.com/api/job-board-api). Coverage skews EU + remote. It
// has no server-side search, so results are keyword-filtered client-side.
type Arbeitnow struct {
	http    httpDoer
	baseURL string
}

// NewArbeitnow builds the Arbeitnow source.
func NewArbeitnow() *Arbeitnow {
	return &Arbeitnow{http: &http.Client{}, baseURL: "https://www.arbeitnow.com/api/job-board-api"}
}

// Name identifies this source in health reporting.
func (a *Arbeitnow) Name() string { return "arbeitnow" }

type arbeitnowResponse struct {
	Data []struct {
		Title       string `json:"title"`
		CompanyName string `json:"company_name"`
		Location    string `json:"location"`
		Description string `json:"description"`
		Remote      bool   `json:"remote"`
		URL         string `json:"url"`
	} `json:"data"`
}

// Discover fetches Arbeitnow, maps and keyword-filters the results.
func (a *Arbeitnow) Discover(ctx context.Context, query Query) ([]RawOpening, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("arbeitnow request: %w", err)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arbeitnow call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("arbeitnow status %d: %s", resp.StatusCode, string(body))
	}

	var parsed arbeitnowResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("arbeitnow decode: %w", err)
	}

	openings := make([]RawOpening, 0, len(parsed.Data))
	for _, job := range parsed.Data {
		workLocation := "unknown"
		if job.Remote {
			workLocation = "remote"
		}
		openings = append(openings, RawOpening{
			Title:            job.Title,
			Employer:         job.CompanyName,
			Location:         job.Location,
			Description:      truncate(htmlToText(job.Description), 4000),
			OriginalURL:      job.URL,
			WorkLocationType: workLocation,
			SourceName:       a.Name(),
		})
	}
	return filterAndCap(openings, query.FilterKeywords(), defaultSourceCap), nil
}
