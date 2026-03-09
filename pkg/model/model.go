// Package model contains the public types shared across Knowledge Broker
// components. External connector and extractor implementations should import
// this package.
package model

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// SourceType constants for connector types.
const (
	SourceTypeFilesystem = "filesystem"
	SourceTypeGit        = "git"
)

// FragmentID generates a deterministic fragment ID from the source type,
// source path, and chunk index: sha256(sourceType:sourcePath:index)[:16].
func FragmentID(sourceType, sourcePath string, index int) string {
	idInput := fmt.Sprintf("%s:%s:%d", sourceType, sourcePath, index)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(idInput)))[:16]
}

// IngestFragment is a single fragment in an ingest request (without ID or embedding).
type IngestFragment struct {
	Content      string    `json:"content"`
	SourceType   string    `json:"source_type"`
	SourceName   string    `json:"source_name,omitempty"`
	SourcePath   string    `json:"source_path"`
	SourceURI    string    `json:"source_uri"`
	LastModified time.Time `json:"last_modified"`
	Author       string    `json:"author"`
	FileType     string    `json:"file_type"`
	Checksum     string    `json:"checksum"`
}

// IngestDeletedPath identifies a source type, source name, and path to delete.
type IngestDeletedPath struct {
	SourceType string `json:"source_type"`
	SourceName string `json:"source_name,omitempty"`
	Path       string `json:"path"`
}

// IngestRequest is the JSON body for POST /v1/ingest.
type IngestRequest struct {
	Fragments []IngestFragment  `json:"fragments"`
	Deleted   []IngestDeletedPath `json:"deleted,omitempty"`
}

// SourceMode indicates how a source was ingested.
const (
	SourceModeLocal = "local" // ingested locally, re-runnable via --all
	SourceModePush  = "push"  // pushed to a remote server
)

// Source represents a registered ingestion source.
type Source struct {
	SourceType string            `json:"source_type"`
	SourceName string            `json:"source_name"`
	Config     map[string]string `json:"config"`
	LastIngest time.Time         `json:"last_ingest"`
}

// RawDocument is the output of a connector before extraction.
type RawDocument struct {
	Path         string
	Content      []byte
	LastModified time.Time
	Author       string
	SourceURI    string
	SourceType   string
	SourceName   string
	Checksum     string
	Chunks       []Chunk // optional pre-chunked content; skips extractor when set
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
	SourceType    string // "filesystem", "git"
	SourceName    string
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
	Topics   []string  `json:"topics,omitempty"`  // optional topics to boost relevance (e.g., "authentication", "octroi")
	Stream   *bool     `json:"stream,omitempty"`  // stream SSE responses; default false (single JSON response)
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
	SourceName string  `json:"source_name,omitempty"`
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
