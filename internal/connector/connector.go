// Package connector provides the internal connector interface backed by the
// public pkg/connector.Connector type. Internal code uses this package;
// external connector authors import pkg/connector directly.
package connector

import "github.com/knowledge-broker/knowledge-broker/pkg/connector"

// Connector is the public connector interface.
type Connector = connector.Connector

// ScanOptions is the public ScanOptions type.
type ScanOptions = connector.ScanOptions
