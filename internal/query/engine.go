package query

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
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

// embedAndSearch validates the request, embeds the query, and searches for fragments.
func (e *Engine) embedAndSearch(ctx context.Context, req model.QueryRequest) (model.Message, []model.SourceFragment, error) {
	if len(req.Messages) == 0 {
		return model.Message{}, nil, fmt.Errorf("no messages in query request")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = e.limit
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != model.RoleUser {
		return model.Message{}, nil, fmt.Errorf("last message must be from user")
	}

	// Embed the query (and topics if present) in a single batch call.
	var queryEmb []float32
	if len(req.Topics) > 0 {
		topicsText := strings.Join(req.Topics, " ")
		vecs, err := e.embedder.EmbedBatch(ctx, []string{lastMsg.Content, topicsText})
		if err != nil {
			return model.Message{}, nil, fmt.Errorf("embed query+topics: %w", err)
		}
		queryEmb = combineEmbeddings(vecs[0], vecs[1], 0.3)
	} else {
		var err error
		queryEmb, err = e.embedder.Embed(ctx, lastMsg.Content)
		if err != nil {
			return model.Message{}, nil, fmt.Errorf("embed query: %w", err)
		}
	}

	fragments, err := e.store.SearchByVector(ctx, queryEmb, limit)
	if err != nil {
		return model.Message{}, nil, fmt.Errorf("search: %w", err)
	}

	return lastMsg, fragments, nil
}

// Query processes a query request and streams the answer.
// onText is called with each text chunk as it arrives from the LLM.
// Returns the complete Answer after streaming finishes.
// If req.Concise is true, the LLM produces terse, agent-friendly output.
func (e *Engine) Query(ctx context.Context, req model.QueryRequest, onText func(string)) (*model.Answer, error) {
	if e.llm == nil {
		return nil, fmt.Errorf("LLM client not configured")
	}

	// Fast-path: exact match, skip embedding + search + LLM entirely.
	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if lastMsg.Role == model.RoleUser {
			if cached := e.cache.GetFastPath(lastMsg.Content, req.Concise); cached != nil {
				if onText != nil {
					onText(cached.Content)
				}
				return cached, nil
			}
		}
	}

	lastMsg, fragments, err := e.embedAndSearch(ctx, req)
	if err != nil {
		return nil, err
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
		// Promote to fast-path cache so future identical queries skip embedding.
		e.cache.PutFastPath(lastMsg.Content, req.Concise, cached)
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
	e.cache.PutFastPath(lastMsg.Content, req.Concise, answer)

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
				SourceName: f.SourceName,
			})
		}
	}

	return answer
}

// combineEmbeddings blends two embedding vectors: result = normalize(a + b * weight).
// This allows topic signals to influence vector search without replacing the
// original query intent.
func combineEmbeddings(a, b []float32, weight float64) []float32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	combined := make([]float32, n)
	var norm float64
	for i := 0; i < n; i++ {
		v := float64(a[i]) + float64(b[i])*weight
		combined[i] = float32(v)
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range combined {
			combined[i] = float32(float64(combined[i]) / norm)
		}
	}
	return combined
}

// QueryRaw performs raw retrieval: embed, search, compute local confidence signals,
// and return fragments directly without LLM synthesis.
func (e *Engine) QueryRaw(ctx context.Context, req model.QueryRequest) (*model.RawResult, error) {
	_, fragments, err := e.embedAndSearch(ctx, req)
	if err != nil {
		return nil, err
	}

	// Count distinct source names for corroboration.
	sourceNames := make(map[string]struct{})
	for _, f := range fragments {
		sourceNames[f.SourceName] = struct{}{}
	}
	corroboration := computeCorroboration(len(sourceNames))

	// Build raw fragments with per-fragment confidence signals.
	rawFragments := make([]model.RawFragment, len(fragments))
	for i, f := range fragments {
		rawFragments[i] = model.RawFragment{
			FragmentID:   f.ID,
			Content:      f.Content,
			SourcePath:   f.SourcePath,
			SourceURI:    f.SourceURI,
			SourceName:   f.SourceName,
			SourceType:   f.SourceType,
			FileType:     f.FileType,
			LastModified: f.LastModified,
			Author:       f.Author,
			Confidence: model.ConfidenceSignals{
				Freshness:     computeFreshness(f.LastModified),
				Corroboration: corroboration,
				Consistency:   computeConsistency(f.ConfidenceAdj),
				Authority:     computeAuthority(f.FileType),
			},
		}
	}

	result := &model.RawResult{
		Fragments: rawFragments,
	}

	// Search knowledge units if any exist (best-effort; ignore errors).
	if len(fragments) > 0 {
		queryEmb, _ := e.embedder.Embed(ctx, req.Messages[len(req.Messages)-1].Content)
		if queryEmb != nil {
			units, err := e.store.SearchKnowledgeUnits(ctx, queryEmb, 5)
			if err == nil && len(units) > 0 {
				rawUnits := make([]model.RawKnowledgeUnit, len(units))
				for i, u := range units {
					rawUnits[i] = model.RawKnowledgeUnit{
						ID:          u.ID,
						Topic:       u.Topic,
						Summary:     u.Summary,
						Confidence:  u.Confidence,
						FragmentIDs: u.FragmentIDs,
					}
				}
				result.KnowledgeUnits = rawUnits
			}
		}
	}

	return result, nil
}

// computeFreshness returns a score from 0 to 1 based on how recent the document is.
// 1.0 for today, decaying over months.
func computeFreshness(lastModified time.Time) float64 {
	if lastModified.IsZero() {
		return 0.3 // unknown date gets a low default
	}
	days := time.Since(lastModified).Hours() / 24
	if days <= 0 {
		return 1.0
	}
	// Exponential decay: half-life of ~90 days
	score := math.Exp(-days / 130.0)
	if score < 0.1 {
		score = 0.1
	}
	return math.Round(score*100) / 100
}

// computeCorroboration returns a score based on how many distinct sources appear.
func computeCorroboration(numSources int) float64 {
	if numSources <= 0 {
		return 0.0
	}
	if numSources == 1 {
		return 0.3
	}
	if numSources == 2 {
		return 0.6
	}
	if numSources >= 5 {
		return 1.0
	}
	// 3-4 sources
	return 0.8
}

// computeConsistency maps a confidence adjustment to a 0-1 range.
// Baseline is 0.5, adjustment is added and clamped.
func computeConsistency(confidenceAdj float64) float64 {
	score := 0.5 + confidenceAdj
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return math.Round(score*100) / 100
}

// computeAuthority returns a heuristic authority score based on file type.
func computeAuthority(fileType string) float64 {
	ext := strings.ToLower(fileType)
	if ext == "" {
		return 0.5
	}
	// Normalize: ensure leading dot
	if ext[0] != '.' {
		ext = "." + ext
	}
	// Also try without the leading dot for matching
	ext = filepath.Ext("file" + ext)
	if ext == "" {
		return 0.5
	}

	switch ext {
	case ".md", ".markdown", ".rst":
		return 0.8
	case ".go", ".py", ".js", ".ts", ".java", ".rs", ".c", ".cpp", ".rb":
		return 0.7
	case ".yaml", ".yml", ".toml", ".json":
		return 0.65
	case ".txt":
		return 0.6
	case ".html", ".htm", ".xml":
		return 0.55
	default:
		return 0.5
	}
}
