package query

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

var (
	queryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "kb_query_duration_seconds", Help: "Query duration by phase", Buckets: prometheus.DefBuckets},
		[]string{"phase"}, // "embedding", "search", "synthesis", "total"
	)
	queryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "kb_queries_total", Help: "Total queries"},
		[]string{"mode"}, // "raw", "synthesis"
	)
	queryErrors = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "kb_query_errors_total", Help: "Total query errors"},
	)
)

func init() {
	prometheus.MustRegister(queryDuration)
	prometheus.MustRegister(queryTotal)
	prometheus.MustRegister(queryErrors)
}

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
	logger   *slog.Logger
}

// NewEngine creates a query engine.
func NewEngine(s store.Store, e embedding.Embedder, llm LLM, defaultLimit int, logger ...*slog.Logger) *Engine {
	if defaultLimit <= 0 {
		defaultLimit = 20
	}
	var lg *slog.Logger
	if len(logger) > 0 {
		lg = logger[0]
	}
	return &Engine{
		store:    s,
		embedder: e,
		llm:      llm,
		limit:    defaultLimit,
		cache:    NewCache(0, 0), // defaults: 10min TTL, 256 entries
		logger:   lg,
	}
}

// SetDiskCache configures a persistent disk backend for the query cache.
// The existing in-memory cache settings (maxAge, maxSize) are preserved.
func (e *Engine) SetDiskCache(disk DiskStore) {
	e.cache = NewCache(e.cache.maxAge, e.cache.maxSize, disk)
	e.cache.SetLogger(e.logger)
}

// HasLLM reports whether an LLM client is configured for synthesis.
func (e *Engine) HasLLM() bool {
	return e.llm != nil
}

// embedAndSearch validates the request, embeds the query, and searches for fragments.
// It performs hybrid search (vector + BM25) with optional multi-query expansion.
// Returns the last user message, the query embedding, and matching fragments.
func (e *Engine) embedAndSearch(ctx context.Context, req model.QueryRequest) (model.Message, []float32, []model.SourceFragment, error) {
	if len(req.Messages) == 0 {
		return model.Message{}, nil, nil, fmt.Errorf("no messages in query request")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = e.limit
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != model.RoleUser {
		return model.Message{}, nil, nil, fmt.Errorf("last message must be from user")
	}

	hasFilters := len(req.Sources) > 0 || len(req.SourceTypes) > 0
	skipExpand := req.Mode == model.ModeRaw || req.Mode == model.ModeLocal || req.NoExpand

	// Step 1: Embed the original query. This embedding is reused for both
	// the vocabulary scout and the main search, avoiding a redundant Ollama call.
	embedStart := time.Now()
	queryEmb, err := e.embedder.Embed(ctx, lastMsg.Content)
	if err != nil {
		return model.Message{}, nil, nil, fmt.Errorf("embed query: %w", err)
	}

	// Step 2: Multi-query expansion (skip in raw mode or when NoExpand is set).
	// Scout with both vector and BM25 to extract domain vocabulary, then ask
	// the LLM to rephrase using those terms.
	allQueries := []string{lastMsg.Content}
	if !skipExpand && e.llm != nil {
		var scoutFrags []model.SourceFragment
		if hasFilters {
			vecFrags, _ := e.store.SearchByVectorFiltered(ctx, queryEmb, 5, req.Sources, req.SourceTypes)
			scoutFrags = append(scoutFrags, vecFrags...)
			ftsFrags, _ := e.store.SearchByFTSFiltered(ctx, lastMsg.Content, 5, req.Sources, req.SourceTypes)
			scoutFrags = append(scoutFrags, ftsFrags...)
		} else {
			vecFrags, _ := e.store.SearchByVector(ctx, queryEmb, 5)
			scoutFrags = append(scoutFrags, vecFrags...)
			ftsFrags, _ := e.store.SearchByFTS(ctx, lastMsg.Content, 5)
			scoutFrags = append(scoutFrags, ftsFrags...)
		}
		vocabHints := extractVocabHints(dedup(scoutFrags), 5)

		expandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		expansions, err := expandQuery(expandCtx, e.llm, lastMsg.Content, vocabHints)
		cancel()
		if err != nil {
			if e.logger != nil {
				e.logger.Warn("query expansion failed, continuing without", "error", err)
			}
		} else if len(expansions) > 0 {
			allQueries = append(allQueries, expansions...)
			if e.logger != nil {
				e.logger.Debug("query expanded", "original", lastMsg.Content, "expansions", expansions)
			}
		}
	}

	// Step 3: Embed expansion queries (original already embedded in Step 1).
	allEmbeddings := [][]float32{queryEmb}
	if len(allQueries) > 1 {
		expansionTexts := allQueries[1:]
		if len(req.Topics) > 0 {
			topicsText := strings.Join(req.Topics, " ")
			texts := append(append([]string(nil), expansionTexts...), topicsText)
			vecs, err := e.embedder.EmbedBatch(ctx, texts)
			if err != nil {
				return model.Message{}, nil, nil, fmt.Errorf("embed expansions+topics: %w", err)
			}
			topicsEmb := vecs[len(vecs)-1]
			// Blend topics into the original query embedding.
			allEmbeddings[0] = combineEmbeddings(queryEmb, topicsEmb, 0.3)
			for i := 0; i < len(vecs)-1; i++ {
				allEmbeddings = append(allEmbeddings, combineEmbeddings(vecs[i], topicsEmb, 0.3))
			}
		} else {
			vecs, err := e.embedder.EmbedBatch(ctx, expansionTexts)
			if err != nil {
				return model.Message{}, nil, nil, fmt.Errorf("embed expansions: %w", err)
			}
			allEmbeddings = append(allEmbeddings, vecs...)
		}
	} else if len(req.Topics) > 0 {
		topicsEmb, err := e.embedder.Embed(ctx, strings.Join(req.Topics, " "))
		if err != nil {
			return model.Message{}, nil, nil, fmt.Errorf("embed topics: %w", err)
		}
		allEmbeddings[0] = combineEmbeddings(queryEmb, topicsEmb, 0.3)
	}

	queryDuration.WithLabelValues("embedding").Observe(time.Since(embedStart).Seconds())

	// Step 4: Vector search for each embedding.
	searchStart := time.Now()
	var resultLists []rankedList
	for _, emb := range allEmbeddings {
		var frags []model.SourceFragment
		var searchErr error
		if hasFilters {
			frags, searchErr = e.store.SearchByVectorFiltered(ctx, emb, limit, req.Sources, req.SourceTypes)
		} else {
			frags, searchErr = e.store.SearchByVector(ctx, emb, limit)
		}
		if searchErr != nil {
			return model.Message{}, nil, nil, fmt.Errorf("vector search: %w", searchErr)
		}
		if len(frags) > 0 {
			resultLists = append(resultLists, rankedList(frags))
		}
	}

	// Step 5: BM25 keyword search for each query.
	for _, q := range allQueries {
		var frags []model.SourceFragment
		var searchErr error
		if hasFilters {
			frags, searchErr = e.store.SearchByFTSFiltered(ctx, q, limit, req.Sources, req.SourceTypes)
		} else {
			frags, searchErr = e.store.SearchByFTS(ctx, q, limit)
		}
		if searchErr != nil {
			if e.logger != nil {
				e.logger.Warn("BM25 search failed, continuing without", "error", searchErr)
			}
			continue
		}
		if len(frags) > 0 {
			resultLists = append(resultLists, rankedList(frags))
		}
	}

	// Step 6: Merge all result lists via RRF.
	fragments := mergeRRF(resultLists, limit)
	queryDuration.WithLabelValues("search").Observe(time.Since(searchStart).Seconds())

	return lastMsg, allEmbeddings[0], fragments, nil
}

// Query processes a query request and streams the answer.
// onText is called with each text chunk as it arrives from the LLM.
// Returns the complete Answer after streaming finishes.
// If req.Concise is true, the LLM produces terse, agent-friendly output.
func (e *Engine) Query(ctx context.Context, req model.QueryRequest, onText func(string)) (*model.Answer, error) {
	totalStart := time.Now()
	queryTotal.WithLabelValues("synthesis").Inc()
	defer func() {
		queryDuration.WithLabelValues("total").Observe(time.Since(totalStart).Seconds())
	}()

	if e.llm == nil {
		queryErrors.Inc()
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

	lastMsg, _, fragments, err := e.embedAndSearch(ctx, req)
	if err != nil {
		queryErrors.Inc()
		return nil, err
	}

	if len(fragments) == 0 {
		answer := &model.Answer{
			Content: "I don't have any relevant information to answer this question.",
			Confidence: model.Confidence{
				Overall:   0,
				Breakdown: model.ConfidenceBreakdown{
					Freshness:     0,
					Corroboration: 0,
					Consistency:   0,
					Authority:     0,
				},
			},
		}
		if onText != nil {
			onText(answer.Content)
		}
		return answer, nil
	}

	// Check cache — exact query match with same underlying fragments.
	if cached := e.cache.Get(lastMsg.Content, req.Concise, fragments, ctx); cached != nil {
		// Promote to fast-path cache so future identical queries skip embedding.
		e.cache.PutFastPath(lastMsg.Content, req.Concise, cached)
		if onText != nil {
			onText(cached.Content)
		}
		return cached, nil
	}

	// Build the system prompt with fragment context.
	systemPrompt := BuildSystemPrompt(fragments, req.Concise)

	if e.logger != nil {
		e.logger.LogAttrs(ctx, slog.LevelDebug, "LLM synthesis",
			slog.Int("fragments", len(fragments)),
			slog.Int("prompt_chars", len(systemPrompt)),
		)
	}

	// Stream the LLM response.
	synthesisStart := time.Now()
	fullResponse, err := e.llm.StreamAnswer(ctx, systemPrompt, req.Messages, onText)
	queryDuration.WithLabelValues("synthesis").Observe(time.Since(synthesisStart).Seconds())
	if err != nil {
		queryErrors.Inc()
		return nil, fmt.Errorf("llm synthesis: %w", err)
	}

	// Build the answer with server-computed confidence and sources.
	answer := buildAnswer(fullResponse, fragments)

	// Cache the result.
	e.cache.Put(lastMsg.Content, req.Concise, fragments, answer, ctx)
	e.cache.PutFastPath(lastMsg.Content, req.Concise, answer)

	return answer, nil
}

// citationRe matches [fragment_id] citations in LLM prose.
var citationRe = regexp.MustCompile(`\[([a-f0-9]{8,})\]`)

// parseCitations extracts fragment IDs cited in the LLM prose text.
func parseCitations(text string) []string {
	matches := citationRe.FindAllStringSubmatch(text, -1)
	seen := make(map[string]struct{})
	var ids []string
	for _, m := range matches {
		id := m[1]
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

// computeConfidence computes server-side confidence from the given fragments,
// using the same logic as QueryRaw.
func computeConfidence(fragments []model.SourceFragment) model.Confidence {
	// Count distinct source names for corroboration.
	sourceNames := make(map[string]struct{})
	for _, f := range fragments {
		sourceNames[f.SourceName] = struct{}{}
	}
	corroboration := ComputeCorroboration(len(sourceNames))

	// Average per-fragment signals.
	var freshness, consistency, authority float64
	for _, f := range fragments {
		freshness += ComputeFreshness(f.ContentDate, f.IngestedAt, f.FileType)
		consistency += ComputeConsistency(f.ConfidenceAdj)
		authority += ComputeAuthority(f.FileType)
	}
	n := float64(len(fragments))
	if n > 0 {
		freshness /= n
		consistency /= n
		authority /= n
	}

	breakdown := model.ConfidenceBreakdown{
		Freshness:     math.Round(freshness*100) / 100,
		Corroboration: corroboration,
		Consistency:   math.Round(consistency*100) / 100,
		Authority:     math.Round(authority*100) / 100,
	}

	return model.Confidence{
		Overall:   ComputeOverallTrust(breakdown, model.DefaultTrustWeights()),
		Breakdown: breakdown,
	}
}

// buildAnswer constructs an Answer from the LLM prose and retrieved fragments.
// Confidence is computed server-side. Sources are derived from cited fragments
// when citations are present, otherwise from all retrieved fragments.
// If the LLM response contains a legacy ---KB_META--- block, it is stripped.
func buildAnswer(response string, fragments []model.SourceFragment) *model.Answer {
	// Strip legacy metadata block if present (backward compat).
	content := response
	if startIdx := strings.LastIndex(content, "---KB_META---"); startIdx >= 0 {
		content = strings.TrimSpace(content[:startIdx])
	}

	// Parse citations from LLM prose.
	cited := parseCitations(content)

	// Build a lookup of retrieved fragments by ID.
	fragByID := make(map[string]model.SourceFragment, len(fragments))
	for _, f := range fragments {
		fragByID[f.ID] = f
	}

	// Build sources list from cited fragments if any, else all fragments.
	var sources []model.SourceRef
	var confidenceFragments []model.SourceFragment
	if len(cited) > 0 {
		seen := make(map[string]struct{})
		for _, id := range cited {
			if f, ok := fragByID[id]; ok {
				if _, dup := seen[id]; !dup {
					seen[id] = struct{}{}
					sources = append(sources, model.SourceRef{
						FragmentID: f.ID,
						SourceURI:  f.SourceURI,
						SourcePath: f.SourcePath,
						SourceName: f.SourceName,
					})
					confidenceFragments = append(confidenceFragments, f)
				}
			}
		}
	}

	// Fall back to all retrieved fragments if no valid citations found.
	if len(sources) == 0 {
		for _, f := range fragments {
			sources = append(sources, model.SourceRef{
				FragmentID: f.ID,
				SourceURI:  f.SourceURI,
				SourcePath: f.SourcePath,
				SourceName: f.SourceName,
			})
		}
		confidenceFragments = fragments
	}

	// Compute confidence server-side from the fragments.
	confidence := computeConfidence(confidenceFragments)

	return &model.Answer{
		Content:    content,
		Confidence: confidence,
		Sources:    sources,
	}
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
	totalStart := time.Now()
	queryTotal.WithLabelValues("raw").Inc()
	defer func() {
		queryDuration.WithLabelValues("total").Observe(time.Since(totalStart).Seconds())
	}()

	_, queryEmb, fragments, err := e.embedAndSearch(ctx, req)
	if err != nil {
		queryErrors.Inc()
		return nil, err
	}

	// Count distinct source names for corroboration.
	sourceNames := make(map[string]struct{})
	for _, f := range fragments {
		sourceNames[f.SourceName] = struct{}{}
	}
	corroboration := ComputeCorroboration(len(sourceNames))

	// Build raw fragments with per-fragment confidence signals.
	rawFragments := make([]model.RawFragment, len(fragments))
	for i, f := range fragments {
		breakdown := model.ConfidenceBreakdown{
			Freshness:     ComputeFreshness(f.ContentDate, f.IngestedAt, f.FileType),
			Corroboration: corroboration,
			Consistency:   ComputeConsistency(f.ConfidenceAdj),
			Authority:     ComputeAuthority(f.FileType),
		}
		rawFragments[i] = model.RawFragment{
			FragmentID:      f.ID,
			Content:         f.RawContent,
			EnrichedContent: f.EnrichedContent,
			SourcePath:      f.SourcePath,
			SourceURI:       f.SourceURI,
			SourceName:      f.SourceName,
			SourceType:      f.SourceType,
			FileType:        f.FileType,
			ContentDate:     f.ContentDate,
			IngestedAt:      f.IngestedAt,
			Author:          f.Author,
			Confidence: model.Confidence{
				Overall:   ComputeOverallTrust(breakdown, model.DefaultTrustWeights()),
				Breakdown: breakdown,
			},
		}
	}

	result := &model.RawResult{
		Fragments: rawFragments,
	}

	// Search knowledge units if any exist (best-effort; ignore errors).
	if len(fragments) > 0 && queryEmb != nil {
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

	return result, nil
}

// QueryLocal performs local LLM synthesis: retrieval + simplified prompt + heuristic confidence.
// It uses the same hybrid search as other modes but sends a simpler prompt suitable for
// small local models. Confidence scores are computed heuristically from fragments, not by the LLM.
func (e *Engine) QueryLocal(ctx context.Context, req model.QueryRequest, onText func(string)) (*model.Answer, error) {
	totalStart := time.Now()
	queryTotal.WithLabelValues("local").Inc()
	defer func() {
		queryDuration.WithLabelValues("total").Observe(time.Since(totalStart).Seconds())
	}()

	if e.llm == nil {
		queryErrors.Inc()
		return nil, fmt.Errorf("LLM client not configured")
	}

	// Force no expansion — too slow/poor on small models.
	req.NoExpand = true

	// Cap fragments at 5 for local mode — small models produce better answers
	// from a focused set of top-ranked sources, and inference is much faster.
	if req.Limit <= 0 || req.Limit > 5 {
		req.Limit = 5
	}

	lastMsg, _, fragments, err := e.embedAndSearch(ctx, req)
	if err != nil {
		queryErrors.Inc()
		return nil, err
	}
	_ = lastMsg

	if len(fragments) == 0 {
		noInfo := "I don't have any relevant information to answer this question."
		if onText != nil {
			onText(noInfo)
		}
		return &model.Answer{
			Content: noInfo,
			Confidence: model.Confidence{
				Overall:   0,
				Breakdown: model.ConfidenceBreakdown{},
			},
		}, nil
	}

	// Build simplified prompt for local LLM.
	systemPrompt := BuildLocalPrompt(fragments)

	if e.logger != nil {
		e.logger.LogAttrs(ctx, slog.LevelDebug, "local LLM synthesis",
			slog.Int("fragments", len(fragments)),
			slog.Int("prompt_chars", len(systemPrompt)),
		)
	}

	// Stream the LLM response.
	synthesisStart := time.Now()
	fullResponse, err := e.llm.StreamAnswer(ctx, systemPrompt, req.Messages, onText)
	queryDuration.WithLabelValues("synthesis").Observe(time.Since(synthesisStart).Seconds())
	if err != nil {
		queryErrors.Inc()
		return nil, fmt.Errorf("local llm synthesis: %w", err)
	}

	// Compute heuristic confidence from fragments (same approach as QueryRaw).
	sourceNames := make(map[string]struct{})
	var maxFreshness, maxAuthority float64
	var totalConsistency float64
	for _, f := range fragments {
		sourceNames[f.SourceName] = struct{}{}
		freshness := ComputeFreshness(f.ContentDate, f.IngestedAt, f.FileType)
		if freshness > maxFreshness {
			maxFreshness = freshness
		}
		authority := ComputeAuthority(f.FileType)
		if authority > maxAuthority {
			maxAuthority = authority
		}
		totalConsistency += ComputeConsistency(f.ConfidenceAdj)
	}

	breakdown := model.ConfidenceBreakdown{
		Freshness:     maxFreshness,
		Corroboration: ComputeCorroboration(len(sourceNames)),
		Consistency:   math.Round(totalConsistency/float64(len(fragments))*100) / 100,
		Authority:     maxAuthority,
	}

	// Build sources list from all retrieved fragments.
	sources := make([]model.SourceRef, len(fragments))
	for i, f := range fragments {
		sources[i] = model.SourceRef{
			FragmentID: f.ID,
			SourceURI:  f.SourceURI,
			SourcePath: f.SourcePath,
			SourceName: f.SourceName,
		}
	}

	return &model.Answer{
		Content: fullResponse,
		Confidence: model.Confidence{
			Overall:   ComputeOverallTrust(breakdown, model.DefaultTrustWeights()),
			Breakdown: breakdown,
		},
		Sources: sources,
	}, nil
}

// ComputeOverallTrust computes a weighted composite trust score from the breakdown.
func ComputeOverallTrust(b model.ConfidenceBreakdown, w model.TrustWeights) float64 {
	score := b.Freshness*w.Freshness + b.Corroboration*w.Corroboration + b.Consistency*w.Consistency + b.Authority*w.Authority
	return math.Round(score*100) / 100
}

// ComputeFreshness returns a score from 0 to 1 based on how recent the document is.
// Code files get high freshness regardless of age (code in the repo is current).
// For docs/prose, uses temporal decay with ingested_at as fallback when content_date is zero.
func ComputeFreshness(contentDate, ingestedAt time.Time, fileType string) float64 {
	// Code files: high freshness as long as they exist in the repo.
	if IsCodeFile(fileType) {
		return 0.95
	}

	// Docs/prose: temporal decay.
	t := contentDate
	if t.IsZero() {
		t = ingestedAt
	}
	if t.IsZero() {
		return 0.3 // unknown date gets a low default
	}
	days := time.Since(t).Hours() / 24
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

// IsCodeFile returns true for source code and config file extensions.
func IsCodeFile(fileType string) bool {
	switch strings.ToLower(fileType) {
	case ".go", ".py", ".js", ".ts", ".java", ".rs", ".c", ".cpp", ".rb",
		".yaml", ".yml", ".toml", ".json", ".xml", ".html":
		return true
	}
	return false
}

// ComputeCorroboration returns a score based on how many distinct sources appear.
func ComputeCorroboration(numSources int) float64 {
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

// ComputeConsistency maps a confidence adjustment to a 0-1 range.
// Baseline is 0.5, adjustment is added and clamped.
func ComputeConsistency(confidenceAdj float64) float64 {
	score := 0.5 + confidenceAdj
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return math.Round(score*100) / 100
}

// ComputeAuthority returns a heuristic authority score based on file type.
func ComputeAuthority(fileType string) float64 {
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
