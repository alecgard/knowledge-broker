package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
)

func TestMCPQueryRaw(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	// Ingest fragments.
	frags := []model.SourceFragment{
		{
			ID:           "rawfrag0001",
			Content:      "Authentication uses JWT tokens.",
			SourceType:   "filesystem",
			SourceName:   "docs",
			SourcePath:   "docs/auth.md",
			SourceURI:    "file://docs/auth.md",
			LastModified: time.Now().UTC(),
			Author:       "alice",
			FileType:     ".md",
			Checksum:     "aaa111",
			Embedding:    emb.hashToVector("Authentication uses JWT tokens."),
		},
		{
			ID:           "rawfrag0002",
			Content:      "Authorization checks user roles.",
			SourceType:   "filesystem",
			SourceName:   "docs",
			SourcePath:   "docs/authz.md",
			SourceURI:    "file://docs/authz.md",
			LastModified: time.Now().UTC(),
			Author:       "bob",
			FileType:     ".md",
			Checksum:     "bbb222",
			Embedding:    emb.hashToVector("Authorization checks user roles."),
		},
	}
	if err := st.UpsertFragments(context.Background(), frags); err != nil {
		t.Fatalf("UpsertFragments: %v", err)
	}

	// Create engine with nil LLM — raw mode should work.
	engine := query.NewEngine(st, emb, nil, 20)
	_ = NewMCPServer(engine, st, nil)

	// Call QueryRaw directly to verify it works without LLM.
	req := model.QueryRequest{
		Messages: []model.Message{{Role: "user", Content: "authentication"}},
		Limit:    10,
	}
	result, err := engine.QueryRaw(context.Background(), req)
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}

	if len(result.Fragments) == 0 {
		t.Fatal("expected at least one fragment from raw query")
	}

	// Verify the result marshals to valid JSON.
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal RawResult: %v", err)
	}

	var decoded model.RawResult
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("unmarshal RawResult: %v", err)
	}
	if len(decoded.Fragments) == 0 {
		t.Fatal("expected fragments after round-trip JSON")
	}

	// Verify confidence signals are populated.
	for _, f := range decoded.Fragments {
		if f.Confidence.Breakdown.Freshness <= 0 {
			t.Errorf("expected positive freshness for fragment %s, got %f", f.FragmentID, f.Confidence.Breakdown.Freshness)
		}
		if f.Confidence.Breakdown.Authority <= 0 {
			t.Errorf("expected positive authority for fragment %s, got %f", f.FragmentID, f.Confidence.Breakdown.Authority)
		}
		if f.Confidence.Overall <= 0 {
			t.Errorf("expected positive overall for fragment %s, got %f", f.FragmentID, f.Confidence.Overall)
		}
	}
}

func TestMCPNilLLMQueryReturnsError(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	engine := query.NewEngine(st, emb, nil, 20)

	// Query (non-raw) should fail with nil LLM.
	req := model.QueryRequest{
		Messages: []model.Message{{Role: "user", Content: "test"}},
	}
	_, err := engine.Query(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error when LLM is nil")
	}
	if err.Error() != "LLM client not configured" {
		t.Fatalf("unexpected error: %v", err)
	}
}
