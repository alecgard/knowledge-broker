package store

import (
	"context"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// CachedAnswer is a query result retrieved from the disk cache.
type CachedAnswer struct {
	AnswerJSON   []byte
	FragmentSigs string
	CreatedAt    time.Time
}

// Store persists and retrieves source fragments.
type Store interface {
	// UpsertFragments inserts or updates fragments (matched by ID).
	UpsertFragments(ctx context.Context, fragments []model.SourceFragment) error

	// SearchByVector finds the nearest fragments to the given embedding.
	SearchByVector(ctx context.Context, embedding []float32, limit int) ([]model.SourceFragment, error)

	// SearchByVectorFiltered finds the nearest fragments filtered by source names and/or types.
	// If both sourceNames and sourceTypes are empty, it behaves identically to SearchByVector.
	SearchByVectorFiltered(ctx context.Context, embedding []float32, limit int, sourceNames []string, sourceTypes []string) ([]model.SourceFragment, error)

	// SearchByFTS performs full-text keyword search using BM25 ranking.
	SearchByFTS(ctx context.Context, query string, limit int) ([]model.SourceFragment, error)

	// SearchByFTSFiltered performs full-text search filtered by source names and/or types.
	SearchByFTSFiltered(ctx context.Context, query string, limit int, sourceNames []string, sourceTypes []string) ([]model.SourceFragment, error)

	// GetFragments retrieves fragments by ID.
	GetFragments(ctx context.Context, ids []string) ([]model.SourceFragment, error)

	// GetChecksums returns path -> checksum for all fragments of a source type and name.
	GetChecksums(ctx context.Context, sourceType, sourceName string) (map[string]string, error)

	// DeleteByPaths removes fragments matching the given source type, name, and paths.
	DeleteByPaths(ctx context.Context, sourceType, sourceName string, paths []string) error

	// ExportFragments returns all fragments with their embeddings.
	ExportFragments(ctx context.Context) ([]model.SourceFragment, error)

	// GetFragmentsBySource returns all fragments for a given source name, with embeddings.
	GetFragmentsBySource(ctx context.Context, sourceName string) ([]model.SourceFragment, error)

	// RegisterSource inserts or updates a registered source.
	RegisterSource(ctx context.Context, src model.Source) error

	// ListSources returns all registered sources.
	ListSources(ctx context.Context) ([]model.Source, error)

	// CountFragmentsBySource returns a map of "source_type/source_name" to fragment count.
	CountFragmentsBySource(ctx context.Context) (map[string]int, error)

	// DeleteFragmentsBySource removes all fragments and their embeddings for the given source.
	DeleteFragmentsBySource(ctx context.Context, sourceType, sourceName string) error

	// GetSource retrieves a single source by type and name. Returns nil if not found.
	GetSource(ctx context.Context, sourceType, sourceName string) (*model.Source, error)

	// UpdateSourceDescription sets the description for an existing source.
	// If force is false and the source already has a non-empty description, it returns an error.
	UpdateSourceDescription(ctx context.Context, sourceType, sourceName, description string, force bool) error

	// DeleteSource removes a source registration.
	DeleteSource(ctx context.Context, sourceType, sourceName string) error

	// UpsertKnowledgeUnit inserts or replaces a knowledge unit and its fragment associations.
	UpsertKnowledgeUnit(ctx context.Context, unit model.KnowledgeUnit) error

	// ListKnowledgeUnits returns all knowledge units with their fragment IDs.
	ListKnowledgeUnits(ctx context.Context) ([]model.KnowledgeUnit, error)

	// GetKnowledgeUnit retrieves a single knowledge unit by ID.
	GetKnowledgeUnit(ctx context.Context, id string) (*model.KnowledgeUnit, error)

	// SearchKnowledgeUnits finds the nearest knowledge units by centroid embedding.
	SearchKnowledgeUnits(ctx context.Context, embedding []float32, limit int) ([]model.KnowledgeUnit, error)

	// DeleteAllKnowledgeUnits removes all knowledge units and their associations.
	DeleteAllKnowledgeUnits(ctx context.Context) error

	// GetCachedAnswer retrieves a cached answer by cache key. Returns nil if not found or expired.
	GetCachedAnswer(ctx context.Context, cacheKey string, maxAge time.Duration) (*CachedAnswer, error)

	// PutCachedAnswer stores a query answer in the disk cache.
	PutCachedAnswer(ctx context.Context, cacheKey, queryText string, concise bool, fragmentSigs string, answer []byte) error

	// PruneCacheEntries deletes entries older than maxAge. Called opportunistically.
	PruneCacheEntries(ctx context.Context, maxAge time.Duration) error

	// Close releases resources.
	Close() error
}
