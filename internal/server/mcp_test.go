package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
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

func TestMCPKBInstructionsPrompt(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	ctx := context.Background()

	// Register sources with descriptions.
	if err := st.RegisterSource(ctx, model.Source{
		SourceType:  "git",
		SourceName:  "owner/repo",
		Description: "Payment processing service",
		Config:      map[string]string{"mode": "local"},
		LastIngest:  time.Now(),
	}); err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}
	if err := st.RegisterSource(ctx, model.Source{
		SourceType: "filesystem",
		SourceName: "docs",
		Config:     map[string]string{"mode": "local"},
		LastIngest: time.Now(),
	}); err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}

	engine := query.NewEngine(st, emb, nil, 20)
	mcpSrv := NewMCPServer(engine, st, nil)

	// Call the handler directly.
	result, err := mcpSrv.handleKBInstructions(ctx, mcp.GetPromptRequest{})
	if err != nil {
		t.Fatalf("handleKBInstructions: %v", err)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}

	content, ok := result.Messages[0].Content.(mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	text := content.Text

	// Should contain source with description.
	if !strings.Contains(text, "Payment processing service") {
		t.Error("expected prompt to contain source description")
	}

	// Should contain derived label for source without description.
	if !strings.Contains(text, "Local directory: docs") {
		t.Error("expected prompt to contain derived label for filesystem source")
	}

	// Should contain usage tips.
	if !strings.Contains(text, "query") {
		t.Error("expected prompt to mention query tool")
	}
	if !strings.Contains(text, "raw=true") {
		t.Error("expected prompt to mention raw mode")
	}
}

// ---------- splitCSV helper ----------

func TestMCPSplitCSV(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		key  string
		want []string
	}{
		{
			name: "empty string",
			args: map[string]interface{}{"k": ""},
			key:  "k",
			want: nil,
		},
		{
			name: "missing key",
			args: map[string]interface{}{},
			key:  "k",
			want: nil,
		},
		{
			name: "single value",
			args: map[string]interface{}{"k": "alpha"},
			key:  "k",
			want: []string{"alpha"},
		},
		{
			name: "multiple values trimmed",
			args: map[string]interface{}{"k": " a , b , c "},
			key:  "k",
			want: []string{"a", "b", "c"},
		},
		{
			name: "trailing comma produces no empty part",
			args: map[string]interface{}{"k": "x,,y,"},
			key:  "k",
			want: []string{"x", "y"},
		},
		{
			name: "non-string value",
			args: map[string]interface{}{"k": 42},
			key:  "k",
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitCSV(tc.args, tc.key)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitCSV(%v, %q) = %v, want %v", tc.args, tc.key, got, tc.want)
			}
		})
	}
}

// ---------- handleQuery ----------

// newCallToolRequest is a helper to build a CallToolRequest with the given arguments.
func newCallToolRequest(args map[string]interface{}) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

// resultText extracts the text from the first TextContent in a CallToolResult.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func TestMCPHandleQueryMissingParam(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	engine := query.NewEngine(st, emb, nil, 20)
	mcpSrv := NewMCPServer(engine, st, nil)

	ctx := context.Background()

	// No arguments at all.
	result, err := mcpSrv.handleQuery(ctx, newCallToolRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for missing query")
	}
	text := resultText(t, result)
	if !strings.Contains(text, "query is required") {
		t.Errorf("expected 'query is required' error, got: %s", text)
	}
}

func TestMCPHandleQueryEmptyString(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	engine := query.NewEngine(st, emb, nil, 20)
	mcpSrv := NewMCPServer(engine, st, nil)

	ctx := context.Background()

	result, err := mcpSrv.handleQuery(ctx, newCallToolRequest(map[string]interface{}{
		"query": "",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for empty query")
	}
	if text := resultText(t, result); !strings.Contains(text, "query is required") {
		t.Errorf("expected 'query is required' error, got: %s", text)
	}
}

func TestMCPHandleQueryNoLLMSynthesisMode(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	engine := query.NewEngine(st, emb, nil, 20) // nil LLM
	mcpSrv := NewMCPServer(engine, st, nil)

	ctx := context.Background()

	// Default mode (raw=false) should fail when LLM is nil.
	result, err := mcpSrv.handleQuery(ctx, newCallToolRequest(map[string]interface{}{
		"query": "test question",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when LLM is nil and synthesis mode")
	}
	text := resultText(t, result)
	if !strings.Contains(text, "ANTHROPIC_API_KEY") {
		t.Errorf("expected API key error message, got: %s", text)
	}
}

func TestMCPHandleQueryRawMode(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	ctx := context.Background()

	// Insert test fragments.
	frags := []model.SourceFragment{
		{
			ID:           "hq_raw_001",
			Content:      "Deploy using Docker containers.",
			SourceType:   "filesystem",
			SourceName:   "docs",
			SourcePath:   "docs/deploy.md",
			SourceURI:    "file://docs/deploy.md",
			LastModified: time.Now().UTC(),
			Author:       "carol",
			FileType:     ".md",
			Checksum:     "deploy111",
			Embedding:    emb.hashToVector("Deploy using Docker containers."),
		},
	}
	if err := st.UpsertFragments(ctx, frags); err != nil {
		t.Fatalf("UpsertFragments: %v", err)
	}

	engine := query.NewEngine(st, emb, nil, 20)
	mcpSrv := NewMCPServer(engine, st, nil)

	result, err := mcpSrv.handleQuery(ctx, newCallToolRequest(map[string]interface{}{
		"query": "docker deploy",
		"raw":   true,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, result))
	}

	// Parse the JSON response.
	text := resultText(t, result)
	var raw model.RawResult
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatalf("unmarshal RawResult: %v", err)
	}
	if len(raw.Fragments) == 0 {
		t.Fatal("expected at least one fragment in raw result")
	}
}

func TestMCPHandleQueryCustomLimit(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	ctx := context.Background()

	// Insert several fragments.
	for i := 0; i < 5; i++ {
		content := strings.Repeat("x", i+1) + " API endpoint docs"
		frag := model.SourceFragment{
			ID:           fmt.Sprintf("hq_limit_%03d", i),
			Content:      content,
			SourceType:   "filesystem",
			SourceName:   "docs",
			SourcePath:   fmt.Sprintf("docs/api%d.md", i),
			SourceURI:    fmt.Sprintf("file://docs/api%d.md", i),
			LastModified: time.Now().UTC(),
			FileType:     ".md",
			Checksum:     fmt.Sprintf("limit%d", i),
			Embedding:    emb.hashToVector(content),
		}
		if err := st.UpsertFragments(ctx, []model.SourceFragment{frag}); err != nil {
			t.Fatalf("UpsertFragments: %v", err)
		}
	}

	engine := query.NewEngine(st, emb, nil, 20)
	mcpSrv := NewMCPServer(engine, st, nil)

	result, err := mcpSrv.handleQuery(ctx, newCallToolRequest(map[string]interface{}{
		"query": "API endpoint",
		"raw":   true,
		"limit": float64(2), // JSON numbers arrive as float64
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, result))
	}

	var raw model.RawResult
	if err := json.Unmarshal([]byte(resultText(t, result)), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(raw.Fragments) > 2 {
		t.Errorf("expected at most 2 fragments with limit=2, got %d", len(raw.Fragments))
	}
}

func TestMCPHandleQueryCSVFilters(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	ctx := context.Background()

	// Insert fragments from different sources.
	frags := []model.SourceFragment{
		{
			ID:           "hq_csv_001",
			Content:      "Git repo auth docs",
			SourceType:   "git",
			SourceName:   "owner/repo",
			SourcePath:   "auth.md",
			SourceURI:    "https://github.com/owner/repo/auth.md",
			LastModified: time.Now().UTC(),
			FileType:     ".md",
			Checksum:     "csv001",
			Embedding:    emb.hashToVector("Git repo auth docs"),
		},
		{
			ID:           "hq_csv_002",
			Content:      "Filesystem auth docs",
			SourceType:   "filesystem",
			SourceName:   "local-docs",
			SourcePath:   "auth.md",
			SourceURI:    "file://auth.md",
			LastModified: time.Now().UTC(),
			FileType:     ".md",
			Checksum:     "csv002",
			Embedding:    emb.hashToVector("Filesystem auth docs"),
		},
	}
	if err := st.UpsertFragments(ctx, frags); err != nil {
		t.Fatalf("UpsertFragments: %v", err)
	}

	engine := query.NewEngine(st, emb, nil, 20)
	mcpSrv := NewMCPServer(engine, st, nil)

	// Filter by source_types=git — should only return git fragments.
	result, err := mcpSrv.handleQuery(ctx, newCallToolRequest(map[string]interface{}{
		"query":        "auth docs",
		"raw":          true,
		"source_types": "git",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, result))
	}

	var raw model.RawResult
	if err := json.Unmarshal([]byte(resultText(t, result)), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, f := range raw.Fragments {
		if f.SourceType != "git" {
			t.Errorf("expected only git fragments, got source_type=%s", f.SourceType)
		}
	}

	// Filter by sources=local-docs.
	result2, err := mcpSrv.handleQuery(ctx, newCallToolRequest(map[string]interface{}{
		"query":   "auth docs",
		"raw":     true,
		"sources": "local-docs",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, result2))
	}

	var raw2 model.RawResult
	if err := json.Unmarshal([]byte(resultText(t, result2)), &raw2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, f := range raw2.Fragments {
		if f.SourceName != "local-docs" {
			t.Errorf("expected only local-docs fragments, got source_name=%s", f.SourceName)
		}
	}
}

// ---------- handleListSources ----------

func TestMCPHandleListSourcesEmpty(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}
	engine := query.NewEngine(st, emb, nil, 20)
	mcpSrv := NewMCPServer(engine, st, nil)

	ctx := context.Background()

	result, err := mcpSrv.handleListSources(ctx, newCallToolRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, result))
	}

	text := resultText(t, result)
	var sources []json.RawMessage
	if err := json.Unmarshal([]byte(text), &sources); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected empty array, got %d sources", len(sources))
	}
}

func TestMCPHandleListSourcesWithData(t *testing.T) {
	st := newTestStore(t)
	emb := &mockEmbedder{dim: testEmbeddingDim}

	ctx := context.Background()

	// Register sources.
	if err := st.RegisterSource(ctx, model.Source{
		SourceType:  "git",
		SourceName:  "owner/repo",
		Description: "Main application repository",
		Config:      map[string]string{"mode": "local"},
		LastIngest:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}
	if err := st.RegisterSource(ctx, model.Source{
		SourceType: "filesystem",
		SourceName: "docs",
		Config:     map[string]string{"mode": "local"},
		LastIngest: time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}

	// Insert fragments for the git source to get a non-zero count.
	frags := []model.SourceFragment{
		{
			ID:           "ls_001",
			Content:      "Fragment one",
			SourceType:   "git",
			SourceName:   "owner/repo",
			SourcePath:   "file1.go",
			LastModified: time.Now().UTC(),
			Checksum:     "ls001",
			Embedding:    emb.hashToVector("Fragment one"),
		},
		{
			ID:           "ls_002",
			Content:      "Fragment two",
			SourceType:   "git",
			SourceName:   "owner/repo",
			SourcePath:   "file2.go",
			LastModified: time.Now().UTC(),
			Checksum:     "ls002",
			Embedding:    emb.hashToVector("Fragment two"),
		},
	}
	if err := st.UpsertFragments(ctx, frags); err != nil {
		t.Fatalf("UpsertFragments: %v", err)
	}

	engine := query.NewEngine(st, emb, nil, 20)
	mcpSrv := NewMCPServer(engine, st, nil)

	result, err := mcpSrv.handleListSources(ctx, newCallToolRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, result))
	}

	text := resultText(t, result)

	type sourceInfo struct {
		SourceType    string `json:"source_type"`
		SourceName    string `json:"source_name"`
		Description   string `json:"description,omitempty"`
		FragmentCount int    `json:"fragment_count"`
		LastIngest    string `json:"last_ingest,omitempty"`
	}

	var sources []sourceInfo
	if err := json.Unmarshal([]byte(text), &sources); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}

	// Build a lookup by source_type/source_name.
	byKey := map[string]sourceInfo{}
	for _, s := range sources {
		byKey[s.SourceType+"/"+s.SourceName] = s
	}

	gitSrc, ok := byKey["git/owner/repo"]
	if !ok {
		t.Fatal("missing git/owner/repo source")
	}
	if gitSrc.Description != "Main application repository" {
		t.Errorf("expected description 'Main application repository', got %q", gitSrc.Description)
	}
	if gitSrc.FragmentCount != 2 {
		t.Errorf("expected fragment_count=2, got %d", gitSrc.FragmentCount)
	}
	if gitSrc.LastIngest == "" {
		t.Error("expected last_ingest to be set")
	}

	fsSrc, ok := byKey["filesystem/docs"]
	if !ok {
		t.Fatal("missing filesystem/docs source")
	}
	if fsSrc.FragmentCount != 0 {
		t.Errorf("expected fragment_count=0 for docs, got %d", fsSrc.FragmentCount)
	}
	if fsSrc.Description != "" {
		t.Errorf("expected empty description for docs, got %q", fsSrc.Description)
	}
}
