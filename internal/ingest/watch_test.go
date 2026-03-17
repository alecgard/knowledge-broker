package ingest_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/ingest"
)

func TestWatchPollCallsReIngest(t *testing.T) {
	var calls atomic.Int32
	reIngestFn := func(ctx context.Context) error {
		calls.Add(1)
		return nil
	}

	w := ingest.NewWatcher(50*time.Millisecond, reIngestFn, slog.Default())

	// Use a fast timer so we don't wait real time.
	w.SetTimerFunc(func(d time.Duration) (<-chan time.Time, func() bool) {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch, func() bool { return true }
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Watch(ctx)
	}()

	// Let a few cycles run.
	time.Sleep(200 * time.Millisecond)
	cancel()

	if err := <-done; err != nil {
		t.Fatalf("watch returned error: %v", err)
	}

	got := calls.Load()
	if got < 2 {
		t.Errorf("expected at least 2 re-ingestion calls, got %d", got)
	}
}

func TestWatchPollContextCancellation(t *testing.T) {
	reIngestFn := func(ctx context.Context) error {
		return nil
	}

	w := ingest.NewWatcher(10*time.Minute, reIngestFn, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Watch(ctx)
	}()

	// Give watcher time to start waiting.
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch returned error on cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit within 2 seconds of cancellation")
	}
}

func TestWatchPollContinuesOnError(t *testing.T) {
	var calls atomic.Int32
	reIngestFn := func(ctx context.Context) error {
		n := calls.Add(1)
		if n == 1 {
			return fmt.Errorf("simulated failure")
		}
		return nil
	}

	w := ingest.NewWatcher(50*time.Millisecond, reIngestFn, slog.Default())
	w.SetTimerFunc(func(d time.Duration) (<-chan time.Time, func() bool) {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch, func() bool { return true }
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Watch(ctx)
	}()

	// Let a few cycles run (including the failing first one).
	time.Sleep(200 * time.Millisecond)
	cancel()

	if err := <-done; err != nil {
		t.Fatalf("watch returned error: %v", err)
	}

	got := calls.Load()
	if got < 2 {
		t.Errorf("expected at least 2 calls (first fails, subsequent succeed), got %d", got)
	}
}
