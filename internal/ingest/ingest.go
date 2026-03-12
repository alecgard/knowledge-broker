package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// ProgressFunc is called during document processing to report progress.
// completed is the number of documents processed so far, total is the
// total number of documents to process.
type ProgressFunc func(completed, total int)

// BatchFunc is called after each batch is committed to the store.
// batch is 1-indexed, totalBatches is the total number of batches,
// and added is the number of fragments stored in this batch.
type BatchFunc func(batch, totalBatches, added int)

// Pipeline orchestrates the ingestion of documents.
type Pipeline struct {
	store       store.Store
	embedder    embedding.Embedder
	extractors  *extractor.Registry
	workers     int
	logger      *slog.Logger
	OnProgress  ProgressFunc
	OnBatchDone BatchFunc
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

// Result summarises an ingestion run.
type Result struct {
	Added   int
	Updated int
	Deleted int
	Skipped int
	Errors  int
}

// Run executes the ingestion pipeline for a connector.
func (p *Pipeline) Run(ctx context.Context, conn connector.Connector) (*Result, error) {
	result := &Result{}

	// Get known checksums for incremental ingestion.
	known, err := p.store.GetChecksums(ctx, conn.Name(), conn.SourceName())
	if err != nil {
		return nil, fmt.Errorf("get checksums: %w", err)
	}

	// Scan for new/changed documents and deleted paths.
	docs, deleted, err := conn.Scan(ctx, connector.ScanOptions{Known: known})
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

		fragments, errs := p.processDocuments(ctx, batch, progressOffset, totalDocs)
		progressOffset += len(batch)
		result.Errors += errs

		if len(fragments) > 0 {
			if err := p.store.UpsertFragments(ctx, fragments); err != nil {
				return result, fmt.Errorf("upsert fragments: %w", err)
			}
			result.Added += len(fragments)
		}

		if p.OnBatchDone != nil {
			p.OnBatchDone(batchIdx+1, len(batches), len(fragments))
		}
	}

	p.logger.Info("ingestion complete",
		"added", result.Added,
		"deleted", result.Deleted,
		"skipped", result.Skipped,
		"errors", result.Errors,
	)

	return result, nil
}

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
	chunks, err := ExtractChunks(doc, p.extractors)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		return nil, nil
	}

	// Derive file type from path extension.
	fileType := filepath.Ext(doc.Path)

	// Embed chunks.
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embeddings, err := p.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed %s: %w", doc.Path, err)
	}

	// Build fragments, skipping any chunks whose embedding failed (nil).
	var fragments []model.SourceFragment
	for i, chunk := range chunks {
		if embeddings[i] == nil {
			p.logger.Warn("skipping chunk with nil embedding",
				"path", doc.Path, "chunk_index", i, "chunk_length", len(chunk.Content))
			continue
		}
		fragments = append(fragments, model.SourceFragment{
			ID:           model.FragmentID(doc.SourceType, doc.Path, i),
			Content:      chunk.Content,
			SourceType:   doc.SourceType,
			SourceName:   doc.SourceName,
			SourcePath:   doc.Path,
			SourceURI:    doc.SourceURI,
			LastModified: doc.LastModified,
			Author:       doc.Author,
			FileType:     fileType,
			Checksum:     doc.Checksum,
			Embedding:    embeddings[i],
		})
	}

	return fragments, nil
}

// ExtractChunks extracts chunks from a single document using the extractor
// registry. It handles pre-chunked documents and extractor lookup. Returns
// nil chunks (not an error) when the document yields no content.
func ExtractChunks(doc model.RawDocument, reg *extractor.Registry) ([]model.Chunk, error) {
	if len(doc.Chunks) > 0 {
		return doc.Chunks, nil
	}
	ext := filepath.Ext(doc.Path)
	e := reg.Get(ext)
	chunks, err := e.Extract(doc.Content, extractor.ExtractOptions{Path: doc.Path})
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", doc.Path, err)
	}
	return chunks, nil
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
