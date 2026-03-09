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

// FromSource reconstructs a Connector from a registered Source.
func FromSource(src model.Source, gitHubClientID string) (Connector, error) {
	switch src.SourceType {
	case model.SourceTypeGit:
		return NewGitConnector(src.Config["url"], src.Config["branch"], gitHubClientID), nil
	case model.SourceTypeFilesystem:
		path := src.Config["path"]
		if path == "" {
			return nil, fmt.Errorf("filesystem source %q missing path in config", src.SourceName)
		}
		return NewFilesystemConnector(path), nil
	default:
		return nil, fmt.Errorf("unknown source type: %s", src.SourceType)
	}
}
