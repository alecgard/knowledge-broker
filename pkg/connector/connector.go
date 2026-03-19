// Package connector defines the interface for Knowledge Broker source connectors.
// Implement this interface to create custom connectors in external repositories.
package connector

import (
	"context"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// ScanOptions holds parameters for a Scan call.
type ScanOptions struct {
	// Known maps paths to their checksums for incremental ingestion.
	// The connector may skip files whose checksum hasn't changed.
	Known map[string]string

	// Force skips incremental optimizations (e.g. diff-based scan) and
	// processes all files.
	Force bool

	// LastIngest is the timestamp of the last successful ingestion for this source.
	// Connectors that support time-based filtering (e.g., Slack) can use this
	// to narrow the scan window instead of always fetching the full lookback period.
	// Nil means no previous ingestion (first scan).
	LastIngest *time.Time
}

// ScanEvent represents a single event from a streaming scan.
type ScanEvent struct {
	Doc     *model.RawDocument // non-nil for a new/changed document
	Deleted []string           // non-empty only in the final event (scan complete)
	Err     error              // non-nil signals a fatal scan error
}

// StreamingConnector extends Connector with streaming scan support.
// Connectors that implement this interface allow the pipeline to start
// processing documents before the full scan completes.
type StreamingConnector interface {
	Connector
	ScanStream(ctx context.Context, opts ScanOptions) <-chan ScanEvent
}

// Connector pulls documents from a source.
type Connector interface {
	// Name returns the connector type identifier (e.g., "filesystem", "git", "confluence").
	Name() string

	// SourceName returns a human-readable name for this specific source
	// (e.g., "owner/repo" for git, directory name for filesystem).
	SourceName() string

	// Config returns the connector's configuration for source registration.
	// The mode indicates how the source is being ingested (local or push).
	// Connectors may omit local-only details (e.g., file paths) for push mode.
	Config(mode string) map[string]string

	// Scan returns documents from the source. Pass known checksums via opts
	// for incremental ingestion — the connector may skip unchanged files.
	// Returns: new/changed docs, deleted paths, error.
	Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error)
}
