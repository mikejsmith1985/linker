package jobsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Remotive is a key-free job source backed by the public Remotive API
// (https://remotive.com/api/remote-jobs). Every listing is a remote role, so it
// is the default source and works with no configuration.
type Remotive struct {
	limit   int
	http    httpDoer
	baseURL string
}

// NewRemotive builds the Remotive source.
func NewRemotive() *Remotive {
	return &Remotive{
		limit:   50,
		http:    &http.Client{},
		baseURL: "https://remotive.com/api/remote-jobs",
	}
}

// Name identifies this source in health reporting.
func (r *Remotive) Name() string { return "remotive" }

// remotiveResponse mirrors the fields of the Remotive API response we use.
type remotiveResponse struct {
	Jobs []struct {
		URL                       string `json:"url"`
		Title                     string `json:"title"`
		CompanyName               string `json:"company_name"`
		CandidateRequiredLocation string `json:"candidate_required_location"`
		Salary                    string `json:"salary"`
		Description               string `json:"description"`
	} `json:"jobs"`
}

// Discover runs each target-role query against Remotive and merges the results.
// Every result is a remote role. Duplicates are collapsed later by the registry.
func (r *Remotive) Discover(ctx context.Context, query Query) ([]RawOpening, error) {
	var openings []RawOpening
	var lastErr error
	for _, term := range query.SearchTerms() {
		batch, err := r.searchOne(ctx, term)
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

func (r *Remotive) searchOne(ctx context.Context, term string) ([]RawOpening, error) {
	params := url.Values{}
	if term != "" {
		params.Set("search", term)
	}
	params.Set("limit", strconv.Itoa(r.limit))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("remotive request: %w", err)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remotive call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("remotive status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed remotiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("remotive decode: %w", err)
	}

	openings := make([]RawOpening, 0, len(parsed.Jobs))
	for _, job := range parsed.Jobs {
		salaryMin, salaryMax := parseSalary(job.Salary)
		openings = append(openings, RawOpening{
			Title:            job.Title,
			Employer:         job.CompanyName,
			Location:         job.CandidateRequiredLocation,
			Description:      truncate(htmlToText(job.Description), 4000),
			OriginalURL:      job.URL,
			WorkLocationType: "remote",
			SalaryMin:        salaryMin,
			SalaryMax:        salaryMax,
			SourceName:       r.Name(),
		})
	}
	return openings, nil
}

// salaryAmountPattern matches dollar amounts like "$120,000" or "$120k". The
// leading "$" keeps it conservative so stray numbers (years, counts) are ignored.
var salaryAmountPattern = regexp.MustCompile(`\$\s?([0-9][0-9,.]*)\s?([kK])?`)

// parseSalary best-effort extracts a min (and optional max) from Remotive's
// free-text salary field. It returns (0, 0) when nothing dollar-denominated is
// found, so the salary gate simply does not fire on unstated pay.
func parseSalary(raw string) (int, int) {
	matches := salaryAmountPattern.FindAllStringSubmatch(raw, -1)
	amounts := make([]int, 0, len(matches))
	for _, m := range matches {
		digits := strings.ReplaceAll(strings.Split(m[1], ".")[0], ",", "")
		value, err := strconv.Atoi(digits)
		if err != nil {
			continue
		}
		if strings.EqualFold(m[2], "k") {
			value *= 1000
		}
		amounts = append(amounts, value)
	}
	switch len(amounts) {
	case 0:
		return 0, 0
	case 1:
		return amounts[0], 0
	default:
		return amounts[0], amounts[1]
	}
}
