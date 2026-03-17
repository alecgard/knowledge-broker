package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// ReIngestFunc is called each polling cycle to re-ingest all registered sources.
// It receives the context and should return an error only if the cycle should
// be considered failed (errors are logged as warnings; the loop continues).
type ReIngestFunc func(ctx context.Context) error

// Watcher periodically re-ingests all registered sources on a fixed interval.
type Watcher struct {
	interval   time.Duration
	reIngestFn ReIngestFunc
	logger     *slog.Logger

	// timerFunc creates a channel that fires after d. Returns the channel
	// and a stop function. Overridable for tests.
	timerFunc func(d time.Duration) (<-chan time.Time, func() bool)
	// nowFunc returns current time. Overridable for tests.
	nowFunc func() time.Time
}

// NewWatcher creates a Watcher that calls reIngestFn every interval.
func NewWatcher(interval time.Duration, reIngestFn ReIngestFunc, logger *slog.Logger) *Watcher {
	return &Watcher{
		interval:   interval,
		reIngestFn: reIngestFn,
		logger:     logger,
		nowFunc:    time.Now,
	}
}

// SetTimerFunc overrides the default timer creation (useful for tests).
func (w *Watcher) SetTimerFunc(fn func(time.Duration) (<-chan time.Time, func() bool)) {
	w.timerFunc = fn
}

// Watch runs the polling loop. It blocks until ctx is cancelled.
// Each cycle logs the start time, calls the re-ingest function, logs results,
// and then waits for the interval before the next cycle.
func (w *Watcher) Watch(ctx context.Context) error {
	for {
		nextRun := w.nowFunc().Add(w.interval)
		fmt.Fprintf(os.Stderr, "Next re-ingestion at %s (in %s)\n", nextRun.Format(time.RFC3339), w.interval)

		ch, stop := w.newTimer(w.interval)
		select {
		case <-ctx.Done():
			stop()
			fmt.Fprintln(os.Stderr, "Watch mode stopped.")
			return nil
		case <-ch:
		}

		fmt.Fprintf(os.Stderr, "Starting scheduled re-ingestion at %s...\n", w.nowFunc().Format(time.RFC3339))

		if err := w.reIngestFn(ctx); err != nil {
			// Check if context was cancelled during re-ingestion.
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, "Watch mode stopped.")
				return nil
			}
			w.logger.Warn("scheduled re-ingestion failed, will retry next cycle", "error", err)
		} else {
			fmt.Fprintln(os.Stderr, "Scheduled re-ingestion complete.")
		}
	}
}

// newTimer creates a timer. Uses timerFunc if set (for testing), otherwise
// uses time.NewTimer.
func (w *Watcher) newTimer(d time.Duration) (<-chan time.Time, func() bool) {
	if w.timerFunc != nil {
		return w.timerFunc(d)
	}
	t := time.NewTimer(d)
	return t.C, t.Stop
}
