// Package model re-exports the public model types for internal use.
// All types are defined in pkg/model. Internal packages should import
// this package for convenience.
package model

import "github.com/knowledge-broker/knowledge-broker/pkg/model"

// Re-export all public types so internal packages can continue importing
// "internal/model" without changes.
type (
	RawDocument      = model.RawDocument
	Chunk            = model.Chunk
	SourceFragment   = model.SourceFragment
	Message          = model.Message
	QueryRequest     = model.QueryRequest
	Answer           = model.Answer
	ConfidenceSignals = model.ConfidenceSignals
	SourceRef        = model.SourceRef
	Contradiction    = model.Contradiction
	FeedbackType     = model.FeedbackType
	Feedback         = model.Feedback
)

// Re-export constants.
const (
	FeedbackCorrection   = model.FeedbackCorrection
	FeedbackChallenge    = model.FeedbackChallenge
	FeedbackConfirmation = model.FeedbackConfirmation
)
