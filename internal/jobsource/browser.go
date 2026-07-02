package jobsource

import (
	"context"
	"errors"
)

// ErrAcknowledgmentRequired is returned when browser automation is attempted
// without the user's explicit risk acknowledgment (FR-023).
var ErrAcknowledgmentRequired = errors.New("browser automation requires explicit risk acknowledgment")

// BrowserRunner performs the actual automated browsing. It is injectable so the
// gating logic is unit-testable without launching a real browser.
type BrowserRunner interface {
	Search(ctx context.Context, query Query) ([]RawOpening, error)
}

// Browser is the opt-in automated-browsing source for boards that do not offer a
// permitted API (e.g. LinkedIn). It refuses to run until the user has explicitly
// acknowledged the terms-of-service and account-ban risk (FR-022, FR-023).
type Browser struct {
	isAcknowledged func() bool
	runner         BrowserRunner
}

// NewBrowser builds the browser source. ackProvider reports whether the user has
// recorded the risk acknowledgment (read at search time so it reflects the
// latest saved preference).
func NewBrowser(ackProvider func() bool, runner BrowserRunner) *Browser {
	return &Browser{isAcknowledged: ackProvider, runner: runner}
}

// Name identifies this source in health reporting.
func (b *Browser) Name() string { return "browser" }

// Discover runs the automated browse, but only after the acknowledgment gate.
func (b *Browser) Discover(ctx context.Context, query Query) ([]RawOpening, error) {
	if b.isAcknowledged == nil || !b.isAcknowledged() {
		return nil, ErrAcknowledgmentRequired
	}
	return b.runner.Search(ctx, query)
}
