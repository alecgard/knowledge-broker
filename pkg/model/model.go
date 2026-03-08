// Package model contains the public types shared across Knowledge Broker
// components. External connector and extractor implementations should import
// this package.
package model

import "time"

// RawDocument is the output of a connector before extraction.
type RawDocument struct {
	Path         string
	Content      []byte
	LastModified time.Time
	Author       string
	SourceURI    string
	SourceType   string
	FileType     string
	Checksum     string
}

// Chunk is the output of an extractor.
type Chunk struct {
	Content  string
	Metadata map[string]string
}

// SourceFragment is a chunk of content extracted from a single source.
type SourceFragment struct {
	ID            string
	Content       string
	SourceType    string // "filesystem", "github"
	SourcePath    string
	SourceURI     string
	LastModified  time.Time
	Author        string
	FileType      string
	Checksum      string
	Embedding     []float32
	ConfidenceAdj float64 // cumulative adjustment from feedback
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string `json:"role"`    // "user", "assistant"
	Content string `json:"content"`
}

// QueryRequest is the input to the query engine.
type QueryRequest struct {
	Messages []Message `json:"messages"`
	Limit    int       `json:"limit,omitempty"`   // max fragments to retrieve, default 20
	Concise  bool      `json:"concise,omitempty"` // terse, agent-friendly output
}

// Answer is the response from the query engine.
type Answer struct {
	Content        string            `json:"content"`
	Confidence     ConfidenceSignals `json:"confidence"`
	Sources        []SourceRef       `json:"sources"`
	Contradictions []Contradiction   `json:"contradictions,omitempty"`
}

// ConfidenceSignals tracks four independent confidence dimensions.
type ConfidenceSignals struct {
	Freshness     float64 `json:"freshness"`
	Corroboration float64 `json:"corroboration"`
	Consistency   float64 `json:"consistency"`
	Authority     float64 `json:"authority"`
}

// SourceRef links an answer back to a source fragment.
type SourceRef struct {
	FragmentID string  `json:"fragment_id"`
	SourceURI  string  `json:"source_uri"`
	SourcePath string  `json:"source_path"`
	Relevance  float64 `json:"relevance,omitempty"`
}

// Contradiction describes conflicting claims between sources.
type Contradiction struct {
	Claim       string      `json:"claim"`
	Sources     []SourceRef `json:"sources"`
	Explanation string      `json:"explanation"`
}

// FeedbackType categorises feedback.
type FeedbackType string

const (
	FeedbackCorrection   FeedbackType = "correction"
	FeedbackChallenge    FeedbackType = "challenge"
	FeedbackConfirmation FeedbackType = "confirmation"
)

// Feedback records user feedback on a fragment.
type Feedback struct {
	FragmentID string       `json:"fragment_id"`
	Type       FeedbackType `json:"type"`
	Content    string       `json:"content,omitempty"`
	Evidence   string       `json:"evidence,omitempty"`
}
