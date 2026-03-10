package embedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	defaultOllamaBaseURL   = "http://localhost:11434"
	defaultOllamaModel     = "nomic-embed-text"
	defaultOllamaDimension = 768
	embCacheMaxSize        = 512
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
}

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
// base URL. It uses a short timeout so callers get a fast, clear error instead
// of waiting for the full HTTP client timeout.
func (o *OllamaEmbedder) CheckHealth(ctx context.Context) error {
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

// Embed returns the embedding vector for a single text.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	key := embCacheKey(text)
	if cached := o.cacheGet(key); cached != nil {
		return cached, nil
	}

	vectors, err := o.doEmbed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("ollama embed: response contained no embeddings")
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
	vectors, err := o.doEmbed(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("ollama embed batch: %w", err)
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
