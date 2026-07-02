package jobsource

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/playwright-community/playwright-go"
)

// PlaywrightRunner drives a headless Chromium via Playwright to scrape a job
// board's public search results. It requires the Playwright browser binaries to
// be installed (`go run github.com/playwright-community/playwright-go/cmd/playwright install`).
//
// NOTE: the selectors below target LinkedIn's public jobs search markup, which
// changes often and may require an authenticated session; treat this runner as a
// best-effort starting point to tune per target board. It is only ever reached
// after the user acknowledges the terms-of-service and account-ban risk.
type PlaywrightRunner struct {
	searchURLTemplate string
}

// NewPlaywrightRunner builds a runner pointed at LinkedIn's public jobs search.
func NewPlaywrightRunner() *PlaywrightRunner {
	return &PlaywrightRunner{
		searchURLTemplate: "https://www.linkedin.com/jobs/search/?keywords=%s",
	}
}

// Search launches a headless browser, runs the query, and extracts job cards.
func (r *PlaywrightRunner) Search(ctx context.Context, query Query) ([]RawOpening, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start playwright (did you run 'playwright install'?): %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		return nil, fmt.Errorf("launch chromium: %w", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		return nil, fmt.Errorf("new page: %w", err)
	}

	target := fmt.Sprintf(r.searchURLTemplate, url.QueryEscape(strings.Join(query.Keywords, " ")))
	if _, err := page.Goto(target, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return nil, fmt.Errorf("navigate to search: %w", err)
	}

	cards, err := page.Locator("div.base-card").All()
	if err != nil {
		return nil, fmt.Errorf("locate job cards: %w", err)
	}

	var openings []RawOpening
	for _, card := range cards {
		title := firstText(card, "h3")
		if strings.TrimSpace(title) == "" {
			continue
		}
		openings = append(openings, RawOpening{
			Title:            strings.TrimSpace(title),
			Employer:         strings.TrimSpace(firstText(card, "h4")),
			Location:         strings.TrimSpace(firstText(card, ".job-search-card__location")),
			OriginalURL:      strings.TrimSpace(firstAttr(card, "a.base-card__full-link", "href")),
			WorkLocationType: "unknown",
			SourceName:       "browser",
		})
	}
	return openings, nil
}

func firstText(card playwright.Locator, selector string) string {
	text, err := card.Locator(selector).First().TextContent()
	if err != nil {
		return ""
	}
	return text
}

func firstAttr(card playwright.Locator, selector, attr string) string {
	value, err := card.Locator(selector).First().GetAttribute(attr)
	if err != nil {
		return ""
	}
	return value
}
