// Package cluster groups source fragments into knowledge units using k-means
// clustering on their embedding vectors.
package cluster

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"math/rand"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/store"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// Engine computes knowledge units from source fragment embeddings.
type Engine struct {
	store store.Store
}

// NewEngine creates a clustering engine.
func NewEngine(s store.Store) *Engine {
	return &Engine{store: s}
}

// Result summarises a clustering run.
type Result struct {
	Units           int     // number of knowledge units created
	AvgClusterSize  float64 // average fragments per unit
	MaxClusterSize  int
	MinClusterSize  int
}

// ComputeUnits clusters all fragments with embeddings into knowledge units,
// computes centroids and confidence signals, and persists them. If k is 0,
// sqrt(n/2) is used as a default.
func (e *Engine) ComputeUnits(ctx context.Context, k int) (*Result, error) {
	fragments, err := e.store.ExportFragments(ctx)
	if err != nil {
		return nil, fmt.Errorf("export fragments: %w", err)
	}

	if len(fragments) == 0 {
		return &Result{}, nil
	}

	// Filter fragments that actually have embeddings.
	var withEmb []model.SourceFragment
	for _, f := range fragments {
		if len(f.Embedding) > 0 {
			withEmb = append(withEmb, f)
		}
	}
	if len(withEmb) == 0 {
		return &Result{}, nil
	}

	// Determine k.
	if k <= 0 {
		k = int(math.Sqrt(float64(len(withEmb)) / 2.0))
		if k < 1 {
			k = 1
		}
	}
	if k > len(withEmb) {
		k = len(withEmb)
	}

	// Extract embedding matrix.
	embeddings := make([][]float32, len(withEmb))
	for i, f := range withEmb {
		embeddings[i] = f.Embedding
	}

	// Run k-means.
	assignments := KMeans(embeddings, k, 100)

	// Group fragments by cluster.
	clusters := make(map[int][]int) // cluster index -> fragment indices
	for i, c := range assignments {
		clusters[c] = append(clusters[c], i)
	}

	// Clear existing knowledge units.
	if err := e.store.DeleteAllKnowledgeUnits(ctx); err != nil {
		return nil, fmt.Errorf("clear existing units: %w", err)
	}

	now := time.Now()
	result := &Result{
		Units:          len(clusters),
		MinClusterSize: len(withEmb), // will be reduced
	}

	for clusterIdx, memberIdxs := range clusters {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		members := make([]model.SourceFragment, len(memberIdxs))
		memberEmbeddings := make([][]float32, len(memberIdxs))
		for i, idx := range memberIdxs {
			members[i] = withEmb[idx]
			memberEmbeddings[i] = withEmb[idx].Embedding
		}

		centroid := computeCentroid(memberEmbeddings)
		topic := deriveTopic(members)
		confidence := aggregateConfidence(members)

		// Generate deterministic ID from cluster index and member IDs.
		unitID := computeUnitID(clusterIdx, members)

		unit := model.KnowledgeUnit{
			ID:           unitID,
			Topic:        topic,
			Summary:      buildSummary(members),
			FragmentIDs:  fragmentIDs(members),
			Confidence:   confidence,
			Centroid:     centroid,
			LastComputed: now,
		}

		if err := e.store.UpsertKnowledgeUnit(ctx, unit); err != nil {
			return nil, fmt.Errorf("upsert unit %s: %w", unitID, err)
		}

		size := len(members)
		if size > result.MaxClusterSize {
			result.MaxClusterSize = size
		}
		if size < result.MinClusterSize {
			result.MinClusterSize = size
		}
	}

	if result.Units > 0 {
		result.AvgClusterSize = float64(len(withEmb)) / float64(result.Units)
	}

	return result, nil
}

// computeUnitID generates a deterministic ID for a knowledge unit based on
// sorted member fragment IDs.
func computeUnitID(clusterIdx int, members []model.SourceFragment) string {
	ids := fragmentIDs(members)
	sort.Strings(ids)
	input := fmt.Sprintf("unit:%d:%s", clusterIdx, strings.Join(ids, ","))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))[:16]
}

// fragmentIDs extracts fragment IDs from a slice of fragments.
func fragmentIDs(fragments []model.SourceFragment) []string {
	ids := make([]string, len(fragments))
	for i, f := range fragments {
		ids[i] = f.ID
	}
	return ids
}

// computeCentroid returns the mean embedding vector of the given embeddings.
func computeCentroid(embeddings [][]float32) []float32 {
	if len(embeddings) == 0 {
		return nil
	}
	dim := len(embeddings[0])
	centroid := make([]float32, dim)
	for _, emb := range embeddings {
		for j, v := range emb {
			centroid[j] += v
		}
	}
	n := float32(len(embeddings))
	for j := range centroid {
		centroid[j] /= n
	}
	return centroid
}

// deriveTopic produces a short topic label from the members' source paths.
// It finds the longest common path prefix and uses the most common directory
// or file name as the topic.
func deriveTopic(members []model.SourceFragment) string {
	if len(members) == 0 {
		return "unknown"
	}

	// Collect directory paths.
	dirs := make(map[string]int)
	for _, m := range members {
		dir := path.Dir(m.SourcePath)
		dirs[dir]++
	}

	// Find most common directory.
	bestDir := ""
	bestCount := 0
	for d, c := range dirs {
		if c > bestCount || (c == bestCount && d < bestDir) {
			bestDir = d
			bestCount = c
		}
	}

	// Use the last component of the most common directory, or the common prefix.
	if bestDir == "." || bestDir == "" || bestDir == "/" {
		// Fall back to common prefix of source paths.
		paths := make([]string, len(members))
		for i, m := range members {
			paths[i] = m.SourcePath
		}
		prefix := commonPrefix(paths)
		if prefix != "" && prefix != "/" && prefix != "." {
			return path.Base(prefix)
		}
		return "general"
	}

	return path.Base(bestDir)
}

// commonPrefix returns the longest common directory prefix of a set of paths.
func commonPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	prefix := paths[0]
	for _, p := range paths[1:] {
		for !strings.HasPrefix(p, prefix) {
			prefix = path.Dir(prefix)
			if prefix == "." || prefix == "/" {
				return prefix
			}
		}
	}
	return prefix
}

// aggregateConfidence computes aggregate confidence signals for a cluster of
// fragments.
func aggregateConfidence(members []model.SourceFragment) model.ConfidenceSignals {
	if len(members) == 0 {
		return model.ConfidenceSignals{}
	}

	var freshSum, consistSum, authSum float64
	sourceNames := make(map[string]struct{})

	for _, m := range members {
		freshSum += computeFreshness(m.LastModified)
		consistSum += computeConsistency(m.ConfidenceAdj)
		authSum += computeAuthority(m.FileType)
		sourceNames[m.SourceName] = struct{}{}
	}

	n := float64(len(members))
	return model.ConfidenceSignals{
		Freshness:     round2(freshSum / n),
		Corroboration: computeCorroboration(len(sourceNames)),
		Consistency:   round2(consistSum / n),
		Authority:     round2(authSum / n),
	}
}

// buildSummary constructs a brief summary listing the source paths in the
// cluster.
func buildSummary(members []model.SourceFragment) string {
	paths := make(map[string]struct{})
	for _, m := range members {
		paths[m.SourcePath] = struct{}{}
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	const maxPaths = 5
	if len(sorted) > maxPaths {
		return fmt.Sprintf("Covers %d files including %s", len(sorted), strings.Join(sorted[:maxPaths], ", "))
	}
	return fmt.Sprintf("Covers %s", strings.Join(sorted, ", "))
}

// --- Confidence helpers (same logic as query engine, kept local to avoid
//     circular imports). ---

func computeFreshness(lastModified time.Time) float64 {
	if lastModified.IsZero() {
		return 0.3
	}
	days := time.Since(lastModified).Hours() / 24
	if days <= 0 {
		return 1.0
	}
	score := math.Exp(-days / 130.0)
	if score < 0.1 {
		score = 0.1
	}
	return round2(score)
}

func computeCorroboration(numSources int) float64 {
	if numSources <= 0 {
		return 0.0
	}
	if numSources == 1 {
		return 0.3
	}
	if numSources == 2 {
		return 0.6
	}
	if numSources >= 5 {
		return 1.0
	}
	return 0.8
}

func computeConsistency(confidenceAdj float64) float64 {
	score := 0.5 + confidenceAdj
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return round2(score)
}

func computeAuthority(fileType string) float64 {
	ext := strings.ToLower(fileType)
	if ext == "" {
		return 0.5
	}
	if ext[0] != '.' {
		ext = "." + ext
	}
	switch ext {
	case ".md", ".markdown", ".rst":
		return 0.8
	case ".go", ".py", ".js", ".ts", ".java", ".rs", ".c", ".cpp", ".rb":
		return 0.7
	case ".yaml", ".yml", ".toml", ".json":
		return 0.65
	case ".txt":
		return 0.6
	case ".html", ".htm", ".xml":
		return 0.55
	default:
		return 0.5
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// --- K-Means implementation ---

// KMeans performs k-means clustering with k-means++ initialization.
// Returns a slice of cluster assignments (one per input embedding).
func KMeans(embeddings [][]float32, k, maxIter int) []int {
	n := len(embeddings)
	if n == 0 || k <= 0 {
		return nil
	}
	if k >= n {
		// Each point is its own cluster.
		assignments := make([]int, n)
		for i := range assignments {
			assignments[i] = i
		}
		return assignments
	}

	dim := len(embeddings[0])

	// K-means++ initialization.
	centroids := kmeansppInit(embeddings, k)

	assignments := make([]int, n)
	for iter := 0; iter < maxIter; iter++ {
		// Assignment step: assign each point to the nearest centroid.
		changed := false
		for i, emb := range embeddings {
			nearest := nearestCentroid(emb, centroids)
			if nearest != assignments[i] {
				assignments[i] = nearest
				changed = true
			}
		}

		if !changed {
			break
		}

		// Update step: recompute centroids.
		newCentroids := make([][]float32, k)
		counts := make([]int, k)
		for c := 0; c < k; c++ {
			newCentroids[c] = make([]float32, dim)
		}
		for i, emb := range embeddings {
			c := assignments[i]
			counts[c]++
			for j, v := range emb {
				newCentroids[c][j] += v
			}
		}
		for c := 0; c < k; c++ {
			if counts[c] > 0 {
				for j := range newCentroids[c] {
					newCentroids[c][j] /= float32(counts[c])
				}
			} else {
				// Empty cluster: keep old centroid.
				newCentroids[c] = centroids[c]
			}
		}
		centroids = newCentroids
	}

	return assignments
}

// kmeansppInit selects k initial centroids using the k-means++ algorithm.
func kmeansppInit(embeddings [][]float32, k int) [][]float32 {
	n := len(embeddings)
	rng := rand.New(rand.NewSource(42)) // deterministic for reproducibility

	centroids := make([][]float32, 0, k)

	// Pick first centroid uniformly at random.
	first := rng.Intn(n)
	centroids = append(centroids, copyVec(embeddings[first]))

	// Distance from each point to nearest existing centroid.
	dists := make([]float64, n)
	for i := range dists {
		dists[i] = math.MaxFloat64
	}

	for len(centroids) < k {
		// Update distances to nearest centroid.
		last := centroids[len(centroids)-1]
		var totalDist float64
		for i, emb := range embeddings {
			d := distSq(emb, last)
			if d < dists[i] {
				dists[i] = d
			}
			totalDist += dists[i]
		}

		// Weighted random selection.
		threshold := rng.Float64() * totalDist
		var cumSum float64
		chosen := n - 1
		for i, d := range dists {
			cumSum += d
			if cumSum >= threshold {
				chosen = i
				break
			}
		}
		centroids = append(centroids, copyVec(embeddings[chosen]))
	}

	return centroids
}

// nearestCentroid returns the index of the nearest centroid.
func nearestCentroid(point []float32, centroids [][]float32) int {
	best := 0
	bestDist := distSq(point, centroids[0])
	for i := 1; i < len(centroids); i++ {
		d := distSq(point, centroids[i])
		if d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}

// distSq computes the squared Euclidean distance between two vectors.
func distSq(a, b []float32) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	return sum
}

// copyVec returns a copy of a float32 slice.
func copyVec(v []float32) []float32 {
	out := make([]float32, len(v))
	copy(out, v)
	return out
}
