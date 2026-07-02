package jobsource

import (
	"context"
	"errors"
	"testing"
)

// recordingRunner reports whether it was invoked.
type recordingRunner struct {
	called bool
	out    []RawOpening
}

func (r *recordingRunner) Search(context.Context, Query) ([]RawOpening, error) {
	r.called = true
	return r.out, nil
}

func TestBrowserRefusesWithoutAcknowledgment(t *testing.T) {
	runner := &recordingRunner{}
	browser := NewBrowser(func() bool { return false }, runner)

	_, err := browser.Discover(context.Background(), Query{})
	if !errors.Is(err, ErrAcknowledgmentRequired) {
		t.Errorf("err = %v, want ErrAcknowledgmentRequired", err)
	}
	if runner.called {
		t.Error("runner ran despite missing acknowledgment (FR-023 violated)")
	}
}

func TestBrowserRefusesWithNilAckProvider(t *testing.T) {
	browser := NewBrowser(nil, &recordingRunner{})
	if _, err := browser.Discover(context.Background(), Query{}); !errors.Is(err, ErrAcknowledgmentRequired) {
		t.Errorf("err = %v, want ErrAcknowledgmentRequired", err)
	}
}

func TestBrowserRunsWhenAcknowledged(t *testing.T) {
	runner := &recordingRunner{out: []RawOpening{{Title: "Engineer", SourceName: "browser"}}}
	browser := NewBrowser(func() bool { return true }, runner)

	out, err := browser.Discover(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if !runner.called {
		t.Error("runner did not run despite acknowledgment")
	}
	if len(out) != 1 || out[0].Title != "Engineer" {
		t.Errorf("output = %+v, want the runner's opening", out)
	}
}
