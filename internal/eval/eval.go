// Package eval implements a retrieval evaluation framework for Knowledge Broker.
//
// It loads a test set of queries with expected source files, runs each
// query through embedding + vector search (no LLM), and computes
// retrieval quality metrics: Recall@K, Precision@K, and MRR.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// TestCase represents a single evaluation query from the test set JSON.
type TestCase struct {
	ID              string   `json:"id"`
	Query           string   `json:"query"`
	ExpectedSources []string `json:"expected_sources"`
	ReferenceAnswer string   `json:"reference_answer"`
	Category        string   `json:"category"` // factual, cross-file, contradiction, unanswerable
}

// Result holds the evaluation outcome for a single test case.
type Result struct {
	ID              string   `json:"id"`
	Query           string   `json:"query"`
	Category        string   `json:"category"`
	ExpectedSources []string `json:"expected_sources"`
	RetrievedPaths  []string `json:"retrieved_paths"`  // source_path of top-K fragments
	Hits            []string `json:"hits"`              // expected sources found in top-K
	RecallAt5       float64  `json:"recall_at_5"`
	RecallAt10      float64  `json:"recall_at_10"`
	RecallAt20      float64  `json:"recall_at_20"`
	PrecisionAt5    float64  `json:"precision_at_5"`
	PrecisionAt10   float64  `json:"precision_at_10"`
	PrecisionAt20   float64  `json:"precision_at_20"`
	MRR             float64  `json:"mrr"` // 1/rank of first relevant fragment
}

// ChunkingStats holds statistics about the chunking of the eval corpus.
type ChunkingStats struct {
	TotalFragments  int            `json:"total_fragments"`
	FragmentsPerFile map[string]int `json:"fragments_per_file"`
	MeanTokenLen    float64        `json:"mean_token_len"`
	MedianTokenLen  float64        `json:"median_token_len"`
	P95TokenLen     float64        `json:"p95_token_len"`
}

// CategoryBreakdown holds aggregate metrics for a single category.
type CategoryBreakdown struct {
	Category    string  `json:"category"`
	Count       int     `json:"count"`
	RecallAt5   float64 `json:"recall_at_5"`
	RecallAt10  float64 `json:"recall_at_10"`
	RecallAt20  float64 `json:"recall_at_20"`
	PrecisionAt5  float64 `json:"precision_at_5"`
	PrecisionAt10 float64 `json:"precision_at_10"`
	PrecisionAt20 float64 `json:"precision_at_20"`
	MRR         float64 `json:"mrr"`
}

// Summary holds the overall evaluation results.
type Summary struct {
	Results           []Result             `json:"results"`
	RecallAt5         float64              `json:"recall_at_5"`
	RecallAt10        float64              `json:"recall_at_10"`
	RecallAt20        float64              `json:"recall_at_20"`
	PrecisionAt5      float64              `json:"precision_at_5"`
	PrecisionAt10     float64              `json:"precision_at_10"`
	PrecisionAt20     float64              `json:"precision_at_20"`
	MRR               float64              `json:"mrr"`
	CategoryBreakdowns []CategoryBreakdown `json:"category_breakdowns"`
	Chunking          *ChunkingStats       `json:"chunking,omitempty"`
}

// Runner executes evaluation against the store using the embedder.
type Runner struct {
	store    store.Store
	embedder embedding.Embedder
}

// NewRunner creates a new evaluation runner.
func NewRunner(s store.Store, e embedding.Embedder) *Runner {
	return &Runner{store: s, embedder: e}
}

// LoadTestSet reads and parses a test set JSON file.
func LoadTestSet(path string) ([]TestCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read test set: %w", err)
	}
	var cases []TestCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("parse test set: %w", err)
	}
	return cases, nil
}

// Run executes the evaluation for all test cases and returns a summary.
// The limit parameter controls how many fragments to retrieve per query (max K).
func (r *Runner) Run(ctx context.Context, cases []TestCase, limit int) (*Summary, error) {
	if limit <= 0 {
		limit = 20
	}

	summary := &Summary{
		Results: make([]Result, 0, len(cases)),
	}

	for _, tc := range cases {
		result, err := r.evalOne(ctx, tc, limit)
		if err != nil {
			return nil, fmt.Errorf("eval %s: %w", tc.ID, err)
		}
		summary.Results = append(summary.Results, *result)
	}

	// Compute overall averages.
	computeOverallMetrics(summary)

	return summary, nil
}

func (r *Runner) evalOne(ctx context.Context, tc TestCase, limit int) (*Result, error) {
	// Embed the query.
	queryEmb, err := r.embedder.Embed(ctx, tc.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Search for relevant fragments.
	fragments, err := r.store.SearchByVector(ctx, queryEmb, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// Collect retrieved source paths.
	retrievedPaths := make([]string, len(fragments))
	for i, f := range fragments {
		retrievedPaths[i] = f.SourcePath
	}

	result := &Result{
		ID:              tc.ID,
		Query:           tc.Query,
		Category:        tc.Category,
		ExpectedSources: tc.ExpectedSources,
		RetrievedPaths:  retrievedPaths,
	}

	// Compute hits: which expected sources appear in retrieved paths.
	result.Hits = computeHits(tc.ExpectedSources, retrievedPaths)

	// Compute metrics at K=5, 10, 20.
	result.RecallAt5 = RecallAtK(tc.ExpectedSources, retrievedPaths, 5)
	result.RecallAt10 = RecallAtK(tc.ExpectedSources, retrievedPaths, 10)
	result.RecallAt20 = RecallAtK(tc.ExpectedSources, retrievedPaths, 20)
	result.PrecisionAt5 = PrecisionAtK(tc.ExpectedSources, retrievedPaths, 5)
	result.PrecisionAt10 = PrecisionAtK(tc.ExpectedSources, retrievedPaths, 10)
	result.PrecisionAt20 = PrecisionAtK(tc.ExpectedSources, retrievedPaths, 20)
	result.MRR = MRRScore(tc.ExpectedSources, retrievedPaths)

	return result, nil
}

// computeHits returns the subset of expectedSources that appear in retrievedPaths.
func computeHits(expectedSources, retrievedPaths []string) []string {
	var hits []string
	seen := make(map[string]bool)
	for _, expected := range expectedSources {
		if seen[expected] {
			continue
		}
		for _, path := range retrievedPaths {
			if sourceMatches(path, expected) {
				hits = append(hits, expected)
				seen[expected] = true
				break
			}
		}
	}
	return hits
}

// sourceMatches checks if a retrieved source_path contains the expected filename.
func sourceMatches(sourcePath, expected string) bool {
	return strings.HasSuffix(sourcePath, "/"+expected) || strings.HasSuffix(sourcePath, "\\"+expected) || sourcePath == expected
}

// RecallAtK computes the fraction of expected source files found in the top-K results.
// For unanswerable questions (empty expected sources), returns 1.0 by convention.
func RecallAtK(expectedSources, retrievedPaths []string, k int) float64 {
	if len(expectedSources) == 0 {
		return 1.0 // unanswerable: no sources expected, recall is perfect
	}

	topK := retrievedPaths
	if len(topK) > k {
		topK = topK[:k]
	}

	found := 0
	for _, expected := range expectedSources {
		for _, path := range topK {
			if sourceMatches(path, expected) {
				found++
				break
			}
		}
	}

	return float64(found) / float64(len(expectedSources))
}

// PrecisionAtK computes the fraction of top-K fragments that come from expected source files.
// For unanswerable questions (empty expected sources), returns 1.0 if no results, 0.0 otherwise.
func PrecisionAtK(expectedSources, retrievedPaths []string, k int) float64 {
	topK := retrievedPaths
	if len(topK) > k {
		topK = topK[:k]
	}

	if len(topK) == 0 {
		if len(expectedSources) == 0 {
			return 1.0
		}
		return 0.0
	}

	if len(expectedSources) == 0 {
		return 0.0 // any results for unanswerable = 0 precision
	}

	relevant := 0
	for _, path := range topK {
		for _, expected := range expectedSources {
			if sourceMatches(path, expected) {
				relevant++
				break
			}
		}
	}

	return float64(relevant) / float64(len(topK))
}

// MRRScore computes the Mean Reciprocal Rank: 1/rank of the first relevant fragment.
// For unanswerable questions (empty expected sources), returns 1.0 by convention.
func MRRScore(expectedSources, retrievedPaths []string) float64 {
	if len(expectedSources) == 0 {
		return 1.0
	}

	for i, path := range retrievedPaths {
		for _, expected := range expectedSources {
			if sourceMatches(path, expected) {
				return 1.0 / float64(i+1)
			}
		}
	}

	return 0.0
}

// ComputeChunkingStats computes statistics about fragments in the store.
func ComputeChunkingStats(ctx context.Context, s store.Store) (*ChunkingStats, error) {
	fragments, err := s.ExportFragments(ctx)
	if err != nil {
		return nil, fmt.Errorf("export fragments for stats: %w", err)
	}

	stats := &ChunkingStats{
		TotalFragments:  len(fragments),
		FragmentsPerFile: make(map[string]int),
	}

	if len(fragments) == 0 {
		return stats, nil
	}

	var tokenLengths []int
	for _, f := range fragments {
		stats.FragmentsPerFile[f.SourcePath]++
		// Whitespace-split approximation for token count.
		tokens := len(strings.Fields(f.Content))
		tokenLengths = append(tokenLengths, tokens)
	}

	sort.Ints(tokenLengths)
	n := len(tokenLengths)

	// Mean
	sum := 0
	for _, t := range tokenLengths {
		sum += t
	}
	stats.MeanTokenLen = float64(sum) / float64(n)

	// Median
	if n%2 == 0 {
		stats.MedianTokenLen = float64(tokenLengths[n/2-1]+tokenLengths[n/2]) / 2.0
	} else {
		stats.MedianTokenLen = float64(tokenLengths[n/2])
	}

	// P95
	p95Idx := int(math.Ceil(0.95*float64(n))) - 1
	if p95Idx < 0 {
		p95Idx = 0
	}
	if p95Idx >= n {
		p95Idx = n - 1
	}
	stats.P95TokenLen = float64(tokenLengths[p95Idx])

	return stats, nil
}

// computeOverallMetrics fills in the summary-level averages and category breakdowns.
func computeOverallMetrics(summary *Summary) {
	n := float64(len(summary.Results))
	if n == 0 {
		return
	}

	var totalR5, totalR10, totalR20 float64
	var totalP5, totalP10, totalP20 float64
	var totalMRR float64

	catMap := make(map[string]*CategoryBreakdown)

	for _, r := range summary.Results {
		totalR5 += r.RecallAt5
		totalR10 += r.RecallAt10
		totalR20 += r.RecallAt20
		totalP5 += r.PrecisionAt5
		totalP10 += r.PrecisionAt10
		totalP20 += r.PrecisionAt20
		totalMRR += r.MRR

		cb, ok := catMap[r.Category]
		if !ok {
			cb = &CategoryBreakdown{Category: r.Category}
			catMap[r.Category] = cb
		}
		cb.Count++
		cb.RecallAt5 += r.RecallAt5
		cb.RecallAt10 += r.RecallAt10
		cb.RecallAt20 += r.RecallAt20
		cb.PrecisionAt5 += r.PrecisionAt5
		cb.PrecisionAt10 += r.PrecisionAt10
		cb.PrecisionAt20 += r.PrecisionAt20
		cb.MRR += r.MRR
	}

	summary.RecallAt5 = totalR5 / n
	summary.RecallAt10 = totalR10 / n
	summary.RecallAt20 = totalR20 / n
	summary.PrecisionAt5 = totalP5 / n
	summary.PrecisionAt10 = totalP10 / n
	summary.PrecisionAt20 = totalP20 / n
	summary.MRR = totalMRR / n

	// Build sorted category breakdowns.
	categories := []string{"factual", "cross-file", "contradiction", "unanswerable"}
	for _, cat := range categories {
		cb, ok := catMap[cat]
		if !ok {
			continue
		}
		cn := float64(cb.Count)
		cb.RecallAt5 /= cn
		cb.RecallAt10 /= cn
		cb.RecallAt20 /= cn
		cb.PrecisionAt5 /= cn
		cb.PrecisionAt10 /= cn
		cb.PrecisionAt20 /= cn
		cb.MRR /= cn
		summary.CategoryBreakdowns = append(summary.CategoryBreakdowns, *cb)
		delete(catMap, cat)
	}
	// Append any remaining categories not in the standard list.
	for _, cb := range catMap {
		cn := float64(cb.Count)
		cb.RecallAt5 /= cn
		cb.RecallAt10 /= cn
		cb.RecallAt20 /= cn
		cb.PrecisionAt5 /= cn
		cb.PrecisionAt10 /= cn
		cb.PrecisionAt20 /= cn
		cb.MRR /= cn
		summary.CategoryBreakdowns = append(summary.CategoryBreakdowns, *cb)
	}
}

// FormatSummaryTable returns an ASCII table summarizing the evaluation results.
func FormatSummaryTable(summary *Summary) string {
	var sb strings.Builder

	// Per-question table.
	sb.WriteString(fmt.Sprintf("%-5s %-14s %-50s  R@5   R@10  R@20  P@5   P@10  P@20  MRR\n",
		"ID", "Category", "Query"))
	sb.WriteString(strings.Repeat("-", 140) + "\n")

	for _, r := range summary.Results {
		q := r.Query
		if len(q) > 48 {
			q = q[:48] + ".."
		}
		sb.WriteString(fmt.Sprintf("%-5s %-14s %-50s  %.2f  %.2f  %.2f  %.2f  %.2f  %.2f  %.2f\n",
			r.ID, r.Category, q,
			r.RecallAt5, r.RecallAt10, r.RecallAt20,
			r.PrecisionAt5, r.PrecisionAt10, r.PrecisionAt20,
			r.MRR))
	}

	// Overall metrics.
	sb.WriteString(strings.Repeat("-", 140) + "\n")
	sb.WriteString(fmt.Sprintf("%-5s %-14s %-50s  %.2f  %.2f  %.2f  %.2f  %.2f  %.2f  %.2f\n",
		"", "OVERALL", fmt.Sprintf("(%d queries)", len(summary.Results)),
		summary.RecallAt5, summary.RecallAt10, summary.RecallAt20,
		summary.PrecisionAt5, summary.PrecisionAt10, summary.PrecisionAt20,
		summary.MRR))

	// Category breakdowns.
	sb.WriteString("\nCategory Breakdowns:\n")
	sb.WriteString(fmt.Sprintf("  %-14s  %5s  R@5   R@10  R@20  P@5   P@10  P@20  MRR\n",
		"Category", "Count"))
	sb.WriteString("  " + strings.Repeat("-", 90) + "\n")

	for _, cb := range summary.CategoryBreakdowns {
		sb.WriteString(fmt.Sprintf("  %-14s  %5d  %.2f  %.2f  %.2f  %.2f  %.2f  %.2f  %.2f\n",
			cb.Category, cb.Count,
			cb.RecallAt5, cb.RecallAt10, cb.RecallAt20,
			cb.PrecisionAt5, cb.PrecisionAt10, cb.PrecisionAt20,
			cb.MRR))
	}

	// Chunking stats if present.
	if summary.Chunking != nil {
		cs := summary.Chunking
		sb.WriteString(fmt.Sprintf("\nChunking Statistics:\n"))
		sb.WriteString(fmt.Sprintf("  Total fragments: %d\n", cs.TotalFragments))
		sb.WriteString(fmt.Sprintf("  Mean token length:   %.1f\n", cs.MeanTokenLen))
		sb.WriteString(fmt.Sprintf("  Median token length: %.1f\n", cs.MedianTokenLen))
		sb.WriteString(fmt.Sprintf("  P95 token length:    %.1f\n", cs.P95TokenLen))
		sb.WriteString(fmt.Sprintf("  Fragments per file:\n"))

		// Sort files for deterministic output.
		files := make([]string, 0, len(cs.FragmentsPerFile))
		for f := range cs.FragmentsPerFile {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, f := range files {
			sb.WriteString(fmt.Sprintf("    %-40s %d\n", f, cs.FragmentsPerFile[f]))
		}
	}

	return sb.String()
}
