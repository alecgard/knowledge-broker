package ingest

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/knowledge-broker/knowledge-broker/internal/connector"
)

// Watcher monitors filesystem paths for changes and triggers re-ingestion.
type Watcher struct {
	pipeline *Pipeline
	logger   *slog.Logger
	debounce time.Duration
}

// NewWatcher creates a Watcher that uses the given pipeline for re-ingestion.
func NewWatcher(pipeline *Pipeline, logger *slog.Logger) *Watcher {
	return &Watcher{
		pipeline: pipeline,
		logger:   logger,
		debounce: 1 * time.Second,
	}
}

// SetDebounce overrides the default debounce duration (useful for tests).
func (w *Watcher) SetDebounce(d time.Duration) {
	w.debounce = d
}

// Watch monitors the given local directory paths for filesystem changes.
// When changes are detected, it re-runs ingestion for the affected source.
// It blocks until ctx is cancelled.
func (w *Watcher) Watch(ctx context.Context, paths []string) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	defer fsw.Close()

	// Resolve paths and build connectors.
	type watchedSource struct {
		absRoot string
		conn    connector.Connector
	}
	var sources []watchedSource

	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("resolve path %s: %w", p, err)
		}
		sources = append(sources, watchedSource{
			absRoot: abs,
			conn:    connector.NewFilesystemConnector(abs),
		})

		// Recursively add all directories under this path.
		if err := addDirsRecursive(fsw, abs); err != nil {
			return fmt.Errorf("add directories for %s: %w", abs, err)
		}
	}

	w.logger.Info("watching for changes", "paths", paths)

	// Debounce: track which source roots have pending changes.
	var mu sync.Mutex
	pending := make(map[string]bool)
	var timer *time.Timer

	resetTimer := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(w.debounce, func() {
			mu.Lock()
			roots := make([]string, 0, len(pending))
			for r := range pending {
				roots = append(roots, r)
			}
			pending = make(map[string]bool)
			mu.Unlock()

			for _, root := range roots {
				// Find the connector for this root.
				for _, src := range sources {
					if src.absRoot == root {
						w.logger.Info("re-ingesting", "source", src.conn.SourceName())
						result, err := w.pipeline.Run(ctx, src.conn)
						if err != nil {
							w.logger.Error("re-ingestion failed", "source", src.conn.SourceName(), "error", err)
						} else {
							w.logger.Info("re-ingestion complete",
								"source", src.conn.SourceName(),
								"added", result.Added,
								"deleted", result.Deleted,
								"skipped", result.Skipped,
								"errors", result.Errors,
							)
						}
						break
					}
				}
			}
		})
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return nil

		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}

			// Only react to writes, creates, removes, and renames.
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) &&
				!event.Has(fsnotify.Remove) && !event.Has(fsnotify.Rename) {
				continue
			}

			// If a new directory was created, start watching it.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := addDirsRecursive(fsw, event.Name); err != nil {
						w.logger.Warn("failed to watch new directory", "path", event.Name, "error", err)
					}
				}
			}

			// Find which source root this event belongs to.
			for _, src := range sources {
				if strings.HasPrefix(event.Name, src.absRoot) {
					mu.Lock()
					pending[src.absRoot] = true
					mu.Unlock()
					resetTimer()
					break
				}
			}

		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.logger.Warn("fsnotify error", "error", err)
		}
	}
}

// addDirsRecursive walks root and adds all directories to the fsnotify watcher,
// skipping directories that should be ignored (hidden dirs, SkipDirs).
func addDirsRecursive(fsw *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path != root {
			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			if connector.SkipDirs[name] {
				return fs.SkipDir
			}
		}
		return fsw.Add(path)
	})
}
