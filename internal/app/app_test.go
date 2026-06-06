package app

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikejsmith1985/linker/internal/buffer"
	"github.com/mikejsmith1985/linker/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSelectPublisherStub(t *testing.T) {
	pub := SelectPublisher(config.Config{}, nil)
	if _, ok := pub.(*buffer.Stub); !ok {
		t.Errorf("expected *buffer.Stub, got %T", pub)
	}
}

func TestSelectPublisherLive(t *testing.T) {
	cfg := config.Config{BufferAccessToken: "tok", BufferProfileID: "prof"}
	pub := SelectPublisher(cfg, nil)
	if _, ok := pub.(*buffer.LiveClient); !ok {
		t.Errorf("expected *buffer.LiveClient, got %T", pub)
	}
}

func TestPollLoopRunsThenStops(t *testing.T) {
	var count int32
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		pollLoop(ctx, 10*time.Millisecond, func(context.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		}, testLogger())
		close(done)
	}()

	// Let the immediate tick plus a couple interval ticks run.
	time.Sleep(35 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pollLoop did not return after cancel")
	}
	if atomic.LoadInt32(&count) < 1 {
		t.Errorf("tick ran %d times, want >= 1", count)
	}
}

func TestPollLoopImmediateTick(t *testing.T) {
	var count int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go pollLoop(ctx, time.Hour, func(context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	}, testLogger())

	// With a 1h interval, only the immediate tick should fire quickly.
	time.Sleep(20 * time.Millisecond)
	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("immediate tick count = %d, want 1", count)
	}
}
