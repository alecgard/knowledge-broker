package ingest

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// Pipeline orchestrates the ingestion of documents.
type Pipeline struct {
	store       store.Store
	embedder    embedding.Embedder
	extractors  *extractor.Registry
	workers     int
	logger      *slog.Logger
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
	known, err := p.store.GetChecksums(ctx, conn.Name())
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
		if err := p.store.DeleteByPaths(ctx, conn.Name(), deleted); err != nil {
			return nil, fmt.Errorf("delete: %w", err)
		}
		result.Deleted = len(deleted)
	}

	result.Skipped = len(known) - len(deleted) - countOverlap(known, docs)

	// Process new/changed documents through extract → embed → store.
	fragments, errs := p.processDocuments(ctx, docs)
	result.Errors = errs

	if len(fragments) == 0 {
		p.logger.Info("ingestion complete", "added", 0, "deleted", result.Deleted, "skipped", result.Skipped)
		return result, nil
	}

	// Store fragments.
	if err := p.store.UpsertFragments(ctx, fragments); err != nil {
		return nil, fmt.Errorf("upsert fragments: %w", err)
	}
	result.Added = len(fragments)

	p.logger.Info("ingestion complete",
		"added", result.Added,
		"deleted", result.Deleted,
		"skipped", result.Skipped,
		"errors", result.Errors,
	)

	return result, nil
}

func (p *Pipeline) processDocuments(ctx context.Context, docs []model.RawDocument) ([]model.SourceFragment, int) {
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
	errCount := 0
	for r := range results {
		if r.err != nil {
			p.logger.Warn("failed to process document", "error", r.err)
			errCount++
			continue
		}
		allFragments = append(allFragments, r.fragments...)
	}

	return allFragments, errCount
}

func (p *Pipeline) processDocument(ctx context.Context, doc model.RawDocument) ([]model.SourceFragment, error) {
	// Use pre-provided chunks when available; otherwise run the extractor.
	var chunks []model.Chunk
	if len(doc.Chunks) > 0 {
		chunks = doc.Chunks
	} else {
		ext := filepath.Ext(doc.Path)
		e := p.extractors.Get(ext)
		var err error
		chunks, err = e.Extract(doc.Content, extractor.ExtractOptions{Path: doc.Path})
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", doc.Path, err)
		}
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

	// Build fragments.
	fragments := make([]model.SourceFragment, len(chunks))
	for i, chunk := range chunks {
		// Generate a unique ID from source + path + chunk index.
		idInput := fmt.Sprintf("%s:%s:%d", doc.SourceType, doc.Path, i)
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(idInput)))[:16]

		fragments[i] = model.SourceFragment{
			ID:           id,
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
		}
	}

	return fragments, nil
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
