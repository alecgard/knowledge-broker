package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/enrich"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// ProgressFunc is called during document processing to report progress.
// completed is the number of documents processed so far, total is the
// total number of documents to process.
type ProgressFunc func(completed, total int)

// BatchFunc is called after each batch is committed to the store.
// batch is 1-indexed, totalBatches is the total number of batches,
// and added is the number of fragments stored in this batch.
type BatchFunc func(batch, totalBatches, added int)

// EnrichmentConfig holds enrichment settings for the pipeline.
type EnrichmentConfig struct {
	Enricher    enrich.Enricher
	HPrev       int // lookback window size (default 3)
	HNext       int // lookahead window size (default 1)
	Concurrency int // parallel enrichment workers (default 4)
}

// ScanCompleteFunc is called after scanning completes with the results.
// total is all files seen, changed is new/modified, deleted is removed,
// unchanged is skipped due to matching checksums.
type ScanCompleteFunc func(total, changed, deleted, unchanged int)

// EmbedFunc is called before embedding starts for a batch.
// batch is 1-indexed, totalBatches is the total, fragments is the count.
type EmbedFunc func(batch, totalBatches, fragments int)

// EmbedProgressFunc is called periodically during embedding to report progress.
// completed and total are fragment counts within the current pipeline batch.
type EmbedProgressFunc func(completed, total int)

// Pipeline orchestrates the ingestion of documents.
type Pipeline struct {
	store       store.Store
	embedder    embedding.Embedder
	extractors  *extractor.Registry
	enrichment  *EnrichmentConfig
	workers     int
	logger      *slog.Logger
	OnProgress       ProgressFunc
	OnBatchDone      BatchFunc
	OnScanComplete   ScanCompleteFunc
	OnEmbedding      EmbedFunc
	OnEmbedProgress  EmbedProgressFunc
	BatchSize   int
}

// NewPipeline creates an ingestion pipeline.
func NewPipeline(s store.Store, e embedding.Embedder, r *extractor.Registry, workers int, logger *slog.Logger) *Pipeline {
	if workers <= 0 {
		workers = 4
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Pipeline{
		store:      s,
		embedder:   e,
		extractors: r,
		workers:    workers,
		logger:     logger,
		BatchSize:  50,
	}
}

// SetEnrichment enables chunk enrichment in the pipeline.
func (p *Pipeline) SetEnrichment(cfg EnrichmentConfig) {
	if cfg.HPrev <= 0 {
		cfg.HPrev = 3
	}
	if cfg.HNext <= 0 {
		cfg.HNext = 1
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	p.enrichment = &cfg
}

// Options controls pipeline behaviour for a single Run.
type Options struct {
	// Force bypasses checksum-based skipping, causing all files to be
	// re-ingested regardless of whether their content has changed.
	Force bool
}

// Result summarises an ingestion run.
type Result struct {
	Added           int
	Updated         int
	Deleted         int
	Skipped         int
	Errors          int
	EnrichmentTimeMS int64
}

// Run executes the ingestion pipeline for a connector.
func (p *Pipeline) Run(ctx context.Context, conn connector.Connector, opts ...Options) (*Result, error) {
	result := &Result{}

	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}

	// Get known checksums for incremental ingestion.
	// When Force is set, use an empty map so connectors treat every file as new
	// and deletion detection is naturally skipped.
	var known map[string]string
	if opt.Force {
		known = make(map[string]string)
	} else {
		var err error
		known, err = p.store.GetChecksums(ctx, conn.Name(), conn.SourceName())
		if err != nil {
			return nil, fmt.Errorf("get checksums: %w", err)
		}
	}

	// Look up the source to get LastIngest for incremental time-based filtering.
	// Skip when Force is set so connectors like Slack scan the full lookback window.
	scanOpts := connector.ScanOptions{Known: known, Force: opt.Force}
	if !opt.Force {
		if src, err := p.store.GetSource(ctx, conn.Name(), conn.SourceName()); err != nil {
			p.logger.Warn("failed to look up source for incremental scan, falling back to full scan", "error", err)
		} else if src != nil && src.LastIngest != nil {
			scanOpts.LastIngest = src.LastIngest
		}
	}

	// Scan for new/changed documents and deleted paths.
	docs, deleted, err := conn.Scan(ctx, scanOpts)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	// Delete removed documents.
	if len(deleted) > 0 {
		if err := p.store.DeleteByPaths(ctx, conn.Name(), conn.SourceName(), deleted); err != nil {
			return nil, fmt.Errorf("delete: %w", err)
		}
		result.Deleted = len(deleted)
	}

	result.Skipped = len(known) - len(deleted) - countOverlap(known, docs)

	if p.OnScanComplete != nil {
		total := len(docs) + len(deleted) + result.Skipped
		p.OnScanComplete(total, len(docs), len(deleted), result.Skipped)
	}

	// Process and store documents in batches so that completed batches
	// survive cancellation and are skipped on re-run.
	batchSize := p.BatchSize
	if batchSize <= 0 {
		batchSize = len(docs)
	}
	batches := batchSlice(docs, batchSize)
	progressOffset := 0
	totalDocs := len(docs)

	for batchIdx, batch := range batches {
		if ctx.Err() != nil {
			p.logger.Info("ingestion interrupted, partial result committed",
				"added", result.Added, "errors", result.Errors)
			return result, ctx.Err()
		}

		batchNum := batchIdx + 1
		p.logger.Debug("extracting chunks", "batch", batchNum, "batches", len(batches), "docs", len(batch))
		fragments, errs := p.processDocuments(ctx, batch, progressOffset, totalDocs)
		progressOffset += len(batch)
		result.Errors += errs

		if len(fragments) > 0 {
			// Per-batch phasing: enrich all chunks first, then embed all chunks.
			if p.enrichment != nil {
				enrichStart := time.Now()
				p.enrichBatch(ctx, fragments)
				result.EnrichmentTimeMS += time.Since(enrichStart).Milliseconds()
			}

			if p.OnEmbedding != nil {
				p.OnEmbedding(batchNum, len(batches), len(fragments))
			}
			p.logger.Debug("embedding fragments", "batch", batchNum, "batches", len(batches), "fragments", len(fragments))
			embedStart := time.Now()
			embedded, err := p.embedBatch(ctx, fragments)
			if err != nil {
				return result, fmt.Errorf("embed batch: %w", err)
			}
			p.logger.Debug("embedding complete", "batch", batchNum, "batches", len(batches), "fragments", len(embedded), "elapsed_ms", time.Since(embedStart).Milliseconds())

			if err := p.store.UpsertFragments(ctx, embedded); err != nil {
				return result, fmt.Errorf("upsert fragments: %w", err)
			}
			result.Added += len(embedded)
		}

		if p.OnBatchDone != nil {
			p.OnBatchDone(batchNum, len(batches), len(fragments))
		}
	}

	p.logger.Debug("ingestion complete",
		"added", result.Added,
		"deleted", result.Deleted,
		"skipped", result.Skipped,
		"errors", result.Errors,
	)

	return result, nil
}

// enrichBatch runs enrichment on all fragments in the batch.
// Fragments are grouped by source path so that the sliding window operates
// within a single document's chunks.
func (p *Pipeline) enrichBatch(ctx context.Context, fragments []model.SourceFragment) {
	cfg := p.enrichment
	modelName := cfg.Enricher.Model()

	// Check store for cached enrichments: if a fragment already exists with the
	// same raw_content, enrichment_model, and enrichment_version, reuse it.
	ids := make([]string, len(fragments))
	for i, f := range fragments {
		ids[i] = f.ID
	}
	cached := make(map[int]bool)
	if existing, err := p.store.GetFragments(ctx, ids); err == nil {
		existingByID := make(map[string]model.SourceFragment, len(existing))
		for _, e := range existing {
			existingByID[e.ID] = e
		}
		for i, f := range fragments {
			if e, ok := existingByID[f.ID]; ok &&
				e.EnrichedContent != "" &&
				e.RawContent == f.RawContent &&
				e.EnrichmentModel == modelName &&
				e.EnrichmentVersion == enrich.PromptVersion {
				fragments[i].EnrichedContent = e.EnrichedContent
				fragments[i].EnrichmentModel = e.EnrichmentModel
				fragments[i].EnrichmentVersion = e.EnrichmentVersion
				cached[i] = true
			}
		}
	}

	needEnrichment := len(fragments) - len(cached)
	if needEnrichment == 0 {
		p.logger.Debug("enrichment cached, skipping LLM calls", "chunks", len(fragments))
		return
	}
	p.logger.Debug("starting enrichment",
		"model", modelName,
		"prompt_version", enrich.PromptVersion,
		"hprev", cfg.HPrev,
		"hnext", cfg.HNext,
		"concurrency", cfg.Concurrency,
		"chunks", needEnrichment,
		"cached", len(cached),
	)

	// Group fragments by source path to maintain document-level context windows.
	type docGroup struct {
		indices []int
		chunks  []model.Chunk
	}
	groups := make(map[string]*docGroup)
	var order []string
	for i, f := range fragments {
		key := f.SourceType + ":" + f.SourcePath
		g, ok := groups[key]
		if !ok {
			g = &docGroup{}
			groups[key] = g
			order = append(order, key)
		}
		g.indices = append(g.indices, i)
		g.chunks = append(g.chunks, model.Chunk{Content: f.RawContent})
	}

	done := 0
	for _, key := range order {
		g := groups[key]

		// Check if all chunks in this group are cached.
		allCached := true
		for _, idx := range g.indices {
			if !cached[idx] {
				allCached = false
				break
			}
		}
		if allCached {
			done += len(g.chunks)
			continue
		}

		progress := func(chunkDone, chunkTotal int) {
			pct := (done + chunkDone) * 100 / needEnrichment
			if pct > 100 {
				pct = 100
			}
			p.logger.Debug("enriching", "chunk", done+chunkDone, "total", needEnrichment, "pct", pct)
		}
		enriched, err := enrich.EnrichChunks(ctx, cfg.Enricher, g.chunks, cfg.HPrev, cfg.HNext, cfg.Concurrency, progress)
		if err != nil {
			p.logger.Warn("enrichment failed, using raw content", "path", key, "error", err)
			done += len(g.chunks)
			continue
		}
		for j, idx := range g.indices {
			fragments[idx].EnrichedContent = enriched[j]
			fragments[idx].EnrichmentModel = modelName
			fragments[idx].EnrichmentVersion = enrich.PromptVersion
		}
		done += len(g.chunks)
	}
}

// embedSubBatchSize is the number of fragments sent to the embedder per call.
// Keeps individual Ollama requests small enough for timely cancellation and
// progress reporting.
const embedSubBatchSize = 200

// embedBatch embeds all fragments and returns only those with successful
// embeddings. Uses enriched content if available, raw otherwise.
// Fragments are sent to the embedder in sub-batches of embedSubBatchSize
// so progress can be reported between calls.
func (p *Pipeline) embedBatch(ctx context.Context, fragments []model.SourceFragment) ([]model.SourceFragment, error) {
	total := len(fragments)
	var result []model.SourceFragment
	embedded := 0

	for start := 0; start < total; start += embedSubBatchSize {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		end := start + embedSubBatchSize
		if end > total {
			end = total
		}
		chunk := fragments[start:end]

		texts := make([]string, len(chunk))
		for i, f := range chunk {
			texts[i] = f.Content()
		}

		embeddings, err := p.embedder.EmbedBatch(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("embed batch: %w", err)
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		for i := range chunk {
			if embeddings[i] == nil {
				p.logger.Warn("skipping fragment with nil embedding",
					"path", chunk[i].SourcePath, "id", chunk[i].ID)
				continue
			}
			chunk[i].Embedding = embeddings[i]
			result = append(result, chunk[i])
		}

		embedded += len(chunk)
		if p.OnEmbedProgress != nil {
			p.OnEmbedProgress(embedded, total)
		}
	}

	return result, nil
}

// processDocuments extracts chunks from documents concurrently.
// When enrichment is enabled, embedding is deferred to the caller (per-batch phasing).
func (p *Pipeline) processDocuments(ctx context.Context, docs []model.RawDocument, progressOffset, progressTotal int) ([]model.SourceFragment, int) {
	type fragmentResult struct {
		fragments []model.SourceFragment
		err       error
	}

	results := make(chan fragmentResult, len(docs))
	sem := make(chan struct{}, p.workers)
	var wg sync.WaitGroup

	for _, doc := range docs {
		wg.Add(1)
		go func(d model.RawDocument) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			frags, err := p.processDocument(ctx, d)
			results <- fragmentResult{frags, err}
		}(doc)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var allFragments []model.SourceFragment
	var completed atomic.Int64
	errCount := 0
	for r := range results {
		if r.err != nil {
			p.logger.Warn("failed to process document", "error", r.err)
			errCount++
		} else {
			allFragments = append(allFragments, r.fragments...)
		}
		n := int(completed.Add(1))
		if p.OnProgress != nil {
			p.OnProgress(progressOffset+n, progressTotal)
		}
	}

	return allFragments, errCount
}

func (p *Pipeline) processDocument(ctx context.Context, doc model.RawDocument) ([]model.SourceFragment, error) {
	result, err := ExtractChunks(doc, p.extractors)
	if err != nil {
		return nil, err
	}

	if len(result.Chunks) == 0 {
		return nil, nil
	}

	// Derive file type from path extension.
	fileType := filepath.Ext(doc.Path)

	// Determine content date: extractor metadata overrides connector date.
	contentDate := doc.ContentDate
	if dateStr, ok := result.Metadata["content_date"]; ok && dateStr != "" {
		if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
			contentDate = t
		}
	}

	// Build fragments — embedding is always deferred to embedBatch (called
	// per-batch in Run) so we get a single batched Ollama call instead of
	// one per document.
	var fragments []model.SourceFragment
	for i, chunk := range result.Chunks {
		fragments = append(fragments, model.SourceFragment{
			ID:          model.FragmentID(doc.SourceType, doc.Path, i),
			RawContent:  chunk.Content,
			SourceType:  doc.SourceType,
			SourceName:  doc.SourceName,
			SourcePath:  doc.Path,
			SourceURI:   doc.SourceURI,
			ContentDate: contentDate,
			Author:      doc.Author,
			FileType:    fileType,
			Checksum:    doc.Checksum,
		})
	}

	return fragments, nil
}

// ExtractChunks extracts chunks from a single document using the extractor
// registry. It handles pre-chunked documents and extractor lookup. Returns
// an ExtractResult with nil/empty chunks (not an error) when the document yields no content.
func ExtractChunks(doc model.RawDocument, reg *extractor.Registry) (*extractor.ExtractResult, error) {
	if len(doc.Chunks) > 0 {
		return &extractor.ExtractResult{Chunks: doc.Chunks}, nil
	}
	ext := filepath.Ext(doc.Path)
	e := reg.Get(ext)
	result, err := e.Extract(doc.Content, extractor.ExtractOptions{Path: doc.Path})
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", doc.Path, err)
	}
	return result, nil
}

func batchSlice[T any](items []T, size int) [][]T {
	var batches [][]T
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}

func countOverlap(known map[string]string, docs []model.RawDocument) int {
	count := 0
	for _, d := range docs {
		if _, ok := known[d.Path]; ok {
			count++
		}
	}
	return count
}
