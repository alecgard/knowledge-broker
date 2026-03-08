package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// LLM is the interface for the language model used for synthesis.
type LLM interface {
	StreamAnswer(ctx context.Context, systemPrompt string, messages []model.Message, onText func(string)) (string, error)
}

// Engine orchestrates query processing: embed → search → synthesise.
type Engine struct {
	store    store.Store
	embedder embedding.Embedder
	llm      LLM
	limit    int
	cache    *Cache
}

// NewEngine creates a query engine.
func NewEngine(s store.Store, e embedding.Embedder, llm LLM, defaultLimit int) *Engine {
	if defaultLimit <= 0 {
		defaultLimit = 20
	}
	return &Engine{
		store:    s,
		embedder: e,
		llm:      llm,
		limit:    defaultLimit,
		cache:    NewCache(0, 0), // defaults: 10min TTL, 256 entries
	}
}

// Query processes a query request and streams the answer.
// onText is called with each text chunk as it arrives from the LLM.
// Returns the complete Answer after streaming finishes.
// If req.Concise is true, the LLM produces terse, agent-friendly output.
func (e *Engine) Query(ctx context.Context, req model.QueryRequest, onText func(string)) (*model.Answer, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("no messages in query request")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = e.limit
	}

	// Get the latest user message for embedding.
	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != "user" {
		return nil, fmt.Errorf("last message must be from user")
	}

	// Embed the query.
	queryEmb, err := e.embedder.Embed(ctx, lastMsg.Content)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Search for relevant fragments.
	fragments, err := e.store.SearchByVector(ctx, queryEmb, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	if len(fragments) == 0 {
		answer := &model.Answer{
			Content: "I don't have any relevant information to answer this question.",
			Confidence: model.ConfidenceSignals{
				Freshness:     0,
				Corroboration: 0,
				Consistency:   0,
				Authority:     0,
			},
		}
		if onText != nil {
			onText(answer.Content)
		}
		return answer, nil
	}

	// Check cache — exact query match with same underlying fragments.
	if cached := e.cache.Get(lastMsg.Content, req.Concise, fragments); cached != nil {
		if onText != nil {
			onText(cached.Content)
		}
		return cached, nil
	}

	// Build the system prompt with fragment context.
	systemPrompt := BuildSystemPrompt(fragments, req.Concise)

	// Stream the LLM response.
	fullResponse, err := e.llm.StreamAnswer(ctx, systemPrompt, req.Messages, onText)
	if err != nil {
		return nil, fmt.Errorf("llm synthesis: %w", err)
	}

	// Parse the response for metadata.
	answer := parseResponse(fullResponse, fragments)

	// Cache the result.
	e.cache.Put(lastMsg.Content, req.Concise, fragments, answer)

	return answer, nil
}

const metaStart = "---KB_META---"
const metaEnd = "---KB_META_END---"

// parseResponse extracts the answer text and metadata JSON from the LLM response.
func parseResponse(response string, fragments []model.SourceFragment) *model.Answer {
	answer := &model.Answer{
		Content: response,
		Sources: make([]model.SourceRef, 0, len(fragments)),
	}

	// Try to extract the metadata block.
	startIdx := strings.LastIndex(response, metaStart)
	endIdx := strings.LastIndex(response, metaEnd)

	if startIdx >= 0 && endIdx > startIdx {
		jsonStr := strings.TrimSpace(response[startIdx+len(metaStart) : endIdx])
		answer.Content = strings.TrimSpace(response[:startIdx])

		var meta struct {
			Confidence     model.ConfidenceSignals `json:"confidence"`
			Sources        []model.SourceRef       `json:"sources"`
			Contradictions []model.Contradiction   `json:"contradictions"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &meta); err == nil {
			answer.Confidence = meta.Confidence
			if len(meta.Sources) > 0 {
				answer.Sources = meta.Sources
			}
			answer.Contradictions = meta.Contradictions
		}
	}

	// If no sources from metadata, include all retrieved fragments as sources.
	if len(answer.Sources) == 0 {
		for _, f := range fragments {
			answer.Sources = append(answer.Sources, model.SourceRef{
				FragmentID: f.ID,
				SourceURI:  f.SourceURI,
				SourcePath: f.SourcePath,
			})
		}
	}

	return answer
}
