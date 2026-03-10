package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
			Category:        "factual",
		},
		{
			ID:              "q02",
			Query:        "Does it support GraphQL?",
			ExpectedSources: []string{},
			ReferenceAnswer: "No information available.",
			Category:        "unanswerable",
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
	if loaded[0].Category != "factual" {
		t.Errorf("expected category factual, got %s", loaded[0].Category)
	}
	if loaded[1].Category != "unanswerable" {
		t.Errorf("expected category unanswerable, got %s", loaded[1].Category)
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
			{Category: "factual", RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0, PrecisionAt5: 0.5, PrecisionAt10: 0.5, PrecisionAt20: 0.5, MRR: 1.0},
			{Category: "factual", RecallAt5: 0.5, RecallAt10: 1.0, RecallAt20: 1.0, PrecisionAt5: 0.3, PrecisionAt10: 0.3, PrecisionAt20: 0.3, MRR: 0.5},
			{Category: "unanswerable", RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0, PrecisionAt5: 0.0, PrecisionAt10: 0.0, PrecisionAt20: 0.0, MRR: 1.0},
		},
	}

	computeOverallMetrics(summary)

	// Overall Recall@5 = (1.0 + 0.5 + 1.0) / 3 = 0.833...
	wantR5 := 2.5 / 3.0
	if diff := summary.RecallAt5 - wantR5; diff > 0.001 || diff < -0.001 {
		t.Errorf("RecallAt5 = %.4f, want %.4f", summary.RecallAt5, wantR5)
	}

	// Check category breakdowns exist.
	if len(summary.CategoryBreakdowns) != 2 {
		t.Fatalf("expected 2 category breakdowns, got %d", len(summary.CategoryBreakdowns))
	}

	// Factual category: R@5 = (1.0 + 0.5) / 2 = 0.75
	for _, cb := range summary.CategoryBreakdowns {
		if cb.Category == "factual" {
			if cb.Count != 2 {
				t.Errorf("factual count = %d, want 2", cb.Count)
			}
			if cb.RecallAt5 != 0.75 {
				t.Errorf("factual R@5 = %.4f, want 0.75", cb.RecallAt5)
			}
		}
	}
}

func TestFormatSummaryTable(t *testing.T) {
	summary := &Summary{
		Results: []Result{
			{
				ID: "q01", Query: "Test question?", Category: "factual",
				RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0,
				PrecisionAt5: 0.5, PrecisionAt10: 0.3, PrecisionAt20: 0.2,
				MRR: 1.0,
			},
		},
		RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0,
		PrecisionAt5: 0.5, PrecisionAt10: 0.3, PrecisionAt20: 0.2,
		MRR: 1.0,
		CategoryBreakdowns: []CategoryBreakdown{
			{Category: "factual", Count: 1, RecallAt5: 1.0, RecallAt10: 1.0, RecallAt20: 1.0,
				PrecisionAt5: 0.5, PrecisionAt10: 0.3, PrecisionAt20: 0.2, MRR: 1.0},
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
	for _, want := range []string{"q01", "factual", "OVERALL", "Chunking", "Total fragments: 10", "config.go"} {
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
			{id: "f1", sourcePath: "eval/corpus/config.go"},
			{id: "f2", sourcePath: "eval/corpus/README.md"},
			{id: "f3", sourcePath: "eval/corpus/api.go"},
		},
	}
	embedder := &mockEmbedder{dim: 4}

	runner := NewRunner(store, embedder)
	cases := []TestCase{
		{
			ID:              "q01",
			Query:        "What database?",
			ExpectedSources: []string{"config.go"},
			Category:        "factual",
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
	if len(r.Hits) != 1 || r.Hits[0] != "config.go" {
		t.Errorf("unexpected hits: %v", r.Hits)
	}
}
