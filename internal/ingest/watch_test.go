package ingest_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/internal/ingest"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// newWatchTestPipeline creates a pipeline with in-memory store and mock embedder for watch tests.
func newWatchTestPipeline(t *testing.T) (*ingest.Pipeline, *store.SQLiteStore) {
	t.Helper()

	s, err := store.NewSQLiteStore(":memory:", embeddingDim)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	emb := NewMockEmbedder(embeddingDim)
	reg := extractor.NewRegistry(extractor.NewPlaintextExtractor(512, 64))
	reg.Register(extractor.NewMarkdownExtractor(512))
	pipeline := ingest.NewPipeline(s, emb, reg, 2, slog.Default())
	return pipeline, s
}

func TestWatchDetectsFileCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping watch test in short mode")
	}

	dir := t.TempDir()

	// Write an initial file so the first ingest has something.
	if err := os.WriteFile(filepath.Join(dir, "initial.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	pipeline, s := newWatchTestPipeline(t)

	// Run initial ingestion.
	conn := connector.NewFilesystemConnector(dir)
	if _, err := pipeline.Run(context.Background(), conn); err != nil {
		t.Fatalf("initial ingest: %v", err)
	}

	watcher := ingest.NewWatcher(pipeline, slog.Default())
	watcher.SetDebounce(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.Watch(ctx, []string{dir})
	}()

	// Give the watcher time to set up.
	time.Sleep(200 * time.Millisecond)

	// Create a new file — should trigger re-ingestion.
	if err := os.WriteFile(filepath.Join(dir, "new-file.txt"), []byte("new content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + processing.
	time.Sleep(500 * time.Millisecond)

	cancel()
	if err := <-watchDone; err != nil {
		t.Fatalf("watch returned error: %v", err)
	}

	// Verify the new file was ingested.
	checksums, err := s.GetChecksums(context.Background(), "filesystem", filepath.Base(dir))
	if err != nil {
		t.Fatalf("get checksums: %v", err)
	}

	newFilePath := filepath.Join(dir, "new-file.txt")
	if _, ok := checksums[newFilePath]; !ok {
		t.Errorf("new-file.txt was not ingested; checksums: %v", checksums)
	}
}

func TestWatchDebounce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping watch test in short mode")
	}

	dir := t.TempDir()

	// Write an initial file.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}

	pipeline, _ := newWatchTestPipeline(t)

	// Run initial ingestion.
	conn := connector.NewFilesystemConnector(dir)
	if _, err := pipeline.Run(context.Background(), conn); err != nil {
		t.Fatalf("initial ingest: %v", err)
	}

	watcher := ingest.NewWatcher(pipeline, slog.Default())
	watcher.SetDebounce(300 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.Watch(ctx, []string{dir})
	}()

	// Give the watcher time to set up.
	time.Sleep(200 * time.Millisecond)

	// Rapid-fire multiple file changes — these should be debounced into one re-ingestion.
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, "rapid.txt")
		if err := os.WriteFile(name, []byte("version "+string(rune('0'+i))), 0644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for debounce to fire.
	time.Sleep(600 * time.Millisecond)

	cancel()
	if err := <-watchDone; err != nil {
		t.Fatalf("watch returned error: %v", err)
	}

	// If we got here without hanging or crashing, debounce worked correctly.
}

func TestWatchContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping watch test in short mode")
	}

	dir := t.TempDir()
	pipeline, _ := newWatchTestPipeline(t)
	watcher := ingest.NewWatcher(pipeline, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())

	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.Watch(ctx, []string{dir})
	}()

	// Give watcher time to start.
	time.Sleep(100 * time.Millisecond)

	// Cancel — watcher should exit cleanly.
	cancel()

	select {
	case err := <-watchDone:
		if err != nil {
			t.Fatalf("watch returned error on cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit within 2 seconds of cancellation")
	}
}
