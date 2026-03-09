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
	// Name returns the connector type identifier (e.g., "filesystem", "git", "confluence").
	Name() string

	// SourceName returns a human-readable name for this specific source
	// (e.g., "owner/repo" for git, directory name for filesystem).
	SourceName() string

	// Config returns the connector's configuration for source registration.
	// Keys are connector-specific (e.g., "url", "branch" for git; "path" for filesystem).
	Config() map[string]string

	// Scan returns documents from the source. Pass known checksums via opts
	// for incremental ingestion — the connector may skip unchanged files.
	// Returns: new/changed docs, deleted paths, error.
	Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error)
}
