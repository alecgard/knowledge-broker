package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

const testEmbeddingDim = 8

// mockEmbedder returns deterministic fake embeddings.
type mockEmbedder struct {
	dim int
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return m.hashToVector(text), nil
}

func (m *mockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		vecs[i] = m.hashToVector(t)
	}
	return vecs, nil
}

func (m *mockEmbedder) Dimension() int {
	return m.dim
}

func (m *mockEmbedder) CheckHealth(_ context.Context) error {
	return nil
}

func (m *mockEmbedder) hashToVector(text string) []float32 {
	h := sha256.Sum256([]byte(text))
	vec := make([]float32, m.dim)
	for i := 0; i < m.dim; i++ {
		idx := (i * 2) % len(h)
		val := float64(uint16(h[idx])<<8|uint16(h[(idx+1)%len(h)])) / 65536.0
		vec[i] = float32(val)
	}
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

func newTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewSQLiteStore(dbPath, testEmbeddingDim)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestHandleIngest(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	srv := NewHTTPServer(nil, emb, st, nil)

	reqBody := model.IngestRequest{
		Fragments: []model.IngestFragment{
			{
				Content:      "This is a test fragment about authentication.",
				SourceType:   "filesystem",
				SourcePath:   "docs/auth.md",
				SourceURI:    "file://docs/auth.md",
				ContentDate: time.Now().UTC(),
				Author:       "test-author",
				FileType:     ".md",
				Checksum:     "abc123",
			},
			{
				Content:      "Another fragment about authorization and roles.",
				SourceType:   "filesystem",
				SourcePath:   "docs/authz.md",
				SourceURI:    "file://docs/authz.md",
				ContentDate: time.Now().UTC(),
				Author:       "test-author",
				FileType:     ".md",
				Checksum:     "def456",
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["ingested"] != 2 {
		t.Fatalf("expected 2 ingested, got %d", resp["ingested"])
	}

	// Verify fragments are stored by checking checksums.
	checksums, err := st.GetChecksums(context.Background(), "filesystem", "")
	if err != nil {
		t.Fatalf("GetChecksums: %v", err)
	}
	if len(checksums) != 2 {
		t.Fatalf("expected 2 checksums, got %d", len(checksums))
	}

	// Verify vector search works on the ingested fragments.
	queryVec, _ := emb.Embed(context.Background(), "authentication")
	fragments, err := st.SearchByVector(context.Background(), queryVec, 5)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(fragments) == 0 {
		t.Fatal("expected vector search to return fragments")
	}
}

func TestHandleIngestWithDeletions(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	srv := NewHTTPServer(nil, emb, st, nil)

	// First, ingest a fragment.
	reqBody := model.IngestRequest{
		Fragments: []model.IngestFragment{
			{
				Content:      "Document to be deleted later.",
				SourceType:   "github",
				SourcePath:   "docs/old.md",
				SourceURI:    "https://github.com/test/repo/blob/main/docs/old.md",
				ContentDate: time.Now().UTC(),
				Author:       "alice",
				FileType:     ".md",
				Checksum:     "xyz789",
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ingest failed: %d: %s", rec.Code, rec.Body.String())
	}

	// Verify fragment exists.
	checksums, _ := st.GetChecksums(context.Background(), "github", "")
	if len(checksums) != 1 {
		t.Fatalf("expected 1 checksum, got %d", len(checksums))
	}

	// Now send a deletion request.
	deleteReq := model.IngestRequest{
		Deleted: []model.IngestDeletedPath{
			{SourceType: "github", Path: "docs/old.md"},
		},
	}
	body, _ = json.Marshal(deleteReq)
	req = httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete request failed: %d: %s", rec.Code, rec.Body.String())
	}

	// Verify fragment is gone.
	checksums, _ = st.GetChecksums(context.Background(), "github", "")
	if len(checksums) != 0 {
		t.Fatalf("expected 0 checksums after deletion, got %d", len(checksums))
	}
}

func TestHandleIngestMethodNotAllowed(t *testing.T) {
	srv := NewHTTPServer(nil, &mockEmbedder{dim: testEmbeddingDim}, newTestStore(t), nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/ingest", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleIngestEmptyBody(t *testing.T) {
	srv := NewHTTPServer(nil, &mockEmbedder{dim: testEmbeddingDim}, newTestStore(t), nil)

	body, _ := json.Marshal(model.IngestRequest{})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleIngestMultipleFragmentsSamePath(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	srv := NewHTTPServer(nil, emb, st, nil)

	// Simulate multiple chunks from the same file (different chunk indices).
	reqBody := model.IngestRequest{
		Fragments: []model.IngestFragment{
			{
				Content:    "First chunk of a long document.",
				SourceType: "filesystem",
				SourcePath: "docs/long.md",
				Checksum:   "same123",
			},
			{
				Content:    "Second chunk of a long document.",
				SourceType: "filesystem",
				SourcePath: "docs/long.md",
				Checksum:   "same123",
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]int
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["ingested"] != 2 {
		t.Fatalf("expected 2 ingested, got %d", resp["ingested"])
	}
}

func TestHandleHealth(t *testing.T) {
	srv := NewHTTPServer(nil, &mockEmbedder{dim: testEmbeddingDim}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", resp["status"])
	}
}

func TestHandleIngestDeduplication(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	srv := NewHTTPServer(nil, emb, st, nil)

	frag := model.IngestFragment{
		Content:    "Dedup test content.",
		SourceType: "filesystem",
		SourcePath: "docs/dedup.md",
		Checksum:   "dedup-checksum",
	}

	// First ingest.
	reqBody := model.IngestRequest{Fragments: []model.IngestFragment{frag}}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first ingest: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp1 map[string]int
	json.Unmarshal(rec.Body.Bytes(), &resp1)
	if resp1["ingested"] != 1 {
		t.Fatalf("first ingest: expected 1 ingested, got %d", resp1["ingested"])
	}

	// Second ingest with same checksum should be skipped.
	body, _ = json.Marshal(reqBody)
	req = httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second ingest: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp2 map[string]int
	json.Unmarshal(rec.Body.Bytes(), &resp2)
	if resp2["ingested"] != 0 {
		t.Fatalf("second ingest: expected 0 ingested (dedup), got %d", resp2["ingested"])
	}
	if resp2["skipped"] != 1 {
		t.Fatalf("second ingest: expected 1 skipped, got %d", resp2["skipped"])
	}
}

func TestHandleIngestConcurrent(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	srv := NewHTTPServer(nil, emb, st, nil)

	const numWorkers = 8
	errs := make(chan error, numWorkers)

	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			reqBody := model.IngestRequest{
				Fragments: []model.IngestFragment{
					{
						Content:    fmt.Sprintf("Worker %d fragment 1", workerID),
						SourceType: "github",
						SourceName: fmt.Sprintf("repo-%d", workerID),
						SourcePath: fmt.Sprintf("worker%d/file1.md", workerID),
						Checksum:   fmt.Sprintf("w%d-c1", workerID),
					},
					{
						Content:    fmt.Sprintf("Worker %d fragment 2", workerID),
						SourceType: "github",
						SourceName: fmt.Sprintf("repo-%d", workerID),
						SourcePath: fmt.Sprintf("worker%d/file2.md", workerID),
						Checksum:   fmt.Sprintf("w%d-c2", workerID),
					},
				},
			}
			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				errs <- fmt.Errorf("worker %d: status %d: %s", workerID, rec.Code, rec.Body.String())
				return
			}
			var resp map[string]int
			json.Unmarshal(rec.Body.Bytes(), &resp)
			if resp["ingested"] != 2 {
				errs <- fmt.Errorf("worker %d: expected 2 ingested, got %d", workerID, resp["ingested"])
				return
			}
			errs <- nil
		}(w)
	}

	for i := 0; i < numWorkers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent ingest: %v", err)
		}
	}

	// Verify total fragment count.
	counts, err := st.CountFragmentsBySource(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	want := numWorkers * 2
	if total != want {
		t.Errorf("expected %d fragments, got %d", want, total)
	}
}

// Verify that fragment IDs generated by the server match the expected algorithm.
func TestIngestFragmentIDConsistency(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	srv := NewHTTPServer(nil, emb, st, nil)

	reqBody := model.IngestRequest{
		Fragments: []model.IngestFragment{
			{
				Content:    "Test content",
				SourceType: "github",
				SourcePath: "path/to/file.go",
				Checksum:   "abc",
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// The server should generate an ID using sha256("github:path/to/file.go:0")[:16].
	// Verify the fragment is retrievable by that expected ID.
	h := sha256.Sum256([]byte("github:path/to/file.go:0"))
	wantID := fmt.Sprintf("%x", h)[:16]
	fragments, err := st.GetFragments(context.Background(), []string{wantID})
	if err != nil {
		t.Fatalf("GetFragments: %v", err)
	}
	if len(fragments) != 1 {
		t.Fatalf("expected 1 fragment with ID %s, got %d", wantID, len(fragments))
	}
	if fragments[0].RawContent != "Test content" {
		t.Fatalf("unexpected content: %s", fragments[0].RawContent)
	}
}

// ---------------------------------------------------------------------------
// Query endpoint tests
// ---------------------------------------------------------------------------

func TestHandleQueryMethodNotAllowed(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	eng := query.NewEngine(st, emb, nil, 20)
	srv := NewHTTPServer(eng, emb, st, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/query", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleQueryEmptyMessages(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	eng := query.NewEngine(st, emb, nil, 20)
	srv := NewHTTPServer(eng, emb, st, nil)

	body, _ := json.Marshal(model.QueryRequest{Messages: []model.Message{}})
	req := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleQueryInvalidJSON(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	eng := query.NewEngine(st, emb, nil, 20)
	srv := NewHTTPServer(eng, emb, st, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader([]byte("{bad json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleQueryRawMode(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	// Insert fragments directly into the store.
	vec, _ := emb.Embed(context.Background(), "authentication login security")
	frags := []model.SourceFragment{
		{
			ID:           "frag-raw-1",
			RawContent:   "Authentication is handled via OAuth2 tokens.",
			SourceType:   "filesystem",
			SourceName:   "docs",
			SourcePath:   "docs/auth.md",
			SourceURI:    "file://docs/auth.md",
			ContentDate: time.Now().UTC(),
			Author:       "alice",
			FileType:     ".md",
			Checksum:     "raw1",
			Embedding:    vec,
		},
	}
	if err := st.UpsertFragments(context.Background(), frags); err != nil {
		t.Fatalf("UpsertFragments: %v", err)
	}

	eng := query.NewEngine(st, emb, nil, 20)
	srv := NewHTTPServer(eng, emb, st, nil)

	queryReq := model.QueryRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "authentication login security"}},
		Mode:     model.ModeRaw,
	}
	body, _ := json.Marshal(queryReq)
	req := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result model.RawResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(result.Fragments) == 0 {
		t.Fatal("expected at least one fragment in raw result")
	}

	found := false
	for _, f := range result.Fragments {
		if f.FragmentID == "frag-raw-1" {
			found = true
			if f.Content != "Authentication is handled via OAuth2 tokens." {
				t.Fatalf("unexpected content: %s", f.Content)
			}
			if f.Confidence.Overall <= 0 {
				t.Fatalf("expected positive overall confidence, got %f", f.Confidence.Overall)
			}
		}
	}
	if !found {
		t.Fatal("expected to find fragment frag-raw-1 in results")
	}
}

func TestHandleQueryNoLLM(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	eng := query.NewEngine(st, emb, nil, 20) // nil LLM
	srv := NewHTTPServer(eng, emb, st, nil)

	queryReq := model.QueryRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "how does auth work?"}},
		// No mode specified -> defaults to synthesis, which needs LLM.
	}
	body, _ := json.Marshal(queryReq)
	req := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	respBody := rec.Body.String()
	if !strings.Contains(respBody, "ANTHROPIC_API_KEY") {
		t.Fatalf("expected error mentioning ANTHROPIC_API_KEY, got: %s", respBody)
	}
}

func TestHandleUpdateSource(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	srv := NewHTTPServer(nil, emb, st, nil)

	// Register a source first.
	if err := st.RegisterSource(context.Background(), model.Source{
		SourceType: "filesystem",
		SourceName: "my-docs",
		Config:     map[string]string{"path": "/docs"},
		LastIngest: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}

	t.Run("MethodNotAllowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/v1/sources", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("SetDescription", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"source_type": "filesystem",
			"source_name": "my-docs",
			"description": "My documentation",
		})
		req := httptest.NewRequest(http.MethodPatch, "/v1/sources", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]string
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["status"] != "ok" {
			t.Fatalf("expected status ok, got %s", resp["status"])
		}

		// Verify the description was set.
		src, err := st.GetSource(context.Background(), "filesystem", "my-docs")
		if err != nil {
			t.Fatalf("GetSource: %v", err)
		}
		if src.Description != "My documentation" {
			t.Fatalf("expected description 'My documentation', got %q", src.Description)
		}
	})

	t.Run("ConflictWithoutForce", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"source_type": "filesystem",
			"source_name": "my-docs",
			"description": "New description",
		})
		req := httptest.NewRequest(http.MethodPatch, "/v1/sources", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "already has a description") {
			t.Fatalf("expected conflict message, got: %s", rec.Body.String())
		}
	})

	t.Run("OverwriteWithForce", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"source_type": "filesystem",
			"source_name": "my-docs",
			"description": "Overwritten description",
			"force":       true,
		})
		req := httptest.NewRequest(http.MethodPatch, "/v1/sources", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		src, err := st.GetSource(context.Background(), "filesystem", "my-docs")
		if err != nil {
			t.Fatalf("GetSource: %v", err)
		}
		if src.Description != "Overwritten description" {
			t.Fatalf("expected 'Overwritten description', got %q", src.Description)
		}
	})

	t.Run("MissingFields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"description": "something",
		})
		req := httptest.NewRequest(http.MethodPatch, "/v1/sources", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandleQueryRawModeWithFilters(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	// Insert fragments from different sources and source types.
	content := "shared deployment guide content for filtering test"
	vec, _ := emb.Embed(context.Background(), content)

	frags := []model.SourceFragment{
		{
			ID:           "frag-filter-1",
			RawContent:   content,
			SourceType:   "filesystem",
			SourceName:   "local-docs",
			SourcePath:   "docs/deploy.md",
			SourceURI:    "file://docs/deploy.md",
			ContentDate: time.Now().UTC(),
			FileType:     ".md",
			Checksum:     "filter1",
			Embedding:    vec,
		},
		{
			ID:           "frag-filter-2",
			RawContent:   content,
			SourceType:   "github",
			SourceName:   "acme/platform",
			SourcePath:   "docs/deploy.md",
			SourceURI:    "https://github.com/acme/platform/blob/main/docs/deploy.md",
			ContentDate: time.Now().UTC(),
			FileType:     ".md",
			Checksum:     "filter2",
			Embedding:    vec,
		},
		{
			ID:           "frag-filter-3",
			RawContent:   content,
			SourceType:   "github",
			SourceName:   "acme/infra",
			SourcePath:   "infra/deploy.md",
			SourceURI:    "https://github.com/acme/infra/blob/main/infra/deploy.md",
			ContentDate: time.Now().UTC(),
			FileType:     ".md",
			Checksum:     "filter3",
			Embedding:    vec,
		},
	}
	if err := st.UpsertFragments(context.Background(), frags); err != nil {
		t.Fatalf("UpsertFragments: %v", err)
	}

	eng := query.NewEngine(st, emb, nil, 20)
	srv := NewHTTPServer(eng, emb, st, nil)

	// Sub-test: filter by source_types to only get github results.
	t.Run("FilterBySourceType", func(t *testing.T) {
		queryReq := model.QueryRequest{
			Messages:    []model.Message{{Role: model.RoleUser, Content: content}},
			Mode:        model.ModeRaw,
			SourceTypes: []string{"github"},
		}
		body, _ := json.Marshal(queryReq)
		req := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result model.RawResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		for _, f := range result.Fragments {
			if f.SourceType != "github" {
				t.Fatalf("expected only github fragments, got source_type=%s", f.SourceType)
			}
		}
		if len(result.Fragments) != 2 {
			t.Fatalf("expected 2 github fragments, got %d", len(result.Fragments))
		}
	})

	// Sub-test: filter by source name.
	t.Run("FilterBySourceName", func(t *testing.T) {
		queryReq := model.QueryRequest{
			Messages: []model.Message{{Role: model.RoleUser, Content: content}},
			Mode:     model.ModeRaw,
			Sources:  []string{"acme/platform"},
		}
		body, _ := json.Marshal(queryReq)
		req := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result model.RawResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if len(result.Fragments) != 1 {
			t.Fatalf("expected 1 fragment for acme/platform, got %d", len(result.Fragments))
		}
		if result.Fragments[0].SourceName != "acme/platform" {
			t.Fatalf("expected source_name=acme/platform, got %s", result.Fragments[0].SourceName)
		}
	})

	// Sub-test: combine source_types and sources filters.
	t.Run("FilterBySourceTypeAndName", func(t *testing.T) {
		queryReq := model.QueryRequest{
			Messages:    []model.Message{{Role: model.RoleUser, Content: content}},
			Mode:        model.ModeRaw,
			SourceTypes: []string{"github"},
			Sources:     []string{"acme/infra"},
		}
		body, _ := json.Marshal(queryReq)
		req := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result model.RawResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if len(result.Fragments) != 1 {
			t.Fatalf("expected 1 fragment, got %d", len(result.Fragments))
		}
		if result.Fragments[0].SourceName != "acme/infra" {
			t.Fatalf("expected source_name=acme/infra, got %s", result.Fragments[0].SourceName)
		}
	})
}
