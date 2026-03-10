package ingest_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"path/filepath"
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
		vecs[i] = m.hashToVector(t)
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
}

var _ connector.Connector = (*MockConnector)(nil)

func (m *MockConnector) Name() string             { return m.ConnectorName }
func (m *MockConnector) SourceName() string       { return m.ConnectorName }
func (m *MockConnector) Config(mode string) map[string]string { return map[string]string{} }

func (m *MockConnector) Scan(_ context.Context, opts connector.ScanOptions) ([]model.RawDocument, []string, error) {
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

// MockLLM satisfies query.LLM and returns a canned response that includes
// the KB_META JSON block.
type MockLLM struct{}

var _ query.LLM = (*MockLLM)(nil)

func (m *MockLLM) StreamAnswer(_ context.Context, _ string, _ []model.Message, onText func(string)) (string, error) {
	response := `Retry logic is implemented using exponential back-off with jitter.
The RetryWithBackoff function wraps any operation and retries it up to
a configurable maximum number of attempts.

---KB_META---
{
  "confidence": {
    "freshness": 0.9,
    "corroboration": 0.8,
    "consistency": 0.95,
    "authority": 0.7
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
		LastModified: time.Now().UTC(),
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
	if answer.Confidence.Freshness == 0 && answer.Confidence.Consistency == 0 {
		t.Fatal("expected confidence signals to be parsed from KB_META block")
	}
	t.Logf("Answer (first 100 chars): %.100s", answer.Content)
	t.Logf("Confidence: freshness=%.2f corroboration=%.2f consistency=%.2f authority=%.2f",
		answer.Confidence.Freshness, answer.Confidence.Corroboration,
		answer.Confidence.Consistency, answer.Confidence.Authority)
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
