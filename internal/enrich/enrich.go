// Package enrich provides chunk enrichment via LLM-based entity resolution
// and contextual grounding. During ingestion, chunks are rewritten to be
// self-contained by resolving pronouns and injecting essential context from
// surrounding chunks.
package enrich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

const (
	defaultBaseURL = "http://localhost:11434"
)

// Supported prompt versions.
const (
	PromptV1 = "v1" // full rewrite: resolve pronouns, inject context
	PromptV2 = "v2" // append-only: keep original text, append resolved entities and keywords

	// DefaultPromptVersion is the default when none is specified.
	DefaultPromptVersion = PromptV2
)

// PromptVersion is the currently active prompt version. It is set by the CLI
// based on --prompt-version flag, and used for cache keying and metadata.
var PromptVersion = DefaultPromptVersion

// Enricher rewrites chunks to be self-contained using an LLM.
type Enricher interface {
	// Enrich rewrites a single chunk using its surrounding context window.
	// prev are the preceding chunks (up to hprev), next are the following chunks (up to hnext).
	Enrich(ctx context.Context, chunk model.Chunk, prev, next []model.Chunk) (string, error)

	// Model returns the model identifier used for enrichment.
	Model() string
}

// OllamaEnricher implements Enricher using the Ollama chat API.
type OllamaEnricher struct {
	baseURL       string
	model         string
	promptVersion string
	httpClient    *http.Client
	logger        *slog.Logger
}

// NewOllamaEnricher creates an enricher backed by Ollama.
func NewOllamaEnricher(baseURL, model, promptVersion string, httpClient *http.Client, logger *slog.Logger) *OllamaEnricher {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if model == "" {
		panic("enrich: model must be provided (set KB_ENRICH_MODEL or pass --enrich-model)")
	}
	if promptVersion == "" {
		promptVersion = DefaultPromptVersion
	}
	PromptVersion = promptVersion
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &OllamaEnricher{
		baseURL:       strings.TrimRight(baseURL, "/"),
		model:         model,
		promptVersion: promptVersion,
		httpClient:    httpClient,
		logger:        logger,
	}
}

func (e *OllamaEnricher) Model() string { return e.model }

// maxOutputTokens returns the max output token limit for the prompt version.
// v2 (annotations) needs far fewer tokens than full rewrites.
func maxOutputTokens(version string) int {
	switch version {
	case PromptV2:
		return 128 // short entity/keyword annotations only
	default:
		return 512 // full rewrite; caps runaway generation
	}
}

func (e *OllamaEnricher) Enrich(ctx context.Context, chunk model.Chunk, prev, next []model.Chunk) (string, error) {
	prompt := buildEnrichmentPrompt(e.promptVersion, chunk, prev, next)

	resp, err := e.generate(ctx, prompt, maxOutputTokens(e.promptVersion))
	if err != nil {
		return "", fmt.Errorf("enrich: %w", err)
	}

	resp = strings.TrimSpace(resp)
	if resp == "" {
		return chunk.Content, nil
	}

	// For append-only prompts, prepend the original content.
	if e.promptVersion == PromptV2 {
		return chunk.Content + "\n\n" + resp, nil
	}

	return resp, nil
}

func buildEnrichmentPrompt(version string, chunk model.Chunk, prev, next []model.Chunk) string {
	switch version {
	case PromptV2:
		return buildPromptV2(chunk, prev, next)
	default:
		return buildPromptV1(chunk, prev, next)
	}
}

func buildPromptV1(chunk model.Chunk, prev, next []model.Chunk) string {
	var b strings.Builder

	b.WriteString("You are a text enrichment tool. Your job is to rewrite the TARGET PASSAGE so it is fully self-contained.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Replace ALL pronouns and vague references (\"he\", \"she\", \"it\", \"this\", \"that\", \"the project\", \"the service\", \"that framework\") with the specific entities they refer to, using the CONTEXT to identify them.\n")
	b.WriteString("- Inject essential context needed to understand the passage in isolation (e.g., a person's role, a system's purpose) if it appears in the CONTEXT but not the TARGET.\n")
	b.WriteString("- Do NOT add information that isn't in the CONTEXT or TARGET.\n")
	b.WriteString("- Do NOT summarise — preserve the full detail of the original passage.\n")
	b.WriteString("- Output ONLY the rewritten passage. No preamble, no explanation.\n\n")

	if len(prev) > 0 {
		b.WriteString("PRECEDING CONTEXT:\n")
		for _, p := range prev {
			b.WriteString(p.Content)
			b.WriteString("\n\n")
		}
	}

	b.WriteString("TARGET PASSAGE:\n")
	b.WriteString(chunk.Content)
	b.WriteString("\n\n")

	if len(next) > 0 {
		b.WriteString("FOLLOWING CONTEXT:\n")
		for _, n := range next {
			b.WriteString(n.Content)
			b.WriteString("\n\n")
		}
	}

	b.WriteString("Rewritten passage:")
	return b.String()
}

func buildPromptV2(chunk model.Chunk, prev, next []model.Chunk) string {
	var b strings.Builder

	b.WriteString("You are a text annotation tool. Given a TARGET PASSAGE and its surrounding CONTEXT, output a brief annotation block to append after the passage.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- List key entities mentioned or referenced (including by pronoun) with their roles/descriptions from the CONTEXT.\n")
	b.WriteString("- List 3-5 search keywords or synonyms that someone might use to find this passage.\n")
	b.WriteString("- Do NOT rewrite or repeat the passage.\n")
	b.WriteString("- Do NOT add information not in the CONTEXT or TARGET.\n")
	b.WriteString("- Use this exact format:\n")
	b.WriteString("  [Entities: Name1 (role), Name2 (role), ...]\n")
	b.WriteString("  [Keywords: keyword1, keyword2, ...]\n\n")

	if len(prev) > 0 {
		b.WriteString("PRECEDING CONTEXT:\n")
		for _, p := range prev {
			b.WriteString(p.Content)
			b.WriteString("\n\n")
		}
	}

	b.WriteString("TARGET PASSAGE:\n")
	b.WriteString(chunk.Content)
	b.WriteString("\n\n")

	if len(next) > 0 {
		b.WriteString("FOLLOWING CONTEXT:\n")
		for _, n := range next {
			b.WriteString(n.Content)
			b.WriteString("\n\n")
		}
	}

	b.WriteString("Annotation:")
	return b.String()
}

// ollamaChatRequest is the JSON body for the Ollama chat API.
type ollamaChatRequest struct {
	Model    string            `json:"model"`
	Messages []ollamaChatMsg   `json:"messages"`
	Stream   bool              `json:"stream"`
	Options  *ollamaOptions    `json:"options,omitempty"`
}

type ollamaOptions struct {
	NumPredict int `json:"num_predict,omitempty"` // max output tokens
}

type ollamaChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

func (e *OllamaEnricher) generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	reqBody := ollamaChatRequest{
		Model: e.model,
		Messages: []ollamaChatMsg{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}
	if maxTokens > 0 {
		reqBody.Options = &ollamaOptions{NumPredict: maxTokens}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/chat", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var chatResp ollamaChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return chatResp.Message.Content, nil
}

// ProgressFunc is called after each chunk is enriched with (completed, total).
type ProgressFunc func(done, total int)

// EnrichChunks enriches a slice of chunks using a sliding window with
// concurrent workers. hprev is the lookback window size, hnext is the
// lookahead window size. concurrency controls how many chunks are enriched
// in parallel (0 or 1 = sequential). Returns enriched content strings
// parallel to the input chunks.
func EnrichChunks(ctx context.Context, enricher Enricher, chunks []model.Chunk, hprev, hnext, concurrency int, progress ...ProgressFunc) ([]string, error) {
	var progressFn ProgressFunc
	if len(progress) > 0 {
		progressFn = progress[0]
	}
	if concurrency <= 0 {
		concurrency = 1
	}

	enriched := make([]string, len(chunks))

	// Build all work items up front (context windows use raw content only).
	type workItem struct {
		index int
		chunk model.Chunk
		prev  []model.Chunk
		next  []model.Chunk
	}
	work := make([]workItem, len(chunks))
	for i, chunk := range chunks {
		prevStart := i - hprev
		if prevStart < 0 {
			prevStart = 0
		}
		nextEnd := i + 1 + hnext
		if nextEnd > len(chunks) {
			nextEnd = len(chunks)
		}
		var next []model.Chunk
		if i+1 < len(chunks) {
			next = chunks[i+1 : nextEnd]
		}
		work[i] = workItem{index: i, chunk: chunk, prev: chunks[prevStart:i], next: next}
	}

	if concurrency == 1 {
		for _, w := range work {
			result, err := enricher.Enrich(ctx, w.chunk, w.prev, w.next)
			if err != nil {
				return nil, fmt.Errorf("enrich chunk %d: %w", w.index, err)
			}
			enriched[w.index] = result
			if progressFn != nil {
				progressFn(w.index+1, len(chunks))
			}
		}
		return enriched, nil
	}

	// Concurrent path.
	var (
		mu      sync.Mutex
		done    int32
		firstErr error
	)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, w := range work {
		wg.Add(1)
		go func(w workItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Check for prior error or context cancellation.
			mu.Lock()
			if firstErr != nil {
				mu.Unlock()
				return
			}
			mu.Unlock()

			if ctx.Err() != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				mu.Unlock()
				return
			}

			result, err := enricher.Enrich(ctx, w.chunk, w.prev, w.next)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("enrich chunk %d: %w", w.index, err)
				}
				mu.Unlock()
				return
			}
			enriched[w.index] = result

			completed := int(atomic.AddInt32(&done, 1))
			if progressFn != nil {
				progressFn(completed, len(chunks))
			}
		}(w)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return enriched, nil
}
