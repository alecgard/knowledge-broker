// Package connector defines the interface for Knowledge Broker source connectors.
// Implement this interface to create custom connectors in external repositories.
package connector

import (
	"context"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// ScanOptions holds parameters for a Scan call.
type ScanOptions struct {
	// Known maps paths to their checksums for incremental ingestion.
	// The connector may skip files whose checksum hasn't changed.
	Known map[string]string
}

// Connector pulls documents from a source.
type Connector interface {
	// Name returns the connector type identifier (e.g., "filesystem", "github", "confluence").
	Name() string

	// Scan returns documents from the source. Pass known checksums via opts
	// for incremental ingestion — the connector may skip unchanged files.
	// Returns: new/changed docs, deleted paths, error.
	Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error)
}
