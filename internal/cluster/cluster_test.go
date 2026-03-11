package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/store"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func TestKMeans_BasicClustering(t *testing.T) {
	// Two clear clusters in 2D space.
	embeddings := [][]float32{
		{0, 0},
		{1, 0},
		{0, 1},
		// cluster 2
		{10, 10},
		{11, 10},
		{10, 11},
	}

	assignments := KMeans(embeddings, 2, 100)

	if len(assignments) != 6 {
		t.Fatalf("expected 6 assignments, got %d", len(assignments))
	}

	// Points 0-2 should be in the same cluster, points 3-5 in another.
	if assignments[0] != assignments[1] || assignments[0] != assignments[2] {
		t.Errorf("points 0-2 should be in the same cluster: %v", assignments)
	}
	if assignments[3] != assignments[4] || assignments[3] != assignments[5] {
		t.Errorf("points 3-5 should be in the same cluster: %v", assignments)
	}
	if assignments[0] == assignments[3] {
		t.Errorf("the two clusters should be different: %v", assignments)
	}
}

func TestKMeans_SingleCluster(t *testing.T) {
	embeddings := [][]float32{
		{1, 2},
		{3, 4},
		{5, 6},
	}

	assignments := KMeans(embeddings, 1, 100)

	if len(assignments) != 3 {
		t.Fatalf("expected 3 assignments, got %d", len(assignments))
	}

	// All should be cluster 0.
	for i, a := range assignments {
		if a != 0 {
			t.Errorf("point %d should be in cluster 0, got %d", i, a)
		}
	}
}

func TestKMeans_KEqualsN(t *testing.T) {
	embeddings := [][]float32{
		{1, 0},
		{0, 1},
		{1, 1},
	}

	assignments := KMeans(embeddings, 3, 100)

	if len(assignments) != 3 {
		t.Fatalf("expected 3 assignments, got %d", len(assignments))
	}

	// Each point should be its own cluster.
	seen := make(map[int]bool)
	for _, a := range assignments {
		seen[a] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct clusters, got %d: %v", len(seen), assignments)
	}
}

func TestKMeans_Empty(t *testing.T) {
	assignments := KMeans(nil, 3, 100)
	if assignments != nil {
		t.Errorf("expected nil for empty input, got %v", assignments)
	}
}

func TestComputeCentroid(t *testing.T) {
	embeddings := [][]float32{
		{1, 2, 3},
		{3, 4, 5},
		{5, 6, 7},
	}

	centroid := computeCentroid(embeddings)

	expected := []float32{3, 4, 5}
	for i, v := range centroid {
		if v != expected[i] {
			t.Errorf("centroid[%d] = %f, expected %f", i, v, expected[i])
		}
	}
}

func TestComputeCentroid_Empty(t *testing.T) {
	centroid := computeCentroid(nil)
	if centroid != nil {
		t.Errorf("expected nil for empty input, got %v", centroid)
	}
}

func TestDeriveTopic(t *testing.T) {
	members := []model.SourceFragment{
		{SourcePath: "internal/store/sqlite.go"},
		{SourcePath: "internal/store/store.go"},
		{SourcePath: "internal/store/migrations/001.sql"},
	}

	topic := deriveTopic(members)
	if topic != "store" {
		t.Errorf("expected topic 'store', got %q", topic)
	}
}

func TestDeriveTopic_RootFiles(t *testing.T) {
	members := []model.SourceFragment{
		{SourcePath: "main.go"},
		{SourcePath: "go.mod"},
	}

	topic := deriveTopic(members)
	// Both are in "." directory, should fall back to "general" or some reasonable name.
	if topic == "" {
		t.Error("topic should not be empty")
	}
}

func TestAggregateConfidence(t *testing.T) {
	members := []model.SourceFragment{
		{
			SourceName:   "repo-a",
			FileType:     ".go",
			LastModified: time.Now(),
		},
		{
			SourceName:   "repo-b",
			FileType:     ".md",
			LastModified: time.Now().Add(-24 * time.Hour),
		},
	}

	conf := aggregateConfidence(members)

	if conf.Breakdown.Freshness <= 0 || conf.Breakdown.Freshness > 1 {
		t.Errorf("freshness out of range: %f", conf.Breakdown.Freshness)
	}
	if conf.Breakdown.Corroboration != 0.6 {
		t.Errorf("expected corroboration 0.6 for 2 sources, got %f", conf.Breakdown.Corroboration)
	}
	if conf.Breakdown.Consistency != 0.5 {
		t.Errorf("expected consistency 0.5 (no adjustment), got %f", conf.Breakdown.Consistency)
	}
	if conf.Breakdown.Authority <= 0 || conf.Breakdown.Authority > 1 {
		t.Errorf("authority out of range: %f", conf.Breakdown.Authority)
	}
	if conf.Overall <= 0 || conf.Overall > 1 {
		t.Errorf("overall out of range: %f", conf.Overall)
	}
}

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		paths    []string
		expected string
	}{
		{[]string{"a/b/c", "a/b/d"}, "a/b"},
		{[]string{"a/b", "c/d"}, "."},
		{[]string{"a/b/c"}, "a/b/c"},
		{nil, ""},
	}

	for _, tt := range tests {
		got := commonPrefix(tt.paths)
		if got != tt.expected {
			t.Errorf("commonPrefix(%v) = %q, want %q", tt.paths, got, tt.expected)
		}
	}
}

func TestComputeUnits_Integration(t *testing.T) {
	s, err := store.NewSQLiteStore(":memory:", 4)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Insert fragments with embeddings into two clear groups.
	fragments := []model.SourceFragment{
		{ID: "a1", Content: "auth login", SourceType: "fs", SourcePath: "auth/login.go", SourceURI: "file://auth/login.go", FileType: ".go", Checksum: "c1", LastModified: time.Now(), Embedding: []float32{1, 0, 0, 0}},
		{ID: "a2", Content: "auth token", SourceType: "fs", SourcePath: "auth/token.go", SourceURI: "file://auth/token.go", FileType: ".go", Checksum: "c2", LastModified: time.Now(), Embedding: []float32{0.9, 0.1, 0, 0}},
		{ID: "b1", Content: "db schema", SourceType: "fs", SourcePath: "db/schema.sql", SourceURI: "file://db/schema.sql", FileType: ".sql", Checksum: "c3", LastModified: time.Now(), Embedding: []float32{0, 0, 1, 0}},
		{ID: "b2", Content: "db migrate", SourceType: "fs", SourcePath: "db/migrate.go", SourceURI: "file://db/migrate.go", FileType: ".go", Checksum: "c4", LastModified: time.Now(), Embedding: []float32{0, 0, 0.9, 0.1}},
	}

	if err := s.UpsertFragments(ctx, fragments); err != nil {
		t.Fatalf("upsert fragments: %v", err)
	}

	eng := NewEngine(s)
	result, err := eng.ComputeUnits(ctx, 2)
	if err != nil {
		t.Fatalf("compute units: %v", err)
	}

	if result.Units != 2 {
		t.Errorf("expected 2 units, got %d", result.Units)
	}
	if result.AvgClusterSize != 2.0 {
		t.Errorf("expected avg cluster size 2.0, got %.1f", result.AvgClusterSize)
	}

	// Verify units were persisted.
	units, err := s.ListKnowledgeUnits(ctx)
	if err != nil {
		t.Fatalf("list units: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 persisted units, got %d", len(units))
	}

	// Each unit should have 2 fragment IDs.
	for _, u := range units {
		if len(u.FragmentIDs) != 2 {
			t.Errorf("unit %s: expected 2 fragment IDs, got %d", u.ID, len(u.FragmentIDs))
		}
		if u.Topic == "" {
			t.Errorf("unit %s: topic should not be empty", u.ID)
		}
	}

	// Search by embedding should return results.
	found, err := s.SearchKnowledgeUnits(ctx, []float32{1, 0, 0, 0}, 1)
	if err != nil {
		t.Fatalf("search units: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(found))
	}
	// The found unit should contain the auth fragments.
	hasAuth := false
	for _, fid := range found[0].FragmentIDs {
		if fid == "a1" || fid == "a2" {
			hasAuth = true
			break
		}
	}
	if !hasAuth {
		t.Errorf("search for auth embedding should return auth unit, got fragment IDs: %v", found[0].FragmentIDs)
	}
}

func TestComputeUnits_NoFragments(t *testing.T) {
	s, err := store.NewSQLiteStore(":memory:", 4)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	eng := NewEngine(s)
	result, err := eng.ComputeUnits(context.Background(), 0)
	if err != nil {
		t.Fatalf("compute units: %v", err)
	}
	if result.Units != 0 {
		t.Errorf("expected 0 units for empty store, got %d", result.Units)
	}
}

func TestComputeUnits_AutoK(t *testing.T) {
	s, err := store.NewSQLiteStore(":memory:", 2)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Insert 8 fragments; auto k should be sqrt(8/2) = 2.
	fragments := make([]model.SourceFragment, 8)
	for i := range fragments {
		fragments[i] = model.SourceFragment{
			ID:           fmt.Sprintf("f%d", i),
			Content:      fmt.Sprintf("content %d", i),
			SourceType:   "fs",
			SourcePath:   fmt.Sprintf("dir/file%d.go", i),
			SourceURI:    fmt.Sprintf("file://dir/file%d.go", i),
			FileType:     ".go",
			Checksum:     fmt.Sprintf("c%d", i),
			LastModified: time.Now(),
			Embedding:    []float32{float32(i), float32(i * 2)},
		}
	}

	if err := s.UpsertFragments(ctx, fragments); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	eng := NewEngine(s)
	result, err := eng.ComputeUnits(ctx, 0)
	if err != nil {
		t.Fatalf("compute units: %v", err)
	}

	if result.Units < 1 {
		t.Errorf("expected at least 1 unit, got %d", result.Units)
	}
}
