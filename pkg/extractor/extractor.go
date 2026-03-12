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

// ExtractResult holds the output of an Extract call: chunks and optional
// document-level metadata (e.g., "content_date" from PDF info dictionaries).
type ExtractResult struct {
	Chunks   []model.Chunk
	Metadata map[string]string
}

// Extractor turns raw file content into chunks.
type Extractor interface {
	// FileTypes returns the file extensions this extractor handles (e.g., ".md", ".go").
	FileTypes() []string

	// Extract splits content into chunks and returns optional document metadata.
	Extract(content []byte, opts ExtractOptions) (*ExtractResult, error)
}
