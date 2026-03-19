// Package connector provides the internal connector interface backed by the
// public pkg/connector.Connector type. Internal code uses this package;
// external connector authors import pkg/connector directly.
package connector

import (
	"fmt"

	"github.com/knowledge-broker/knowledge-broker/pkg/connector"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// Connector is the public connector interface.
type Connector = connector.Connector

// ScanOptions is the public ScanOptions type.
type ScanOptions = connector.ScanOptions

// ScanEvent is the public ScanEvent type.
type ScanEvent = connector.ScanEvent

// StreamingConnector is the public StreamingConnector interface.
type StreamingConnector = connector.StreamingConnector

// Factory creates a Connector from source config.
type Factory func(config map[string]string) (Connector, error)

// registry maps source type names to their factory functions.
var registry = map[string]Factory{}

// Register adds a connector factory for the given source type.
func Register(sourceType string, factory Factory) {
	registry[sourceType] = factory
}

// FromSource reconstructs a Connector from a registered Source.
func FromSource(src model.Source) (Connector, error) {
	factory, ok := registry[src.SourceType]
	if !ok {
		return nil, fmt.Errorf("unknown source type: %s", src.SourceType)
	}
	return factory(src.Config)
}
