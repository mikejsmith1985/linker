package jobsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// companyCap bounds how many of a single company's openings are returned after
// keyword filtering, so a large employer does not flood a search.
const companyCap = 20

// CompanyCareers discovers openings straight from a named company's public
// applicant-tracking feed — Greenhouse first, then Lever. It targets specific
// employers the user names (e.g. "Stripe"), rather than searching a board.
// Companies on other systems (e.g. Workday) are not covered here.
type CompanyCareers struct {
	companies []string
	http      httpDoer
	ghBase    string
	leverBase string
}

// NewCompanyCareers builds the source for the given company names.
func NewCompanyCareers(companies []string) *CompanyCareers {
	return &CompanyCareers{
		companies: companies,
		http:      &http.Client{},
		ghBase:    "https://boards-api.greenhouse.io/v1/boards",
		leverBase: "https://api.lever.co/v0/postings",
	}
}

// Name identifies this source in health reporting.
func (c *CompanyCareers) Name() string { return "company" }

// Discover resolves each company to its ATS feed and returns keyword-filtered
// openings. A company that cannot be resolved fails on its own without aborting
// the batch.
func (c *CompanyCareers) Discover(ctx context.Context, query Query) ([]RawOpening, error) {
	var out []RawOpening
	var lastErr error
	for _, company := range c.companies {
		jobs, err := c.fetchCompany(ctx, company)
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, filterAndCap(jobs, query.FilterKeywords(), companyCap)...)
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

// fetchCompany tries Greenhouse, then Lever, for one company.
func (c *CompanyCareers) fetchCompany(ctx context.Context, company string) ([]RawOpening, error) {
	token := normalizeCompany(company)
	if jobs, err := c.greenhouse(ctx, token, company); err == nil && len(jobs) > 0 {
		return jobs, nil
	}
	jobs, err := c.lever(ctx, token, company)
	if err != nil {
		return nil, fmt.Errorf("no public ATS feed found for %q (tried Greenhouse and Lever)", company)
	}
	return jobs, nil
}

// normalizeCompany turns a display name into a likely ATS board token:
// lowercase, alphanumeric only ("Match Group" -> "matchgroup").
func normalizeCompany(company string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(company) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type greenhouseResponse struct {
	Jobs []struct {
		Title       string `json:"title"`
		AbsoluteURL string `json:"absolute_url"`
		Content     string `json:"content"`
		Location    struct {
			Name string `json:"name"`
		} `json:"location"`
	} `json:"jobs"`
}

func (c *CompanyCareers) greenhouse(ctx context.Context, token, display string) ([]RawOpening, error) {
	url := fmt.Sprintf("%s/%s/jobs?content=true", c.ghBase, token)
	body, err := c.getJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	var parsed greenhouseResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("greenhouse decode: %w", err)
	}
	openings := make([]RawOpening, 0, len(parsed.Jobs))
	for _, job := range parsed.Jobs {
		text := truncate(htmlToText(job.Content), 4000)
		openings = append(openings, RawOpening{
			Title:            job.Title,
			Employer:         display,
			Location:         job.Location.Name,
			Description:      text,
			OriginalURL:      job.AbsoluteURL,
			WorkLocationType: inferWorkLocation(job.Title, job.Location.Name, text),
			SourceName:       c.Name(),
		})
	}
	return openings, nil
}

type leverPosting struct {
	Text             string `json:"text"`
	HostedURL        string `json:"hostedUrl"`
	DescriptionPlain string `json:"descriptionPlain"`
	WorkplaceType    string `json:"workplaceType"`
	Categories       struct {
		Location string `json:"location"`
	} `json:"categories"`
}

func (c *CompanyCareers) lever(ctx context.Context, token, display string) ([]RawOpening, error) {
	url := fmt.Sprintf("%s/%s?mode=json", c.leverBase, token)
	body, err := c.getJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	var postings []leverPosting
	if err := json.Unmarshal(body, &postings); err != nil {
		return nil, fmt.Errorf("lever decode: %w", err)
	}
	openings := make([]RawOpening, 0, len(postings))
	for _, posting := range postings {
		workLocation := inferWorkLocation(posting.WorkplaceType, posting.Categories.Location, posting.DescriptionPlain)
		openings = append(openings, RawOpening{
			Title:            posting.Text,
			Employer:         display,
			Location:         posting.Categories.Location,
			Description:      truncate(posting.DescriptionPlain, 4000),
			OriginalURL:      posting.HostedURL,
			WorkLocationType: workLocation,
			SourceName:       c.Name(),
		})
	}
	return openings, nil
}

// getJSON performs a GET and returns the body, treating non-200 as an error.
func (c *CompanyCareers) getJSON(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "linker-job-matcher")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
