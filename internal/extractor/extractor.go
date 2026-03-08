// Package extractor provides the internal extractor interface backed by the
// public pkg/extractor.Extractor type, plus the Registry for mapping file
// types to extractors.
package extractor

import "github.com/knowledge-broker/knowledge-broker/pkg/extractor"

// Extractor is the public extractor interface.
type Extractor = extractor.Extractor

// ExtractOptions is the public ExtractOptions type.
type ExtractOptions = extractor.ExtractOptions

// Registry maps file extensions to extractors.
type Registry struct {
	extractors map[string]Extractor
	fallback   Extractor
}

// NewRegistry creates a registry with the given fallback extractor.
func NewRegistry(fallback Extractor) *Registry {
	return &Registry{
		extractors: make(map[string]Extractor),
		fallback:   fallback,
	}
}

// Register adds an extractor for its declared file types.
func (r *Registry) Register(e Extractor) {
	for _, ft := range e.FileTypes() {
		r.extractors[ft] = e
	}
}

// Get returns the extractor for a file extension, or the fallback.
func (r *Registry) Get(ext string) Extractor {
	if e, ok := r.extractors[ext]; ok {
		return e
	}
	return r.fallback
}
