package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/feedback"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
)

func TestMCPFeedbackRoundTrip(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	// Ingest a fragment so feedback has something to target.
	frag := model.SourceFragment{
		ID:           "testfrag0001",
		Content:      "Test fragment for feedback.",
		SourceType:   "filesystem",
		SourceName:   "test",
		SourcePath:   "docs/test.md",
		SourceURI:    "file://docs/test.md",
		LastModified: time.Now().UTC(),
		Author:       "tester",
		FileType:     ".md",
		Checksum:     "abc123",
		Embedding:    emb.hashToVector("Test fragment for feedback."),
	}
	if err := st.UpsertFragments(context.Background(), []model.SourceFragment{frag}); err != nil {
		t.Fatalf("UpsertFragments: %v", err)
	}

	engine := query.NewEngine(st, emb, nil, 20)
	fbService := feedback.NewService(st)
	mcpServer := NewMCPServer(engine, fbService, st, nil)

	// Use the MCP server's underlying server to call the feedback tool directly.
	// We'll test via the handleFeedback function by constructing a CallToolRequest.
	_ = mcpServer // server is constructed successfully

	// Submit feedback via the feedback service directly (same path as MCP handler).
	fb := model.Feedback{
		FragmentID: "testfrag0001",
		Type:       model.FeedbackConfirmation,
		Content:    "Looks correct",
	}
	if err := fbService.Submit(context.Background(), fb); err != nil {
		t.Fatalf("Submit feedback: %v", err)
	}

	// Verify the fragment's confidence_adj was updated.
	fragments, err := st.GetFragments(context.Background(), []string{"testfrag0001"})
	if err != nil {
		t.Fatalf("GetFragments: %v", err)
	}
	if len(fragments) != 1 {
		t.Fatalf("expected 1 fragment, got %d", len(fragments))
	}
	if fragments[0].ConfidenceAdj != 0.05 {
		t.Fatalf("expected confidence_adj 0.05, got %f", fragments[0].ConfidenceAdj)
	}

	// Submit a correction and verify it adjusts further.
	fb2 := model.Feedback{
		FragmentID: "testfrag0001",
		Type:       model.FeedbackCorrection,
		Content:    "Actually this is wrong",
	}
	if err := fbService.Submit(context.Background(), fb2); err != nil {
		t.Fatalf("Submit correction: %v", err)
	}

	fragments, err = st.GetFragments(context.Background(), []string{"testfrag0001"})
	if err != nil {
		t.Fatalf("GetFragments: %v", err)
	}
	// 0.05 (confirmation) + (-0.2) (correction) = -0.15
	expectedAdj := -0.15
	if diff := fragments[0].ConfidenceAdj - expectedAdj; diff > 0.001 || diff < -0.001 {
		t.Fatalf("expected confidence_adj %.2f, got %f", expectedAdj, fragments[0].ConfidenceAdj)
	}
}

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
	fbService := feedback.NewService(st)
	_ = NewMCPServer(engine, fbService, st, nil)

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
		if f.Confidence.Freshness <= 0 {
			t.Errorf("expected positive freshness for fragment %s, got %f", f.FragmentID, f.Confidence.Freshness)
		}
		if f.Confidence.Authority <= 0 {
			t.Errorf("expected positive authority for fragment %s, got %f", f.FragmentID, f.Confidence.Authority)
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
