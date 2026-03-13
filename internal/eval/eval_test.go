package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func TestRecallAtK(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
		retrieved []string
		k        int
		want     float64
	}{
		{
			name:     "all found in top-5",
			expected: []string{"config.go", "README.md"},
			retrieved: []string{"eval/corpus/config.go", "eval/corpus/README.md", "eval/corpus/api.go", "x", "y"},
			k:        5,
			want:     1.0,
		},
		{
			name:     "one of two found",
			expected: []string{"config.go", "README.md"},
			retrieved: []string{"eval/corpus/config.go", "eval/corpus/api.go", "x"},
			k:        5,
			want:     0.5,
		},
		{
			name:     "none found",
			expected: []string{"config.go"},
			retrieved: []string{"eval/corpus/api.go", "eval/corpus/README.md"},
			k:        5,
			want:     0.0,
		},
		{
			name:     "found beyond K",
			expected: []string{"config.go"},
			retrieved: []string{"a", "b", "c", "d", "e", "eval/corpus/config.go"},
			k:        5,
			want:     0.0,
		},
		{
			name:     "found at exactly K",
			expected: []string{"config.go"},
			retrieved: []string{"a", "b", "c", "d", "eval/corpus/config.go"},
			k:        5,
			want:     1.0,
		},
		{
			name:     "unanswerable - no expected sources",
			expected: []string{},
			retrieved: []string{"a", "b"},
			k:        5,
			want:     1.0,
		},
		{
			name:     "empty retrieved",
			expected: []string{"config.go"},
			retrieved: []string{},
			k:        5,
			want:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RecallAtK(tt.expected, tt.retrieved, tt.k)
			if got != tt.want {
				t.Errorf("RecallAtK() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPrecisionAtK(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
		retrieved []string
		k        int
		want     float64
	}{
		{
			name:     "all relevant",
			expected: []string{"config.go", "README.md"},
			retrieved: []string{"eval/corpus/config.go", "eval/corpus/README.md"},
			k:        5,
			want:     1.0,
		},
		{
			name:     "half relevant",
			expected: []string{"config.go"},
			retrieved: []string{"eval/corpus/config.go", "eval/corpus/api.go", "x", "y"},
			k:        4,
			want:     0.25,
		},
		{
			name:     "none relevant",
			expected: []string{"config.go"},
			retrieved: []string{"eval/corpus/api.go", "eval/corpus/README.md"},
			k:        5,
			want:     0.0,
		},
		{
			name:     "unanswerable with results",
			expected: []string{},
			retrieved: []string{"a", "b"},
			k:        5,
			want:     0.0,
		},
		{
			name:     "unanswerable with no results",
			expected: []string{},
			retrieved: []string{},
			k:        5,
			want:     1.0,
		},
		{
			name:     "empty retrieved with expected",
			expected: []string{"config.go"},
			retrieved: []string{},
			k:        5,
			want:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrecisionAtK(tt.expected, tt.retrieved, tt.k)
			if got != tt.want {
				t.Errorf("PrecisionAtK() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMRRScore(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
		retrieved []string
		want     float64
	}{
		{
			name:     "first result relevant",
			expected: []string{"config.go"},
			retrieved: []string{"eval/corpus/config.go", "x", "y"},
			want:     1.0,
		},
		{
			name:     "second result relevant",
			expected: []string{"config.go"},
			retrieved: []string{"eval/corpus/api.go", "eval/corpus/config.go", "y"},
			want:     0.5,
		},
		{
			name:     "third result relevant",
			expected: []string{"config.go"},
			retrieved: []string{"a", "b", "eval/corpus/config.go"},
			want:     1.0 / 3.0,
		},
		{
			name:     "not found",
			expected: []string{"config.go"},
			retrieved: []string{"a", "b", "c"},
			want:     0.0,
		},
		{
			name:     "unanswerable",
			expected: []string{},
			retrieved: []string{"a", "b"},
			want:     1.0,
		},
		{
			name:     "multiple expected - first match counts",
			expected: []string{"config.go", "README.md"},
			retrieved: []string{"a", "eval/corpus/README.md", "eval/corpus/config.go"},
			want:     0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MRRScore(tt.expected, tt.retrieved)
			if got != tt.want {
				t.Errorf("MRRScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSourceMatches(t *testing.T) {
	tests := []struct {
		sourcePath string
		expected   string
		want       bool
	}{
		{"eval/corpus/config.go", "config.go", true},
		{"eval/corpus/README.md", "README.md", true},
		{"config.go", "config.go", true},
		{"eval/corpus/api.go", "config.go", false},
		{"some/path/config.go.bak", "config.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.sourcePath+"_"+tt.expected, func(t *testing.T) {
			got := sourceMatches(tt.sourcePath, tt.expected)
			if got != tt.want {
				t.Errorf("sourceMatches(%q, %q) = %v, want %v", tt.sourcePath, tt.expected, got, tt.want)
			}
		})
	}
}

func TestLoadTestSet(t *testing.T) {
	// Create a temporary test set file.
	tmpDir := t.TempDir()
	testSetPath := filepath.Join(tmpDir, "testset.json")

	cases := []TestCase{
		{
			ID:              "q01",
			Query:        "What database is used?",
			ExpectedSources: []string{"config.go"},
			ReferenceAnswer: "PostgreSQL",
			Category:        "direct_extraction",
		},
		{
			ID:              "q02",
			Query:        "Does it support GraphQL?",
			ExpectedSources: []string{},
			ReferenceAnswer: "No information available.",
			Category:        "abstention",
		},
	}

	data, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testSetPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadTestSet(testSetPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(loaded))
	}
	if loaded[0].ID != "q01" {
		t.Errorf("expected ID q01, got %s", loaded[0].ID)
	}
	if loaded[0].Category != "direct_extraction" {
		t.Errorf("expected category direct_extraction, got %s", loaded[0].Category)
	}
	if loaded[1].Category != "abstention" {
		t.Errorf("expected category abstention, got %s", loaded[1].Category)
	}
}

func TestLoadTestSet_FileNotFound(t *testing.T) {
	_, err := LoadTestSet("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestComputeHits(t *testing.T) {
	hits := computeHits(
		[]string{"config.go", "README.md", "api.go"},
		[]string{"eval/corpus/config.go", "eval/corpus/api.go", "other.go"},
	)

	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d: %v", len(hits), hits)
	}

	hitSet := make(map[string]bool)
	for _, h := range hits {
		hitSet[h] = true
	}
	if !hitSet["config.go"] || !hitSet["api.go"] {
		t.Errorf("unexpected hits: %v", hits)
	}
}

func TestComputeOverallMetrics(t *testing.T) {
	summary := &Summary{
		Results: []Result{
			{Category: "direct_extraction", RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0, PrecisionAt5: 0.5, PrecisionAt10: 0.5, PrecisionAt20: 0.5, MRR: 1.0, HitAt1: 1.0, HitAt3: 1.0, HitAt5: 1.0},
			{Category: "direct_extraction", RecallAt5: 0.5, RecallAt10: 1.0, RecallAt20: 1.0, PrecisionAt5: 0.3, PrecisionAt10: 0.3, PrecisionAt20: 0.3, MRR: 0.5, HitAt1: 0.0, HitAt3: 1.0, HitAt5: 1.0},
			{Category: "abstention", RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0, PrecisionAt5: 0.0, PrecisionAt10: 0.0, PrecisionAt20: 0.0, MRR: 1.0, HitAt1: 1.0, HitAt3: 1.0, HitAt5: 1.0},
		},
	}

	computeOverallMetrics(summary)

	// Overall Recall@5 = (1.0 + 0.5 + 1.0) / 3 = 0.833...
	wantR5 := 2.5 / 3.0
	if diff := summary.RecallAt5 - wantR5; diff > 0.001 || diff < -0.001 {
		t.Errorf("RecallAt5 = %.4f, want %.4f", summary.RecallAt5, wantR5)
	}

	// Check HitAt fields.
	wantH1 := 2.0 / 3.0
	if diff := summary.HitAt1 - wantH1; diff > 0.001 || diff < -0.001 {
		t.Errorf("HitAt1 = %.4f, want %.4f", summary.HitAt1, wantH1)
	}
	if summary.HitAt3 != 1.0 {
		t.Errorf("HitAt3 = %.4f, want 1.0", summary.HitAt3)
	}
	if summary.HitAt5 != 1.0 {
		t.Errorf("HitAt5 = %.4f, want 1.0", summary.HitAt5)
	}

	// Check timestamp is set.
	if summary.Timestamp == "" {
		t.Error("expected Timestamp to be set")
	}

	// Check category breakdowns exist.
	if len(summary.CategoryBreakdowns) != 2 {
		t.Fatalf("expected 2 category breakdowns, got %d", len(summary.CategoryBreakdowns))
	}

	// direct_extraction category: R@5 = (1.0 + 0.5) / 2 = 0.75
	for _, cb := range summary.CategoryBreakdowns {
		if cb.Category == "direct_extraction" {
			if cb.Count != 2 {
				t.Errorf("direct_extraction count = %d, want 2", cb.Count)
			}
			if cb.RecallAt5 != 0.75 {
				t.Errorf("direct_extraction R@5 = %.4f, want 0.75", cb.RecallAt5)
			}
			if cb.HitAt1 != 0.5 {
				t.Errorf("direct_extraction HitAt1 = %.4f, want 0.5", cb.HitAt1)
			}
			if cb.HitAt3 != 1.0 {
				t.Errorf("direct_extraction HitAt3 = %.4f, want 1.0", cb.HitAt3)
			}
		}
	}
}

func TestFormatSummaryTable(t *testing.T) {
	summary := &Summary{
		Results: []Result{
			{
				ID: "q01", Query: "Test question?", Category: "direct_extraction",
				RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0,
				PrecisionAt5: 0.5, PrecisionAt10: 0.3, PrecisionAt20: 0.2,
				MRR: 1.0,
				HitAt1: 1.0, HitAt3: 1.0, HitAt5: 1.0,
			},
		},
		RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0,
		PrecisionAt5: 0.5, PrecisionAt10: 0.3, PrecisionAt20: 0.2,
		MRR: 1.0,
		HitAt1: 1.0, HitAt3: 1.0, HitAt5: 1.0,
		CategoryBreakdowns: []CategoryBreakdown{
			{Category: "direct_extraction", Count: 1, RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0,
				PrecisionAt5: 0.5, PrecisionAt10: 0.3, PrecisionAt20: 0.2, MRR: 1.0,
				HitAt1: 1.0, HitAt3: 1.0, HitAt5: 1.0},
		},
		Chunking: &ChunkingStats{
			TotalFragments:  10,
			FragmentsPerFile: map[string]int{"config.go": 3, "README.md": 7},
			MeanTokenLen:    150.5,
			MedianTokenLen:  140.0,
			P95TokenLen:     280.0,
		},
	}

	table := FormatSummaryTable(summary)

	// Verify the table contains key elements.
	if table == "" {
		t.Fatal("empty table")
	}
	for _, want := range []string{"q01", "direct_extraction", "Hit@1", "Chunking", "Total fragments: 10", "config.go"} {
		if !contains(table, want) {
			t.Errorf("table missing %q", want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestChunkingStatsEmpty(t *testing.T) {
	// Test that ChunkingStats handles zero fragments gracefully.
	stats := &ChunkingStats{
		TotalFragments:  0,
		FragmentsPerFile: map[string]int{},
	}
	if stats.TotalFragments != 0 {
		t.Errorf("expected 0 total fragments, got %d", stats.TotalFragments)
	}
}

// TestRunnerWithMockStore tests the Runner with a mock store and embedder.
func TestRunnerWithMockStore(t *testing.T) {
	store := &mockStore{
		fragments: []mockFragment{
			{id: "f1", sourcePath: "eval/corpus/config.go", fileType: ".go", contentDate: time.Now().Add(-24 * time.Hour), sourceName: "test-source"},
			{id: "f2", sourcePath: "eval/corpus/README.md", fileType: ".md", contentDate: time.Now().Add(-24 * time.Hour), sourceName: "test-source"},
			{id: "f3", sourcePath: "eval/corpus/api.go", fileType: ".go", contentDate: time.Now().Add(-24 * time.Hour), sourceName: "test-source"},
		},
	}
	embedder := &mockEmbedder{dim: 4}

	runner := NewRunner(store, embedder)
	cases := []TestCase{
		{
			ID:              "q01",
			Query:        "What database?",
			ExpectedSources: []string{"config.go"},
			Category:        "direct_extraction",
		},
	}

	summary, err := runner.Run(context.Background(), cases, 5)
	if err != nil {
		t.Fatal(err)
	}

	if len(summary.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(summary.Results))
	}

	r := summary.Results[0]
	if r.RecallAt5 != 1.0 {
		t.Errorf("RecallAt5 = %.2f, want 1.0", r.RecallAt5)
	}
	if r.HitAt1 != 1.0 {
		t.Errorf("HitAt1 = %.2f, want 1.0", r.HitAt1)
	}
	if r.HitAt3 != 1.0 {
		t.Errorf("HitAt3 = %.2f, want 1.0", r.HitAt3)
	}
	if r.HitAt5 != 1.0 {
		t.Errorf("HitAt5 = %.2f, want 1.0", r.HitAt5)
	}
	if len(r.Hits) != 1 || r.Hits[0] != "config.go" {
		t.Errorf("unexpected hits: %v", r.Hits)
	}
}

func TestHitAtK(t *testing.T) {
	tests := []struct {
		name      string
		expected  []string
		retrieved []string
		k         int
		want      float64
	}{
		{
			name:      "hit in top-1",
			expected:  []string{"config.go"},
			retrieved: []string{"eval/corpus/config.go", "x", "y"},
			k:         1,
			want:      1.0,
		},
		{
			name:      "hit in top-3 but not top-1",
			expected:  []string{"config.go"},
			retrieved: []string{"x", "y", "eval/corpus/config.go"},
			k:         3,
			want:      1.0,
		},
		{
			name:      "no hit in top-3",
			expected:  []string{"config.go"},
			retrieved: []string{"x", "y", "z", "eval/corpus/config.go"},
			k:         3,
			want:      0.0,
		},
		{
			name:      "multiple expected - any hit counts",
			expected:  []string{"config.go", "README.md"},
			retrieved: []string{"x", "eval/corpus/README.md", "y"},
			k:         3,
			want:      1.0,
		},
		{
			name:      "abstention - empty expected",
			expected:  []string{},
			retrieved: []string{"x", "y"},
			k:         1,
			want:      1.0,
		},
		{
			name:      "no results",
			expected:  []string{"config.go"},
			retrieved: []string{},
			k:         5,
			want:      0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HitAtK(tt.expected, tt.retrieved, tt.k)
			if got != tt.want {
				t.Errorf("HitAtK() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSaveAndLoadResults(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "results.json")

	summary := &Summary{
		Results: []Result{
			{ID: "q01", Query: "test", Category: "direct_extraction", HitAt1: 1.0, HitAt3: 1.0, HitAt5: 1.0, MRR: 1.0},
		},
		HitAt1:    1.0,
		HitAt3:    1.0,
		HitAt5:    1.0,
		MRR:       1.0,
		Timestamp: "2026-03-12T00:00:00Z",
	}

	if err := SaveResults(summary, path); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadResults(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.HitAt1 != 1.0 {
		t.Errorf("HitAt1 = %v, want 1.0", loaded.HitAt1)
	}
	if loaded.Timestamp != "2026-03-12T00:00:00Z" {
		t.Errorf("Timestamp = %v, want 2026-03-12T00:00:00Z", loaded.Timestamp)
	}
	if len(loaded.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(loaded.Results))
	}
	if loaded.Results[0].ID != "q01" {
		t.Errorf("result ID = %v, want q01", loaded.Results[0].ID)
	}
}

func TestContentMatch(t *testing.T) {
	store := &mockStore{
		fragments: []mockFragment{
			{id: "f1", sourcePath: "eval/corpus/config.go", content: "The database is PostgreSQL running on port 5432", fileType: ".go", contentDate: time.Now().Add(-24 * time.Hour), sourceName: "test-source"},
			{id: "f2", sourcePath: "eval/corpus/README.md", content: "This service uses Redis for caching", fileType: ".md", contentDate: time.Now().Add(-24 * time.Hour), sourceName: "test-source"},
			{id: "f3", sourcePath: "eval/corpus/api.go", content: "API endpoints defined here", fileType: ".go", contentDate: time.Now().Add(-24 * time.Hour), sourceName: "test-source"},
		},
	}
	embedder := &mockEmbedder{dim: 4}
	runner := NewRunner(store, embedder)

	cases := []TestCase{
		{
			ID:                      "q01",
			Query:                   "What database?",
			ExpectedSources:         []string{"config.go"},
			Category:                "direct_extraction",
			ExpectedContentContains: []string{"PostgreSQL"},
		},
		{
			ID:                      "q02",
			Query:                   "What about MongoDB?",
			ExpectedSources:         []string{"config.go"},
			Category:                "direct_extraction",
			ExpectedContentContains: []string{"MongoDB"},
		},
	}

	summary, err := runner.Run(context.Background(), cases, 5)
	if err != nil {
		t.Fatal(err)
	}

	if !summary.Results[0].ContentMatch {
		t.Error("expected ContentMatch=true for PostgreSQL query")
	}
	if summary.Results[1].ContentMatch {
		t.Error("expected ContentMatch=false for MongoDB query")
	}
}

func TestConfidenceMetrics(t *testing.T) {
	s := &mockStore{
		fragments: []mockFragment{
			{id: "f1", sourcePath: "eval/corpus/config.go", content: "config data", fileType: ".go", contentDate: time.Now().Add(-24 * time.Hour), sourceName: "source-a"},
			{id: "f2", sourcePath: "eval/corpus/README.md", content: "readme content", fileType: ".md", contentDate: time.Now().Add(-2 * time.Hour), sourceName: "source-b"},
			{id: "f3", sourcePath: "eval/corpus/api.go", content: "api code", fileType: ".go", contentDate: time.Now().Add(-48 * time.Hour), sourceName: "source-a"},
		},
	}
	embedder := &mockEmbedder{dim: 4}
	runner := NewRunner(s, embedder)

	cases := []TestCase{
		{
			ID:              "q01",
			Query:           "What config?",
			ExpectedSources: []string{"config.go"},
			Category:        "direct_extraction",
		},
	}

	summary, err := runner.Run(context.Background(), cases, 5)
	if err != nil {
		t.Fatal(err)
	}

	r := summary.Results[0]

	// Code files (.go) get freshness of 0.95; .md with recent date gets high freshness.
	// With 2 .go files (0.95 each) and 1 .md file (~1.0 for 2h old), avg should be high.
	if r.AvgFreshness < 0.90 {
		t.Errorf("AvgFreshness = %.2f, want >= 0.90", r.AvgFreshness)
	}

	// AvgConfidence should be populated (> 0).
	if r.AvgConfidence <= 0 {
		t.Errorf("AvgConfidence = %.2f, want > 0", r.AvgConfidence)
	}

	// Summary-level confidence should also be populated.
	if summary.AvgFreshness < 0.90 {
		t.Errorf("Summary AvgFreshness = %.2f, want >= 0.90", summary.AvgFreshness)
	}
	if summary.AvgConfidence <= 0 {
		t.Errorf("Summary AvgConfidence = %.2f, want > 0", summary.AvgConfidence)
	}

	// Category breakdown should have confidence.
	if len(summary.CategoryBreakdowns) == 0 {
		t.Fatal("expected at least one category breakdown")
	}
	cb := summary.CategoryBreakdowns[0]
	if cb.AvgFreshness < 0.90 {
		t.Errorf("Category AvgFreshness = %.2f, want >= 0.90", cb.AvgFreshness)
	}
	if cb.AvgConfidence <= 0 {
		t.Errorf("Category AvgConfidence = %.2f, want > 0", cb.AvgConfidence)
	}
}

// mockQueryEngine implements QueryEngine for testing.
type mockQueryEngine struct {
	answer string
	err    error
}

func (m *mockQueryEngine) QueryRaw(ctx context.Context, req model.QueryRequest) (*model.RawResult, error) {
	return &model.RawResult{}, nil
}

func (m *mockQueryEngine) Query(ctx context.Context, req model.QueryRequest, onText func(string)) (*model.Answer, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &model.Answer{Content: m.answer}, nil
}

func TestRunnerWithQueryEngine(t *testing.T) {
	s := &mockStore{
		fragments: []mockFragment{
			{id: "f1", sourcePath: "eval/corpus/config.go", content: "PostgreSQL", fileType: ".go", contentDate: time.Now().Add(-24 * time.Hour), sourceName: "test-source"},
		},
	}
	embedder := &mockEmbedder{dim: 4}
	runner := NewRunner(s, embedder)
	runner.SetQueryEngine(&mockQueryEngine{answer: "The database is PostgreSQL."})

	cases := []TestCase{
		{
			ID:              "q01",
			Query:           "What database?",
			ExpectedSources: []string{"config.go"},
			Category:        "direct_extraction",
		},
	}

	// With QueryEngine set, retrieval should go through QueryRaw (hybrid pipeline).
	summary, err := runner.Run(context.Background(), cases, 5)
	if err != nil {
		t.Fatal(err)
	}

	// QueryRaw returns empty fragments, so no hits expected.
	if len(summary.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(summary.Results))
	}
}


func TestFormatSummaryTableWithDelta(t *testing.T) {
	current := &Summary{
		Results: []Result{
			{ID: "q01", Query: "Test?", Category: "direct_extraction", HitAt1: 1.0, HitAt3: 1.0, HitAt5: 1.0, MRR: 1.0, RecallAt5: 1.0},
		},
		HitAt1: 1.0, HitAt3: 1.0, HitAt5: 1.0, MRR: 1.0,
		CategoryBreakdowns: []CategoryBreakdown{
			{Category: "direct_extraction", Count: 1, HitAt1: 1.0, HitAt3: 1.0, HitAt5: 1.0, MRR: 1.0},
		},
	}

	previous := &Summary{
		Results: []Result{
			{ID: "q01", Query: "Test?", Category: "direct_extraction", HitAt1: 0.5, HitAt3: 0.8, HitAt5: 1.0, MRR: 0.8, RecallAt5: 0.5},
		},
		HitAt1: 0.5, HitAt3: 0.8, HitAt5: 1.0, MRR: 0.8,
		CategoryBreakdowns: []CategoryBreakdown{
			{Category: "direct_extraction", Count: 1, HitAt1: 0.5, HitAt3: 0.8, HitAt5: 1.0, MRR: 0.8},
		},
	}

	table := FormatSummaryTableWithDelta(current, previous)
	if !strings.Contains(table, "(+0.50)") {
		t.Error("expected delta (+0.50) in table output")
	}
	if !strings.Contains(table, "(=)") {
		t.Error("expected (=) delta in table output")
	}
}
