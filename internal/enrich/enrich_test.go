package enrich

import (
	"context"
	"strings"
	"testing"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

type mockEnricher struct {
	model string
	calls int
}

func (m *mockEnricher) Enrich(_ context.Context, chunk model.Chunk, prev, next []model.Chunk) (string, error) {
	m.calls++
	// Simple mock: prefix with resolved context info.
	var result strings.Builder
	if len(prev) > 0 {
		result.WriteString("[resolved] ")
	}
	result.WriteString(chunk.Content)
	return result.String(), nil
}

func (m *mockEnricher) Model() string { return m.model }

func TestEnrichChunks_SlidingWindow(t *testing.T) {
	enricher := &mockEnricher{model: "test-model"}
	chunks := []model.Chunk{
		{Content: "Elena proposed NATS JetStream."},
		{Content: "She chose it for simplicity."},
		{Content: "He asked about push notifications."},
		{Content: "The team agreed to proceed."},
	}

	enriched, err := EnrichChunks(context.Background(), enricher, chunks, 3, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(enriched) != len(chunks) {
		t.Fatalf("expected %d enriched chunks, got %d", len(chunks), len(enriched))
	}

	// First chunk has no preceding context, so no [resolved] prefix.
	if strings.HasPrefix(enriched[0], "[resolved]") {
		t.Error("first chunk should not have [resolved] prefix (no preceding context)")
	}

	// All subsequent chunks should have preceding context.
	for i := 1; i < len(enriched); i++ {
		if !strings.HasPrefix(enriched[i], "[resolved]") {
			t.Errorf("chunk %d should have [resolved] prefix", i)
		}
	}

	if enricher.calls != len(chunks) {
		t.Errorf("expected %d enricher calls, got %d", len(chunks), enricher.calls)
	}
}

func TestEnrichChunks_EmptyInput(t *testing.T) {
	enricher := &mockEnricher{model: "test"}
	enriched, err := EnrichChunks(context.Background(), enricher, nil, 3, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(enriched) != 0 {
		t.Errorf("expected 0 enriched chunks, got %d", len(enriched))
	}
}

func TestBuildEnrichmentPrompt(t *testing.T) {
	chunk := model.Chunk{Content: "He proposed adding a circuit breaker."}
	prev := []model.Chunk{
		{Content: "Marcus Rivera is the backend engineer."},
	}
	next := []model.Chunk{
		{Content: "The team agreed."},
	}

	prompt := buildEnrichmentPrompt(chunk, prev, next)

	if !strings.Contains(prompt, "TARGET PASSAGE:") {
		t.Error("prompt should contain TARGET PASSAGE header")
	}
	if !strings.Contains(prompt, "PRECEDING CONTEXT:") {
		t.Error("prompt should contain PRECEDING CONTEXT header")
	}
	if !strings.Contains(prompt, "FOLLOWING CONTEXT:") {
		t.Error("prompt should contain FOLLOWING CONTEXT header")
	}
	if !strings.Contains(prompt, "circuit breaker") {
		t.Error("prompt should contain the chunk content")
	}
	if !strings.Contains(prompt, "Marcus Rivera") {
		t.Error("prompt should contain preceding context")
	}
}

func TestPromptVersion(t *testing.T) {
	if PromptVersion == "" {
		t.Error("PromptVersion should not be empty")
	}
}
