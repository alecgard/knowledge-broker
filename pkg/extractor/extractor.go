// Package extractor defines the interface for Knowledge Broker content extractors.
// Implement this interface to create custom extractors for proprietary file formats
// in external repositories.
package extractor

import "github.com/knowledge-broker/knowledge-broker/pkg/model"

// ExtractOptions holds parameters for an Extract call.
type ExtractOptions struct {
	// Path is the file path, used for context in chunk metadata.
	Path string
}

// Extractor turns raw file content into chunks.
type Extractor interface {
	// FileTypes returns the file extensions this extractor handles (e.g., ".md", ".go").
	FileTypes() []string

	// Extract splits content into chunks.
	Extract(content []byte, opts ExtractOptions) ([]model.Chunk, error)
}
