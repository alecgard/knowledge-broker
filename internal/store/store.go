package store

import (
	"context"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// Store persists and retrieves source fragments.
type Store interface {
	// UpsertFragments inserts or updates fragments (matched by ID).
	UpsertFragments(ctx context.Context, fragments []model.SourceFragment) error

	// SearchByVector finds the nearest fragments to the given embedding.
	SearchByVector(ctx context.Context, embedding []float32, limit int) ([]model.SourceFragment, error)

	// SearchByVectorFiltered finds the nearest fragments filtered by source names.
	// If sourceNames is empty, it behaves identically to SearchByVector.
	SearchByVectorFiltered(ctx context.Context, embedding []float32, limit int, sourceNames []string) ([]model.SourceFragment, error)

	// GetFragments retrieves fragments by ID.
	GetFragments(ctx context.Context, ids []string) ([]model.SourceFragment, error)

	// GetChecksums returns path -> checksum for all fragments of a source type and name.
	GetChecksums(ctx context.Context, sourceType, sourceName string) (map[string]string, error)

	// DeleteByPaths removes fragments matching the given source type, name, and paths.
	DeleteByPaths(ctx context.Context, sourceType, sourceName string, paths []string) error

	// ExportFragments returns all fragments with their embeddings.
	ExportFragments(ctx context.Context) ([]model.SourceFragment, error)

	// RegisterSource inserts or updates a registered source.
	RegisterSource(ctx context.Context, src model.Source) error

	// ListSources returns all registered sources.
	ListSources(ctx context.Context) ([]model.Source, error)

	// CountFragmentsBySource returns a map of "source_type/source_name" to fragment count.
	CountFragmentsBySource(ctx context.Context) (map[string]int, error)

	// DeleteFragmentsBySource removes all fragments and their embeddings for the given source.
	DeleteFragmentsBySource(ctx context.Context, sourceType, sourceName string) error

	// DeleteSource removes a source registration.
	DeleteSource(ctx context.Context, sourceType, sourceName string) error

	// Close releases resources.
	Close() error
}
