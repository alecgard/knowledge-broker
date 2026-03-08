// Package extractor defines the interface for Knowledge Broker content extractors.
// Implement this interface to create custom extractors for proprietary file formats
// in external repositories.
package extractor

import "github.com/knowledge-broker/knowledge-broker/pkg/model"

// Extractor turns raw file content into chunks.
type Extractor interface {
	// FileTypes returns the file extensions this extractor handles (e.g., ".md", ".go").
	FileTypes() []string

	// Extract splits content into chunks.
	Extract(content []byte, path string) ([]model.Chunk, error)
}
