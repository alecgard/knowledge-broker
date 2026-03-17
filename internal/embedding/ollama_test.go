package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewOllamaEmbedderDefaults(t *testing.T) {
	e := NewOllamaEmbedder("", "", 0, nil)
	if e.baseURL != defaultOllamaBaseURL {
		t.Errorf("baseURL = %q, want %q", e.baseURL, defaultOllamaBaseURL)
	}
	if e.model != defaultOllamaModel {
		t.Errorf("model = %q, want %q", e.model, defaultOllamaModel)
	}
	if e.dimension != defaultOllamaDimension {
		t.Errorf("dimension = %d, want %d", e.dimension, defaultOllamaDimension)
	}
}

func TestNewOllamaEmbedderCustom(t *testing.T) {
	e := NewOllamaEmbedder("http://example.com:1234", "mxbai-embed-large", 1024, nil)
	if e.baseURL != "http://example.com:1234" {
		t.Errorf("baseURL = %q, want %q", e.baseURL, "http://example.com:1234")
	}
	if e.model != "mxbai-embed-large" {
		t.Errorf("model = %q, want %q", e.model, "mxbai-embed-large")
	}
	if e.Dimension() != 1024 {
		t.Errorf("Dimension() = %d, want %d", e.Dimension(), 1024)
	}
}

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "nomic-embed-text" {
			t.Errorf("model = %q, want %q", req.Model, "nomic-embed-text")
		}

		resp := ollamaEmbedResponse{
			Model:      "nomic-embed-text",
			Embeddings: [][]float64{{0.1, 0.2, 0.3}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 3, nil)
	vec, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("len(vec) = %d, want 3", len(vec))
	}
	expected := []float32{0.1, 0.2, 0.3}
	for i, v := range vec {
		if v != expected[i] {
			t.Errorf("vec[%d] = %f, want %f", i, v, expected[i])
		}
	}
}

func TestEmbed_CacheHit(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := ollamaEmbedResponse{
			Model:      "nomic-embed-text",
			Embeddings: [][]float64{{0.1, 0.2, 0.3}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 3, nil)

	// First call: cache miss, hits server.
	vec1, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 server call, got %d", callCount)
	}

	// Second call: cache hit, should NOT hit server.
	vec2, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed() error on cache hit: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected server call count to remain 1, got %d", callCount)
	}

	// Verify the vectors match.
	for i := range vec1 {
		if vec1[i] != vec2[i] {
			t.Errorf("vec1[%d]=%f != vec2[%d]=%f", i, vec1[i], i, vec2[i])
		}
	}

	// Different text: cache miss, hits server.
	_, err = e.Embed(context.Background(), "different text")
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 server calls, got %d", callCount)
	}
}

func TestEmbedBatch_CachePartialHit(t *testing.T) {
	var requestedInputs []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requestedInputs = append(requestedInputs, req.Input)

		// Return embeddings based on what was requested.
		var embeddings [][]float64
		switch v := req.Input.(type) {
		case string:
			embeddings = [][]float64{{1.0, 1.0}}
		case []any:
			for range v {
				embeddings = append(embeddings, []float64{1.0, 1.0})
			}
		}
		resp := ollamaEmbedResponse{
			Model:      "nomic-embed-text",
			Embeddings: embeddings,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 2, nil)

	// Pre-populate cache by embedding "hello".
	_, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if len(requestedInputs) != 1 {
		t.Fatalf("expected 1 server call, got %d", len(requestedInputs))
	}

	// Batch with "hello" (cached) and "world" (uncached).
	vecs, err := e.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedBatch() error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	// Should have made exactly one more server call (for "world" only).
	if len(requestedInputs) != 2 {
		t.Fatalf("expected 2 total server calls, got %d", len(requestedInputs))
	}

	// Batch where all are cached.
	vecs, err = e.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedBatch() error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	// No new server calls.
	if len(requestedInputs) != 2 {
		t.Fatalf("expected server call count to remain 2, got %d", len(requestedInputs))
	}
}

func TestEmbedBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		// input should be an array of strings for batch.
		inputs, ok := req.Input.([]any)
		if !ok {
			t.Fatalf("expected input to be []any, got %T", req.Input)
		}
		if len(inputs) != 2 {
			t.Fatalf("expected 2 inputs, got %d", len(inputs))
		}

		resp := ollamaEmbedResponse{
			Model: "nomic-embed-text",
			Embeddings: [][]float64{
				{1.0, 2.0},
				{3.0, 4.0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 2, nil)
	vecs, err := e.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedBatch() error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("len(vecs) = %d, want 2", len(vecs))
	}
	if vecs[0][0] != 1.0 || vecs[0][1] != 2.0 {
		t.Errorf("vecs[0] = %v, want [1.0, 2.0]", vecs[0])
	}
	if vecs[1][0] != 3.0 || vecs[1][1] != 4.0 {
		t.Errorf("vecs[1] = %v, want [3.0, 4.0]", vecs[1])
	}
}

func TestEmbedBatchEmpty(t *testing.T) {
	e := NewOllamaEmbedder("http://localhost:11434", "", 0, nil)
	vecs, err := e.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch(nil) error: %v", err)
	}
	if vecs != nil {
		t.Errorf("EmbedBatch(nil) = %v, want nil", vecs)
	}
}

func TestEmbedServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 0, nil)
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestEmbedAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaEmbedResponse{
			Error: "model 'nonexistent' not found",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nonexistent", 0, nil)
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestEmbedBadResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not valid json`))
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 0, nil)
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestEmbedConnectionError(t *testing.T) {
	// Use a URL that will refuse connections.
	e := NewOllamaEmbedder("http://127.0.0.1:1", "", 0, nil)
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestEmbedBatch_FallbackOnBatchError(t *testing.T) {
	// Simulate a batch request that fails (e.g. 400 due to oversized input),
	// then individual requests where one succeeds and one fails.
	batchAttempted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		switch v := req.Input.(type) {
		case []any:
			// Batch request — fail with 400 to trigger fallback.
			batchAttempted = true
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"input length exceeds context length"}`))
		case string:
			// Individual request — fail for "too_long", succeed for others.
			if v == "too_long" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"input length exceeds context length"}`))
			} else {
				resp := ollamaEmbedResponse{
					Model:      "nomic-embed-text",
					Embeddings: [][]float64{{0.5, 0.6}},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}
		default:
			t.Fatalf("unexpected input type: %T", req.Input)
		}
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 2, nil)
	vecs, err := e.EmbedBatch(context.Background(), []string{"ok_text", "too_long"})
	if err != nil {
		t.Fatalf("EmbedBatch() should not return error on fallback, got: %v", err)
	}
	if !batchAttempted {
		t.Fatal("expected batch request to be attempted first")
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 result slots, got %d", len(vecs))
	}
	// "ok_text" should have a valid embedding.
	if vecs[0] == nil {
		t.Fatal("expected non-nil embedding for 'ok_text'")
	}
	if vecs[0][0] != 0.5 || vecs[0][1] != 0.6 {
		t.Errorf("vecs[0] = %v, want [0.5, 0.6]", vecs[0])
	}
	// "too_long" should have a nil embedding.
	if vecs[1] != nil {
		t.Fatalf("expected nil embedding for 'too_long', got %v", vecs[1])
	}
}

func TestEmbed_EmptyInput(t *testing.T) {
	e := NewOllamaEmbedder("http://localhost:11434", "", 0, nil)

	for _, input := range []string{"", "   ", "\t\n"} {
		_, err := e.Embed(context.Background(), input)
		if err == nil {
			t.Errorf("Embed(%q) should return error for empty/whitespace input", input)
		}
		if !strings.Contains(err.Error(), "empty or whitespace") {
			t.Errorf("error should mention empty/whitespace, got: %s", err.Error())
		}
	}
}

func TestEmbed_RetryOnZeroEmbeddings(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		var resp ollamaEmbedResponse
		if n <= 2 {
			// First two calls return 0 embeddings.
			resp = ollamaEmbedResponse{Model: "nomic-embed-text", Embeddings: [][]float64{}}
		} else {
			// Third call succeeds.
			resp = ollamaEmbedResponse{Model: "nomic-embed-text", Embeddings: [][]float64{{0.1, 0.2}}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 2, nil)
	vec, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() should succeed after retry, got: %v", err)
	}
	if len(vec) != 2 {
		t.Fatalf("expected 2-dim vector, got %d", len(vec))
	}
	// 1 initial + 2 retries = 3 calls
	if callCount.Load() != 3 {
		t.Errorf("expected 3 server calls (1 initial + 2 retries), got %d", callCount.Load())
	}
}

func TestEmbed_RetryExhausted(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		// Always return 0 embeddings.
		resp := ollamaEmbedResponse{Model: "nomic-embed-text", Embeddings: [][]float64{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 2, nil)
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed() should fail after all retries exhausted")
	}
	if !strings.Contains(err.Error(), "no embeddings after") {
		t.Errorf("error should mention retries, got: %s", err.Error())
	}
	// 1 initial + 3 retries = 4 calls
	if callCount.Load() != 4 {
		t.Errorf("expected 4 server calls (1 initial + 3 retries), got %d", callCount.Load())
	}
}

func TestEmbedBatch_SkipsEmptyTexts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		// Should only receive the non-empty text.
		resp := ollamaEmbedResponse{
			Model:      "nomic-embed-text",
			Embeddings: [][]float64{{0.5, 0.6}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 2, nil)
	vecs, err := e.EmbedBatch(context.Background(), []string{"hello", "", "  "})
	if err != nil {
		t.Fatalf("EmbedBatch() error: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("expected 3 result slots, got %d", len(vecs))
	}
	if vecs[0] == nil {
		t.Fatal("expected non-nil embedding for 'hello'")
	}
	if vecs[1] != nil {
		t.Errorf("expected nil for empty string, got %v", vecs[1])
	}
	if vecs[2] != nil {
		t.Errorf("expected nil for whitespace string, got %v", vecs[2])
	}
}

func TestTruncateForLog(t *testing.T) {
	short := "hello"
	if got := truncateForLog(short); got != "hello" {
		t.Errorf("truncateForLog(%q) = %q, want %q", short, got, "hello")
	}

	long := strings.Repeat("a", 200)
	got := truncateForLog(long)
	if len(got) != 103 { // 100 + "..."
		t.Errorf("truncateForLog(200 chars) length = %d, want 103", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncateForLog should end with '...', got: %s", got)
	}
}

func TestCheckHealth_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ollama is running"))
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "", 0, nil)
	err := e.CheckHealth(context.Background())
	if err != nil {
		t.Fatalf("CheckHealth() returned unexpected error: %v", err)
	}
}

func TestCheckHealth_Unreachable(t *testing.T) {
	e := NewOllamaEmbedder("http://127.0.0.1:1", "", 0, nil)
	err := e.CheckHealth(context.Background())
	if err == nil {
		t.Fatal("CheckHealth() should return error for unreachable server")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "Ollama not reachable") {
		t.Errorf("error should mention 'Ollama not reachable', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "http://127.0.0.1:1") {
		t.Errorf("error should contain the URL, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "ollama serve") {
		t.Errorf("error should contain 'ollama serve' hint, got: %s", errMsg)
	}
}
