package store

import (
	"context"

	"github.com/knowledge-broker/knowledge-broker/internal/model"
)

// Store persists and retrieves source fragments.
type Store interface {
	// UpsertFragments inserts or updates fragments (matched by ID).
	UpsertFragments(ctx context.Context, fragments []model.SourceFragment) error

	// SearchByVector finds the nearest fragments to the given embedding.
	SearchByVector(ctx context.Context, embedding []float32, limit int) ([]model.SourceFragment, error)

	// GetFragments retrieves fragments by ID.
	GetFragments(ctx context.Context, ids []string) ([]model.SourceFragment, error)

	// GetChecksums returns path -> checksum for all fragments of a source type.
	GetChecksums(ctx context.Context, sourceType string) (map[string]string, error)

	// DeleteByPaths removes fragments matching the given source type and paths.
	DeleteByPaths(ctx context.Context, sourceType string, paths []string) error

	// RecordFeedback stores feedback for a fragment.
	RecordFeedback(ctx context.Context, fb model.Feedback) error

	// Close releases resources.
	Close() error
}
