package ingest_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/internal/ingest"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

const embeddingDim = 8

// ---------------------------------------------------------------------------
// MockEmbedder
// ---------------------------------------------------------------------------

// MockEmbedder returns deterministic fake embeddings derived from a simple
// hash of the input text.  Because the hash distributes bits evenly, texts
// that share words will still differ, but the vectors are stable across runs
// and fast to compute.
type MockEmbedder struct {
	dim int
	// FailTexts is a set of text content that should return nil embeddings
	// (simulating embedding failure for oversized chunks).
	FailTexts map[string]bool
}

func NewMockEmbedder(dim int) *MockEmbedder {
	return &MockEmbedder{dim: dim}
}

func (m *MockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return m.hashToVector(text), nil
}

func (m *MockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		if m.FailTexts != nil && m.FailTexts[t] {
			vecs[i] = nil
		} else {
			vecs[i] = m.hashToVector(t)
		}
	}
	return vecs, nil
}

func (m *MockEmbedder) Dimension() int {
	return m.dim
}

func (m *MockEmbedder) CheckHealth(_ context.Context) error {
	return nil
}

// hashToVector produces a deterministic float32 vector of length m.dim from
// the SHA-256 of text.  Each component is in [0, 1) and the result is
// L2-normalised so that cosine distance is meaningful.
func (m *MockEmbedder) hashToVector(text string) []float32 {
	h := sha256.Sum256([]byte(text))
	vec := make([]float32, m.dim)
	for i := 0; i < m.dim; i++ {
		// Use two hash bytes per dimension to get a value in [0, 65535].
		idx := (i * 2) % len(h)
		val := float64(uint16(h[idx])<<8|uint16(h[(idx+1)%len(h)])) / 65536.0
		vec[i] = float32(val)
	}
	// L2-normalise.
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	}
	return vec
}

// ---------------------------------------------------------------------------
// MockConnector
// ---------------------------------------------------------------------------

// MockConnector is an in-memory connector that returns a configurable set of
// RawDocuments.  Callers can mutate Docs and RemovedPaths between calls to
// simulate file changes.
type MockConnector struct {
	ConnectorName string
	Docs          []model.RawDocument
	RemovedPaths  []string
	// LastScanOpts captures the ScanOptions from the most recent Scan call.
	LastScanOpts connector.ScanOptions
}

var _ connector.Connector = (*MockConnector)(nil)
var _ connector.StreamingConnector = (*MockConnector)(nil)

func (m *MockConnector) Name() string             { return m.ConnectorName }
func (m *MockConnector) SourceName() string       { return m.ConnectorName }
func (m *MockConnector) Config(mode string) map[string]string { return map[string]string{} }

func (m *MockConnector) ScanStream(ctx context.Context, opts connector.ScanOptions) <-chan connector.ScanEvent {
	ch := make(chan connector.ScanEvent, 64)
	go func() {
		defer close(ch)
		m.LastScanOpts = opts
		known := opts.Known

		currentPaths := make(map[string]bool)
		for _, d := range m.Docs {
			currentPaths[d.Path] = true
		}

		// Send each doc as an event.
		for _, d := range m.Docs {
			oldChecksum, exists := known[d.Path]
			if !exists || oldChecksum != d.Checksum {
				doc := d // copy
				select {
				case ch <- connector.ScanEvent{Doc: &doc}:
				case <-ctx.Done():
					return
				}
			}
		}

		// Compute deletions.
		var deleted []string
		for path := range known {
			if !currentPaths[path] {
				deleted = append(deleted, path)
			}
		}

		// Send final event with deletions.
		select {
		case ch <- connector.ScanEvent{Deleted: deleted}:
		case <-ctx.Done():
		}
	}()
	return ch
}

func (m *MockConnector) Scan(_ context.Context, opts connector.ScanOptions) ([]model.RawDocument, []string, error) {
	m.LastScanOpts = opts
	known := opts.Known
	// Build the set of currently-known paths for deletion detection.
	currentPaths := make(map[string]bool)
	for _, d := range m.Docs {
		currentPaths[d.Path] = true
	}

	// Determine which docs are new or changed.
	var changed []model.RawDocument
	for _, d := range m.Docs {
		oldChecksum, exists := known[d.Path]
		if !exists || oldChecksum != d.Checksum {
			changed = append(changed, d)
		}
	}

	// Determine deleted paths: anything in known that is no longer present,
	// plus any explicitly-removed paths.
	var deleted []string
	for path := range known {
		if !currentPaths[path] {
			deleted = append(deleted, path)
		}
	}
	// Also honour explicit removals (useful when the test wants to be
	// explicit rather than relying on path-set difference).
	for _, p := range m.RemovedPaths {
		if _, inKnown := known[p]; inKnown && !currentPaths[p] {
			// Already captured above; skip duplicate.
			continue
		}
	}

	return changed, deleted, nil
}

// ---------------------------------------------------------------------------
// MockLLM
// ---------------------------------------------------------------------------

// MockLLM satisfies query.LLM and returns a canned response.
// It includes a legacy KB_META block to verify backward-compat stripping.
type MockLLM struct{}

var _ query.LLM = (*MockLLM)(nil)

func (m *MockLLM) StreamAnswer(_ context.Context, _ string, _ []model.Message, onText func(string)) (string, error) {
	response := `Retry logic is implemented using exponential back-off with jitter.
The RetryWithBackoff function wraps any operation and retries it up to
a configurable maximum number of attempts.

---KB_META---
{
  "confidence": {
    "overall": 0.84,
    "breakdown": {
      "freshness": 0.9,
      "corroboration": 0.8,
      "consistency": 0.95,
      "authority": 0.7
    }
  },
  "sources": [],
  "contradictions": []
}
---KB_META_END---`

	if onText != nil {
		onText(response)
	}
	return response, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewSQLiteStore(dbPath, embeddingDim)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newTestRegistry() *extractor.Registry {
	r := extractor.NewRegistry(extractor.NewPlaintextExtractor(1500, 150))
	r.Register(extractor.NewMarkdownExtractor(2000))
	r.Register(extractor.NewCodeExtractor(2000))
	return r
}

func makeDoc(path, content, checksum string) model.RawDocument {
	return model.RawDocument{
		Path:         path,
		Content:      []byte(content),
		ContentDate: time.Now().UTC(),
		Author:       "test-author",
		SourceURI:    "file://" + path,
		SourceType:   "mock",
		SourceName:   "mock",
		Checksum:     checksum,
	}
}

func checksum(content string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestIngestAndQuery(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	mdContent := `# Retry Logic

## Overview
The retry module implements exponential back-off with jitter.
It is used throughout the codebase to handle transient failures.

## Configuration
Max retries can be set via the RETRY_MAX environment variable.
Default is 3 attempts.
`

	goContent := `package retry

import (
	"math/rand"
	"time"
)

func RetryWithBackoff(attempts int, fn func() error) error {
	for i := 0; i < attempts; i++ {
		err := fn()
		if err == nil {
			return nil
		}
		backoff := time.Duration(1<<uint(i)) * time.Second
		jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
		time.Sleep(backoff + jitter)
	}
	return fn()
}
`

	configContent := `retry:
  max_attempts: 3
  initial_backoff_ms: 1000
  max_backoff_ms: 30000
  jitter: true
`

	conn := &MockConnector{
		ConnectorName: "mock",
		Docs: []model.RawDocument{
			makeDoc("docs/retry.md", mdContent, checksum(mdContent)),
			makeDoc("pkg/retry/retry.go", goContent, checksum(goContent)),
			makeDoc("config/retry.yaml", configContent, checksum(configContent)),
		},
	}

	// Run ingestion.
	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)
	result, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	if result.Added == 0 {
		t.Fatal("expected fragments to be added, got 0")
	}
	t.Logf("Ingestion result: added=%d deleted=%d skipped=%d errors=%d",
		result.Added, result.Deleted, result.Skipped, result.Errors)

	// Verify fragments are stored by checking checksums.
	checksums, err := s.GetChecksums(ctx, "mock", "mock")
	if err != nil {
		t.Fatalf("GetChecksums: %v", err)
	}
	if len(checksums) == 0 {
		t.Fatal("expected checksums to be stored, got 0")
	}
	t.Logf("Stored %d path checksums", len(checksums))

	// Verify vector search returns results.
	queryVec, _ := embedder.Embed(ctx, "how does retry work?")
	fragments, err := s.SearchByVector(ctx, queryVec, 5)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(fragments) == 0 {
		t.Fatal("expected vector search to return fragments, got 0")
	}
	t.Logf("Vector search returned %d fragments", len(fragments))

	// Verify the query engine returns an answer.
	llm := &MockLLM{}
	engine := query.NewEngine(s, embedder, llm, 10)

	req := model.QueryRequest{
		Messages: []model.Message{
			{Role: "user", Content: "how does retry work?"},
		},
		Limit: 5,
	}

	var streamed string
	answer, err := engine.Query(ctx, req, func(text string) {
		streamed += text
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if answer.Content == "" {
		t.Fatal("expected answer content, got empty string")
	}
	if streamed == "" {
		t.Fatal("expected streamed text, got empty string")
	}
	if answer.Confidence.Breakdown.Freshness == 0 && answer.Confidence.Breakdown.Consistency == 0 {
		t.Fatal("expected server-computed confidence signals to be non-zero")
	}
	if answer.Confidence.Overall == 0 {
		t.Fatal("expected server-computed overall confidence to be non-zero")
	}
	// Legacy KB_META block should be stripped from the content.
	if strings.Contains(answer.Content, "KB_META") {
		t.Fatal("expected KB_META block to be stripped from answer content")
	}
	t.Logf("Answer (first 100 chars): %.100s", answer.Content)
	t.Logf("Confidence: overall=%.2f freshness=%.2f corroboration=%.2f consistency=%.2f authority=%.2f",
		answer.Confidence.Overall,
		answer.Confidence.Breakdown.Freshness, answer.Confidence.Breakdown.Corroboration,
		answer.Confidence.Breakdown.Consistency, answer.Confidence.Breakdown.Authority)
}

func TestIncrementalIngestion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	originalContent := "# Original Document\n\nThis is the original content about authentication.\n"
	doc := makeDoc("docs/auth.md", originalContent, checksum(originalContent))

	conn := &MockConnector{
		ConnectorName: "mock",
		Docs:       []model.RawDocument{doc},
	}

	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)

	// First ingestion.
	result1, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("First Run: %v", err)
	}
	if result1.Added == 0 {
		t.Fatal("first ingestion should add fragments")
	}
	t.Logf("First ingestion: added=%d skipped=%d", result1.Added, result1.Skipped)

	// Second ingestion with same checksum: everything should be skipped.
	result2, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Second Run: %v", err)
	}
	if result2.Added != 0 {
		t.Fatalf("second ingestion should add 0 fragments, got %d", result2.Added)
	}
	if result2.Skipped == 0 {
		t.Fatal("second ingestion should skip fragments")
	}
	t.Logf("Second ingestion: added=%d skipped=%d", result2.Added, result2.Skipped)

	// Third ingestion with changed content.
	updatedContent := "# Updated Document\n\nThis content has been updated with new auth flow.\n"
	conn.Docs = []model.RawDocument{
		makeDoc("docs/auth.md", updatedContent, checksum(updatedContent)),
	}

	result3, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Third Run: %v", err)
	}
	if result3.Added == 0 {
		t.Fatal("third ingestion should add updated fragments")
	}
	t.Logf("Third ingestion: added=%d skipped=%d", result3.Added, result3.Skipped)

	// Verify the stored content reflects the update.
	checksums, err := s.GetChecksums(ctx, "mock", "mock")
	if err != nil {
		t.Fatalf("GetChecksums: %v", err)
	}
	storedChecksum := checksums["docs/auth.md"]
	expectedChecksum := checksum(updatedContent)
	if storedChecksum != expectedChecksum {
		t.Fatalf("stored checksum %q does not match expected %q", storedChecksum, expectedChecksum)
	}
}

func TestForceReingestion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	content := "# Auth Guide\n\nThis document describes authentication flows.\n"
	conn := &MockConnector{
		ConnectorName: "mock",
		Docs:          []model.RawDocument{makeDoc("docs/auth.md", content, checksum(content))},
	}

	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)

	// First ingestion.
	result1, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("First Run: %v", err)
	}
	if result1.Added == 0 {
		t.Fatal("first ingestion should add fragments")
	}

	// Second ingestion without force: should skip everything.
	result2, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Second Run: %v", err)
	}
	if result2.Added != 0 {
		t.Fatalf("second ingestion without force should add 0, got %d", result2.Added)
	}
	if result2.Skipped == 0 {
		t.Fatal("second ingestion without force should skip fragments")
	}

	// Third ingestion with force: should re-ingest everything.
	result3, err := pipeline.Run(ctx, conn, ingest.Options{Force: true})
	if err != nil {
		t.Fatalf("Third Run (force): %v", err)
	}
	if result3.Added == 0 {
		t.Fatal("force ingestion should add fragments")
	}
	if result3.Skipped != 0 {
		t.Fatalf("force ingestion should skip 0, got %d", result3.Skipped)
	}
	if result3.Deleted != 0 {
		t.Fatalf("force ingestion should delete 0, got %d", result3.Deleted)
	}
	t.Logf("Force ingestion: added=%d skipped=%d deleted=%d", result3.Added, result3.Skipped, result3.Deleted)
}

func TestDeletedFiles(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	content1 := "# Document One\n\nFirst document about networking.\n"
	content2 := "# Document Two\n\nSecond document about logging.\n"
	content3 := "# Document Three\n\nThird document about metrics.\n"

	conn := &MockConnector{
		ConnectorName: "mock",
		Docs: []model.RawDocument{
			makeDoc("docs/networking.md", content1, checksum(content1)),
			makeDoc("docs/logging.md", content2, checksum(content2)),
			makeDoc("docs/metrics.md", content3, checksum(content3)),
		},
	}

	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)

	// Initial ingestion.
	result1, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("First Run: %v", err)
	}
	initialAdded := result1.Added
	t.Logf("Initial ingestion: added=%d", initialAdded)

	// Verify all three documents are stored.
	checksums, err := s.GetChecksums(ctx, "mock", "mock")
	if err != nil {
		t.Fatalf("GetChecksums: %v", err)
	}
	if len(checksums) != 3 {
		t.Fatalf("expected 3 paths in checksums, got %d", len(checksums))
	}

	// Remove docs/logging.md from the connector.
	conn.Docs = []model.RawDocument{
		makeDoc("docs/networking.md", content1, checksum(content1)),
		makeDoc("docs/metrics.md", content3, checksum(content3)),
	}

	// Re-ingest.
	result2, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Second Run: %v", err)
	}
	if result2.Deleted == 0 {
		t.Fatal("expected deletions after removing a file")
	}
	t.Logf("After deletion: added=%d deleted=%d skipped=%d",
		result2.Added, result2.Deleted, result2.Skipped)

	// Verify only two documents remain.
	checksums, err = s.GetChecksums(ctx, "mock", "mock")
	if err != nil {
		t.Fatalf("GetChecksums after deletion: %v", err)
	}
	if len(checksums) != 2 {
		t.Fatalf("expected 2 paths after deletion, got %d", len(checksums))
	}
	if _, exists := checksums["docs/logging.md"]; exists {
		t.Fatal("docs/logging.md should have been deleted")
	}

	// Verify vector search no longer returns fragments from the deleted file.
	queryVec, _ := embedder.Embed(ctx, "logging")
	fragments, err := s.SearchByVector(ctx, queryVec, 10)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	for _, f := range fragments {
		if f.SourcePath == "docs/logging.md" {
			t.Fatal("fragment from deleted file docs/logging.md still appears in search results")
		}
	}
	t.Logf("After deletion, vector search returns %d fragments (none from deleted file)", len(fragments))
}

func TestNilEmbeddingSkipsFragment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	registry := newTestRegistry()

	// Create a Go file with two functions. We'll make the embedder fail on
	// one of the chunk contents to simulate an oversized chunk.
	goContent := `package main

func Good() {
	// this function is fine
}

func Bad() {
	// this function will fail embedding
}
`

	embedder := NewMockEmbedder(embeddingDim)

	conn := &MockConnector{
		ConnectorName: "mock",
		Docs: []model.RawDocument{
			makeDoc("pkg/example.go", goContent, checksum(goContent)),
		},
	}

	// First, ingest normally to see how many fragments we get.
	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)
	result1, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("First Run: %v", err)
	}
	totalFragments := result1.Added
	t.Logf("Normal ingestion: added=%d", totalFragments)
	if totalFragments == 0 {
		t.Fatal("expected fragments to be added")
	}

	// Now re-ingest with one chunk's text causing a nil embedding.
	// We need a fresh store so that checksums don't cause skipping.
	s2 := newTestStore(t)

	// Extract chunks to find one to fail on.
	doc := conn.Docs[0]
	extractResult, err := ingest.ExtractChunks(doc, registry)
	if err != nil {
		t.Fatalf("ExtractChunks: %v", err)
	}
	if len(extractResult.Chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(extractResult.Chunks))
	}

	// Pick the second chunk's content to fail.
	failContent := extractResult.Chunks[1].Content
	embedder2 := NewMockEmbedder(embeddingDim)
	embedder2.FailTexts = map[string]bool{failContent: true}

	pipeline2 := ingest.NewPipeline(s2, embedder2, registry, 2, nil)
	result2, err := pipeline2.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Second Run: %v", err)
	}

	// Should have fewer fragments than the normal run (the failed one is skipped).
	if result2.Added >= totalFragments {
		t.Fatalf("expected fewer fragments when one embedding fails: got %d, normal was %d",
			result2.Added, totalFragments)
	}
	if result2.Added == 0 {
		t.Fatal("expected at least some fragments to be added despite one failure")
	}
	t.Logf("With nil embedding: added=%d (normal was %d)", result2.Added, totalFragments)
}

func TestPipelinePassesLastIngestToConnector(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	content := "# Test Doc\n\nSome content.\n"
	conn := &MockConnector{
		ConnectorName: "mock",
		Docs:          []model.RawDocument{makeDoc("docs/test.md", content, checksum(content))},
	}

	// Register a source with a LastIngest timestamp.
	lastIngest := time.Now().Add(-24 * time.Hour)
	err := s.RegisterSource(ctx, model.Source{
		SourceType: "mock",
		SourceName: "mock",
		Config:     map[string]string{},
		LastIngest: &lastIngest,
	})
	if err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}

	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)
	_, err = pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	// Verify that the connector received the LastIngest value.
	if conn.LastScanOpts.LastIngest == nil {
		t.Fatal("expected LastIngest to be passed to connector, got nil")
	}
	// Allow 1 second tolerance for timestamp comparison.
	diff := conn.LastScanOpts.LastIngest.Sub(lastIngest)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("LastIngest mismatch: got %v, want ~%v", *conn.LastScanOpts.LastIngest, lastIngest)
	}
}

func TestPipelineNoLastIngestForNewSource(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	content := "# New Doc\n\nBrand new content.\n"
	conn := &MockConnector{
		ConnectorName: "mock",
		Docs:          []model.RawDocument{makeDoc("docs/new.md", content, checksum(content))},
	}

	// Do NOT register a source — simulates first-ever ingestion.
	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)
	_, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	// Verify that LastIngest is nil (no previous source record).
	if conn.LastScanOpts.LastIngest != nil {
		t.Errorf("expected nil LastIngest for new source, got %v", conn.LastScanOpts.LastIngest)
	}
}

func TestPipelineForceSkipsLastIngest(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	content := "# Force Test\n\nContent for force test.\n"
	conn := &MockConnector{
		ConnectorName: "mock",
		Docs:          []model.RawDocument{makeDoc("docs/force.md", content, checksum(content))},
	}

	// Register a source with a LastIngest timestamp.
	forceLastIngest := time.Now().Add(-24 * time.Hour)
	err := s.RegisterSource(ctx, model.Source{
		SourceType: "mock",
		SourceName: "mock",
		Config:     map[string]string{},
		LastIngest: &forceLastIngest,
	})
	if err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}

	// Run with Force — LastIngest should NOT be passed to the connector.
	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)
	_, err = pipeline.Run(ctx, conn, ingest.Options{Force: true})
	if err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	if conn.LastScanOpts.LastIngest != nil {
		t.Errorf("expected nil LastIngest when Force is true, got %v", conn.LastScanOpts.LastIngest)
	}
}

func TestPipelineZeroLastIngestSource(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	content := "# Zero Test\n\nContent for zero LastIngest test.\n"
	conn := &MockConnector{
		ConnectorName: "mock",
		Docs:          []model.RawDocument{makeDoc("docs/zero.md", content, checksum(content))},
	}

	// Register a source WITHOUT setting LastIngest (zero value).
	err := s.RegisterSource(ctx, model.Source{
		SourceType: "mock",
		SourceName: "mock",
		Config:     map[string]string{},
	})
	if err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}

	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)
	_, err = pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	// Source exists but LastIngest is nil — should NOT pass LastIngest to connector.
	if conn.LastScanOpts.LastIngest != nil {
		t.Errorf("expected nil LastIngest for source with nil LastIngest, got %v", conn.LastScanOpts.LastIngest)
	}
}

func TestStreamingIngestion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	mdContent := "# Streaming Test\n\nThis document tests streaming ingestion.\n"
	goContent := "package main\n\nfunc main() {}\n"
	yamlContent := "key: value\nnested:\n  a: 1\n  b: 2\n"

	conn := &MockConnector{
		ConnectorName: "mock",
		Docs: []model.RawDocument{
			makeDoc("docs/streaming.md", mdContent, checksum(mdContent)),
			makeDoc("pkg/main.go", goContent, checksum(goContent)),
			makeDoc("config/app.yaml", yamlContent, checksum(yamlContent)),
		},
	}

	// The MockConnector implements StreamingConnector, so the pipeline
	// should use the streaming path (enrichment is nil by default).
	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)
	result, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Pipeline.Run (streaming): %v", err)
	}

	if result.Added == 0 {
		t.Fatal("expected fragments to be added via streaming path, got 0")
	}
	t.Logf("Streaming ingestion: added=%d deleted=%d skipped=%d errors=%d",
		result.Added, result.Deleted, result.Skipped, result.Errors)

	// Verify fragments are searchable.
	checksums, err := s.GetChecksums(ctx, "mock", "mock")
	if err != nil {
		t.Fatalf("GetChecksums: %v", err)
	}
	if len(checksums) != 3 {
		t.Fatalf("expected 3 path checksums, got %d", len(checksums))
	}

	// Re-ingest: everything should be skipped.
	result2, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Pipeline.Run (streaming, re-ingest): %v", err)
	}
	if result2.Added != 0 {
		t.Fatalf("expected 0 added on re-ingest, got %d", result2.Added)
	}
	if result2.Skipped == 0 {
		t.Fatal("expected skipped > 0 on re-ingest")
	}
	t.Logf("Streaming re-ingest: added=%d skipped=%d", result2.Added, result2.Skipped)
}

func TestStreamingDeletions(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	content1 := "# Doc One\n\nFirst document.\n"
	content2 := "# Doc Two\n\nSecond document.\n"

	conn := &MockConnector{
		ConnectorName: "mock",
		Docs: []model.RawDocument{
			makeDoc("docs/one.md", content1, checksum(content1)),
			makeDoc("docs/two.md", content2, checksum(content2)),
		},
	}

	pipeline := ingest.NewPipeline(s, embedder, registry, 2, nil)

	// Initial ingest.
	result1, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("First Run: %v", err)
	}
	if result1.Added == 0 {
		t.Fatal("first ingestion should add fragments")
	}

	// Remove one document.
	conn.Docs = []model.RawDocument{
		makeDoc("docs/one.md", content1, checksum(content1)),
	}

	result2, err := pipeline.Run(ctx, conn)
	if err != nil {
		t.Fatalf("Second Run: %v", err)
	}
	if result2.Deleted == 0 {
		t.Fatal("expected deletions after removing a file")
	}

	// Verify only one document remains.
	checksums, err := s.GetChecksums(ctx, "mock", "mock")
	if err != nil {
		t.Fatalf("GetChecksums: %v", err)
	}
	if _, exists := checksums["docs/two.md"]; exists {
		t.Fatal("docs/two.md should have been deleted")
	}
	t.Logf("After streaming deletion: added=%d deleted=%d skipped=%d",
		result2.Added, result2.Deleted, result2.Skipped)
}

func TestStreamingMatchesNonStreaming(t *testing.T) {
	ctx := context.Background()
	embedder := NewMockEmbedder(embeddingDim)
	registry := newTestRegistry()

	mdContent := "# Comparison Test\n\nThis document compares streaming vs non-streaming.\n"

	// Run via streaming path (MockConnector implements StreamingConnector).
	s1 := newTestStore(t)
	conn1 := &MockConnector{
		ConnectorName: "mock",
		Docs: []model.RawDocument{
			makeDoc("docs/compare.md", mdContent, checksum(mdContent)),
		},
	}
	pipeline1 := ingest.NewPipeline(s1, embedder, registry, 2, nil)
	result1, err := pipeline1.Run(ctx, conn1)
	if err != nil {
		t.Fatalf("Streaming Run: %v", err)
	}

	// Run via non-streaming path (use enrichment to force non-streaming).
	s2 := newTestStore(t)
	conn2 := &MockConnector{
		ConnectorName: "mock",
		Docs: []model.RawDocument{
			makeDoc("docs/compare.md", mdContent, checksum(mdContent)),
		},
	}
	pipeline2 := ingest.NewPipeline(s2, embedder, registry, 2, nil)
	// Set a dummy enrichment config to force the non-streaming path.
	pipeline2.SetEnrichment(ingest.EnrichmentConfig{
		Enricher: &noopEnricher{},
	})
	result2, err := pipeline2.Run(ctx, conn2)
	if err != nil {
		t.Fatalf("Non-streaming Run: %v", err)
	}

	if result1.Added != result2.Added {
		t.Errorf("Added mismatch: streaming=%d non-streaming=%d", result1.Added, result2.Added)
	}
	if result1.Errors != result2.Errors {
		t.Errorf("Errors mismatch: streaming=%d non-streaming=%d", result1.Errors, result2.Errors)
	}
	t.Logf("Streaming: added=%d, Non-streaming: added=%d", result1.Added, result2.Added)
}

// noopEnricher is an enricher that returns content unchanged (used to force non-streaming path).
type noopEnricher struct{}

func (e *noopEnricher) Enrich(_ context.Context, chunk model.Chunk, _, _ []model.Chunk) (string, error) {
	return chunk.Content, nil
}
func (e *noopEnricher) Model() string { return "noop" }
