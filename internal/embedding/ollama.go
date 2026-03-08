package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	defaultOllamaBaseURL   = "http://localhost:11434"
	defaultOllamaModel     = "nomic-embed-text"
	defaultOllamaDimension = 768
)

// OllamaEmbedder implements Embedder using the Ollama embedding API.
type OllamaEmbedder struct {
	baseURL    string
	model      string
	dimension  int
	httpClient *http.Client
}

// NewOllamaEmbedder creates an OllamaEmbedder with sensible defaults.
// If baseURL is empty, "http://localhost:11434" is used.
// If model is empty, "nomic-embed-text" is used.
// If dimension is 0, 768 is used.
// If httpClient is nil, a default client is used.
func NewOllamaEmbedder(baseURL, model string, dimension int, httpClient ...*http.Client) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	if model == "" {
		model = defaultOllamaModel
	}
	if dimension == 0 {
		dimension = defaultOllamaDimension
	}
	var client *http.Client
	if len(httpClient) > 0 && httpClient[0] != nil {
		client = httpClient[0]
	} else {
		client = &http.Client{}
	}
	return &OllamaEmbedder{
		baseURL:    baseURL,
		model:      model,
		dimension:  dimension,
		httpClient: client,
	}
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

// Embed returns the embedding vector for a single text.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := o.doEmbed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("ollama embed: response contained no embeddings")
	}
	return vectors[0], nil
}

// EmbedBatch returns embeddings for multiple texts in a single request.
func (o *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	vectors, err := o.doEmbed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("ollama embed batch: %w", err)
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("ollama embed batch: expected %d embeddings, got %d", len(texts), len(vectors))
	}
	return vectors, nil
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
