// Package cluster groups source fragments into knowledge units using k-means
// clustering on their embedding vectors.
package cluster

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/query"
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
	Units          int     // number of knowledge units created
	AvgClusterSize float64 // average fragments per unit
	MaxClusterSize int
	MinClusterSize int
}

// ClusterInfo describes a single cluster from RunClustering.
type ClusterInfo struct {
	Index      int
	Members    []model.SourceFragment
	Topic      string
	Confidence model.Confidence
}

// RunClustering clusters all fragments with embeddings using k-means and
// returns cluster information without persisting anything. If k is 0,
// sqrt(n/2) is used as a default.
func RunClustering(ctx context.Context, s store.Store, k int) ([]ClusterInfo, error) {
	fragments, err := s.ExportFragments(ctx)
	if err != nil {
		return nil, fmt.Errorf("export fragments: %w", err)
	}

	if len(fragments) == 0 {
		return nil, nil
	}

	// Filter fragments that actually have embeddings.
	var withEmb []model.SourceFragment
	for _, f := range fragments {
		if len(f.Embedding) > 0 {
			withEmb = append(withEmb, f)
		}
	}
	if len(withEmb) == 0 {
		return nil, nil
	}

	slog.Info("loaded fragments", "total", len(fragments), "withEmbeddings", len(withEmb))

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

	slog.Info("clustering", "k", k, "fragments", len(withEmb))

	// Extract embedding matrix.
	embeddings := make([][]float32, len(withEmb))
	for i, f := range withEmb {
		embeddings[i] = f.Embedding
	}

	// Run k-means.
	assignments, actualIters := KMeans(embeddings, k, 100)

	slog.Info("clustering complete", "iterations", actualIters)

	// Group fragments by cluster.
	grouped := make(map[int][]int) // cluster index -> fragment indices
	for i, c := range assignments {
		grouped[c] = append(grouped[c], i)
	}

	var clusters []ClusterInfo
	for clusterIdx, memberIdxs := range grouped {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		members := make([]model.SourceFragment, len(memberIdxs))
		for i, idx := range memberIdxs {
			members[i] = withEmb[idx]
		}

		clusters = append(clusters, ClusterInfo{
			Index:      clusterIdx,
			Members:    members,
			Topic:      deriveTopic(members),
			Confidence: aggregateConfidence(members),
		})
	}

	// Sort by index for deterministic output.
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Index < clusters[j].Index
	})

	return clusters, nil
}

// ComputeUnits clusters all fragments with embeddings into knowledge units,
// computes centroids and confidence signals, and persists them. If k is 0,
// sqrt(n/2) is used as a default.
func (e *Engine) ComputeUnits(ctx context.Context, k int) (*Result, error) {
	clusters, err := RunClustering(ctx, e.store, k)
	if err != nil {
		return nil, err
	}

	if len(clusters) == 0 {
		return &Result{}, nil
	}

	// Clear existing knowledge units.
	if err := e.store.DeleteAllKnowledgeUnits(ctx); err != nil {
		return nil, fmt.Errorf("clear existing units: %w", err)
	}

	now := time.Now()
	totalFragments := 0
	result := &Result{
		Units:          len(clusters),
		MinClusterSize: len(clusters[0].Members), // will be reduced
	}

	for _, ci := range clusters {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		memberEmbeddings := make([][]float32, len(ci.Members))
		for i, m := range ci.Members {
			memberEmbeddings[i] = m.Embedding
		}

		centroid := computeCentroid(memberEmbeddings)
		unitID := computeUnitID(ci.Index, ci.Members)

		unit := model.KnowledgeUnit{
			ID:           unitID,
			Topic:        ci.Topic,
			Summary:      buildSummary(ci.Members),
			FragmentIDs:  fragmentIDs(ci.Members),
			Confidence:   ci.Confidence,
			Centroid:     centroid,
			LastComputed: now,
		}

		if err := e.store.UpsertKnowledgeUnit(ctx, unit); err != nil {
			return nil, fmt.Errorf("upsert unit %s: %w", unitID, err)
		}

		size := len(ci.Members)
		totalFragments += size
		if size > result.MaxClusterSize {
			result.MaxClusterSize = size
		}
		if size < result.MinClusterSize {
			result.MinClusterSize = size
		}
	}

	if result.Units > 0 {
		result.AvgClusterSize = float64(totalFragments) / float64(result.Units)
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

// deriveTopic produces a descriptive topic label from the members' source
// paths. It picks the most common directory (keeping up to 2 path levels),
// then disambiguates with the dominant file extension when the directory
// alone is too generic.
func deriveTopic(members []model.SourceFragment) string {
	if len(members) == 0 {
		return "unknown"
	}

	// Count full directory paths.
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

	// Determine source prefix if the cluster spans a single source.
	sourcePrefix := dominantSourceLabel(members)

	if bestDir == "" || bestDir == "." || bestDir == "/" {
		// Root-level files — use dominant extension.
		ext := dominantExtension(members)
		topic := "root"
		if ext != "" {
			topic += " " + ext
		}
		if sourcePrefix != "" {
			return sourcePrefix + ": " + topic
		}
		return topic
	}

	// Keep last 2 path components for specificity.
	topic := compactPath(bestDir, 2)

	// If the topic is only 1 level and the dominant dir covers less than 70%
	// of members, append the dominant extension to differentiate.
	parts := strings.Split(topic, "/")
	if len(parts) == 1 && bestCount < (len(members)*7/10) {
		ext := dominantExtension(members)
		if ext != "" {
			topic += " " + ext
		}
	}

	if sourcePrefix != "" {
		return sourcePrefix + ": " + topic
	}
	return topic
}

// compactPath returns the last n path components of p.
func compactPath(p string, n int) string {
	if p == "" || p == "." || p == "/" {
		return p
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) <= n {
		return strings.Join(parts, "/")
	}
	return strings.Join(parts[len(parts)-n:], "/")
}

// dominantSourceLabel returns the source name if a clear majority (>=70%) of
// members come from one source. Returns "" if the cluster is mixed across
// many sources (the source column in the table already covers that case).
func dominantSourceLabel(members []model.SourceFragment) string {
	counts := make(map[string]int)
	for _, m := range members {
		counts[m.SourceName]++
	}
	best, bestCount := "", 0
	for s, c := range counts {
		if c > bestCount || (c == bestCount && s < best) {
			best = s
			bestCount = c
		}
	}
	if len(counts) == 1 || bestCount >= (len(members)*7/10) {
		return best
	}
	return ""
}

// dominantExtension returns the most common file extension among members.
func dominantExtension(members []model.SourceFragment) string {
	counts := make(map[string]int)
	for _, m := range members {
		ext := strings.ToLower(path.Ext(m.SourcePath))
		if ext != "" {
			counts[ext]++
		}
	}
	best, bestCount := "", 0
	for e, c := range counts {
		if c > bestCount || (c == bestCount && e < best) {
			best = e
			bestCount = c
		}
	}
	return best
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
func aggregateConfidence(members []model.SourceFragment) model.Confidence {
	if len(members) == 0 {
		return model.Confidence{
			Overall: 0,
			Breakdown: model.ConfidenceBreakdown{
				Freshness:     0,
				Corroboration: 0,
				Consistency:   0,
				Authority:     0,
			},
		}
	}

	var freshSum, consistSum, authSum float64
	sourceNames := make(map[string]struct{})

	for _, m := range members {
		freshSum += query.ComputeFreshness(m.ContentDate, m.IngestedAt, m.FileType)
		consistSum += query.ComputeConsistency(m.ConfidenceAdj)
		authSum += query.ComputeAuthority(m.FileType)
		sourceNames[m.SourceName] = struct{}{}
	}

	n := float64(len(members))
	breakdown := model.ConfidenceBreakdown{
		Freshness:     round2(freshSum / n),
		Corroboration: query.ComputeCorroboration(len(sourceNames)),
		Consistency:   round2(consistSum / n),
		Authority:     round2(authSum / n),
	}
	return model.Confidence{
		Overall:   query.ComputeOverallTrust(breakdown, model.DefaultTrustWeights()),
		Breakdown: breakdown,
	}
}

// computeOverallTrust computes a weighted composite trust score from the breakdown.
func computeOverallTrust(b model.ConfidenceBreakdown, w model.TrustWeights) float64 {
	score := b.Freshness*w.Freshness + b.Corroboration*w.Corroboration + b.Consistency*w.Consistency + b.Authority*w.Authority
	return round2(score)
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


func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// --- K-Means implementation ---

// KMeans performs k-means clustering with k-means++ initialization.
// Returns a slice of cluster assignments (one per input embedding) and the
// number of iterations actually performed.
func KMeans(embeddings [][]float32, k, maxIter int) ([]int, int) {
	n := len(embeddings)
	if n == 0 || k <= 0 {
		return nil, 0
	}
	if k >= n {
		// Each point is its own cluster.
		assignments := make([]int, n)
		for i := range assignments {
			assignments[i] = i
		}
		return assignments, 0
	}

	dim := len(embeddings[0])

	// K-means++ initialization.
	centroids := kmeansppInit(embeddings, k)

	assignments := make([]int, n)
	var actualIter int
	for iter := 0; iter < maxIter; iter++ {
		actualIter = iter + 1

		if iter%10 == 0 {
			slog.Info("k-means", "iteration", iter, "maxIter", maxIter)
		}

		// Assignment step: assign each point to the nearest centroid.
		numChanged := 0
		for i, emb := range embeddings {
			nearest := nearestCentroid(emb, centroids)
			if nearest != assignments[i] {
				assignments[i] = nearest
				numChanged++
			}
		}

		if numChanged == 0 {
			break
		}

		// Convergence threshold: if less than 0.1% of points changed, stop.
		if iter > 0 && float64(numChanged)/float64(n) < 0.001 {
			slog.Info("k-means converged early", "iteration", iter, "changed", numChanged, "total", n)
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

	return assignments, actualIter
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
