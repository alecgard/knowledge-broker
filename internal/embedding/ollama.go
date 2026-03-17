package embedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	defaultOllamaBaseURL   = "http://localhost:11434"
	defaultOllamaModel     = "nomic-embed-text"
	defaultOllamaDimension = 768
	embCacheMaxSize        = 512

	// Retry settings for empty embedding responses.
	embedMaxRetries = 3
)

// embCacheEntry holds a cached embedding with its creation time for LRU eviction.
type embCacheEntry struct {
	vec       []float32
	createdAt time.Time
}

// OllamaEmbedder implements Embedder using the Ollama embedding API.
type OllamaEmbedder struct {
	baseURL    string
	model      string
	dimension  int
	httpClient *http.Client

	cacheMu sync.RWMutex
	cache   map[string]embCacheEntry

	healthMu        sync.Mutex
	healthErr       error
	healthCheckedAt time.Time
	healthFlight    singleflight.Group
}

const healthCacheTTL = 5 * time.Second

// NewOllamaEmbedder creates an OllamaEmbedder with sensible defaults.
// If baseURL is empty, "http://localhost:11434" is used.
// If model is empty, "nomic-embed-text" is used.
// If dimension is 0, 768 is used.
// If httpClient is nil, a default client is used.
func NewOllamaEmbedder(baseURL, model string, dimension int, httpClient *http.Client) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	if model == "" {
		model = defaultOllamaModel
	}
	if dimension == 0 {
		dimension = defaultOllamaDimension
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &OllamaEmbedder{
		baseURL:    baseURL,
		model:      model,
		dimension:  dimension,
		httpClient: httpClient,
		cache:      make(map[string]embCacheEntry, embCacheMaxSize),
	}
}

// CheckHealth verifies that Ollama is reachable by sending a GET request to its
// base URL. Results are cached for 5 seconds so that frequent callers (e.g.
// load balancer health probes) don't hammer Ollama. Concurrent callers are
// coalesced via singleflight so only one request is in-flight at a time.
func (o *OllamaEmbedder) CheckHealth(ctx context.Context) error {
	o.healthMu.Lock()
	if time.Since(o.healthCheckedAt) < healthCacheTTL {
		err := o.healthErr
		o.healthMu.Unlock()
		return err
	}
	o.healthMu.Unlock()

	v, err, _ := o.healthFlight.Do("health", func() (any, error) {
		return nil, o.doHealthCheck(ctx)
	})
	_ = v

	o.healthMu.Lock()
	o.healthErr = err
	o.healthCheckedAt = time.Now()
	o.healthMu.Unlock()

	return err
}

func (o *OllamaEmbedder) doHealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL, nil)
	if err != nil {
		return fmt.Errorf("Ollama not reachable at %s — is it running? Start it with: ollama serve", o.baseURL)
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Ollama not reachable at %s — is it running? Start it with: ollama serve", o.baseURL)
	}
	resp.Body.Close()
	return nil
}

// ollamaEmbedRequest is the JSON body sent to /api/embed.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

// ollamaEmbedResponse is the JSON response from /api/embed.
type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float64 `json:"embeddings"`
	Error      string      `json:"error,omitempty"`
}

// embCacheKey returns a hex-encoded SHA-256 hash of the text for use as a cache key.
func embCacheKey(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}

// cacheGet retrieves an embedding from the cache. Returns nil on miss.
func (o *OllamaEmbedder) cacheGet(key string) []float32 {
	o.cacheMu.RLock()
	defer o.cacheMu.RUnlock()
	if entry, ok := o.cache[key]; ok {
		return entry.vec
	}
	return nil
}

// cachePut stores an embedding in the cache, evicting the oldest entry if at capacity.
func (o *OllamaEmbedder) cachePut(key string, vec []float32) {
	o.cacheMu.Lock()
	defer o.cacheMu.Unlock()

	if len(o.cache) >= embCacheMaxSize {
		o.evictOldest()
	}
	o.cache[key] = embCacheEntry{vec: vec, createdAt: time.Now()}
}

// evictOldest removes the oldest cache entry. Must be called with cacheMu held.
func (o *OllamaEmbedder) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, e := range o.cache {
		if oldestKey == "" || e.createdAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.createdAt
		}
	}
	if oldestKey != "" {
		delete(o.cache, oldestKey)
	}
}

// truncateForLog returns the first 100 characters of s for use in log messages.
func truncateForLog(s string) string {
	if len(s) <= 100 {
		return s
	}
	return s[:100] + "..."
}

// isEmptyOrWhitespace reports whether s is empty or contains only whitespace.
func isEmptyOrWhitespace(s string) bool {
	return strings.TrimSpace(s) == ""
}

// embedWithRetry calls doEmbed and retries up to embedMaxRetries times when
// Ollama returns 0 embeddings. Backoff intervals: 100ms, 500ms, 2s.
func (o *OllamaEmbedder) embedWithRetry(ctx context.Context, input any) ([][]float32, error) {
	backoffs := []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, 2 * time.Second}

	vectors, err := o.doEmbed(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(vectors) > 0 {
		return vectors, nil
	}

	// Log the input that produced an empty response.
	inputSnippet := ""
	switch v := input.(type) {
	case string:
		inputSnippet = truncateForLog(v)
	case []string:
		if len(v) > 0 {
			inputSnippet = truncateForLog(v[0])
		}
	}
	slog.Warn("ollama returned 0 embeddings, retrying", "input_preview", inputSnippet)

	for attempt := 0; attempt < embedMaxRetries; attempt++ {
		wait := backoffs[attempt]
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}

		vectors, err = o.doEmbed(ctx, input)
		if err != nil {
			return nil, err
		}
		if len(vectors) > 0 {
			slog.Info("ollama embed retry succeeded", "attempt", attempt+1)
			return vectors, nil
		}
		slog.Warn("ollama embed retry still returned 0 embeddings", "attempt", attempt+1)
	}

	return nil, fmt.Errorf("response contained no embeddings after %d retries", embedMaxRetries)
}

// Embed returns the embedding vector for a single text.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if isEmptyOrWhitespace(text) {
		return nil, fmt.Errorf("ollama embed: input text is empty or whitespace-only")
	}

	key := embCacheKey(text)
	if cached := o.cacheGet(key); cached != nil {
		return cached, nil
	}

	vectors, err := o.embedWithRetry(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}

	o.cachePut(key, vectors[0])
	return vectors[0], nil
}

// EmbedBatch returns embeddings for multiple texts in a single request.
// Cached embeddings are returned directly; only uncached texts are sent to Ollama.
func (o *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))
	var uncachedTexts []string
	var uncachedIndices []int

	for i, text := range texts {
		if isEmptyOrWhitespace(text) {
			slog.Warn("skipping empty/whitespace-only text in batch", "index", i)
			results[i] = nil
			continue
		}
		key := embCacheKey(text)
		if cached := o.cacheGet(key); cached != nil {
			results[i] = cached
		} else {
			uncachedTexts = append(uncachedTexts, text)
			uncachedIndices = append(uncachedIndices, i)
		}
	}

	// All cached — nothing to fetch.
	if len(uncachedTexts) == 0 {
		return results, nil
	}

	// Fetch uncached embeddings from Ollama.
	var input any = uncachedTexts
	if len(uncachedTexts) == 1 {
		input = uncachedTexts[0]
	}
	vectors, err := o.embedWithRetry(ctx, input)
	if err != nil {
		// Batch request failed — fall back to embedding one-at-a-time so that
		// a single oversized text doesn't fail the entire batch.
		slog.Warn("batch embed failed, falling back to individual requests", "error", err, "count", len(uncachedTexts))
		vectors = make([][]float32, len(uncachedTexts))
		for i, text := range uncachedTexts {
			vec, singleErr := o.embedWithRetry(ctx, text)
			if singleErr != nil {
				slog.Warn("skipping text that failed to embed", "error", singleErr, "text_length", len(text), "input_preview", truncateForLog(text))
				vectors[i] = nil
			} else if len(vec) > 0 {
				vectors[i] = vec[0]
			}
		}

		// Place results and cache successful ones.
		for j, idx := range uncachedIndices {
			results[idx] = vectors[j]
			if vectors[j] != nil {
				o.cachePut(embCacheKey(uncachedTexts[j]), vectors[j])
			}
		}
		return results, nil
	}
	if len(vectors) != len(uncachedTexts) {
		return nil, fmt.Errorf("ollama embed batch: expected %d embeddings, got %d", len(uncachedTexts), len(vectors))
	}

	// Place results and cache them.
	for j, idx := range uncachedIndices {
		results[idx] = vectors[j]
		o.cachePut(embCacheKey(uncachedTexts[j]), vectors[j])
	}

	return results, nil
}

// Dimension returns the configured embedding dimension.
func (o *OllamaEmbedder) Dimension() int {
	return o.dimension
}

// doEmbed sends a request to the Ollama /api/embed endpoint.
// input can be a string (single) or []string (batch).
func (o *OllamaEmbedder) doEmbed(ctx context.Context, input any) ([][]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model: o.model,
		Input: input,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := o.baseURL + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBytes))
	}

	var embedResp ollamaEmbedResponse
	if err := json.Unmarshal(respBytes, &embedResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if embedResp.Error != "" {
		return nil, fmt.Errorf("api error: %s", embedResp.Error)
	}

	// Convert [][]float64 to [][]float32.
	result := make([][]float32, len(embedResp.Embeddings))
	for i, vec := range embedResp.Embeddings {
		f32 := make([]float32, len(vec))
		for j, v := range vec {
			f32[j] = float32(v)
		}
		result[i] = f32
	}
	return result, nil
}
