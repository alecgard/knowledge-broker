package cluster

import (
	"math"
	"math/rand"
	"testing"
)

func TestPCA2D_TwoSeparableClusters(t *testing.T) {
	// Cluster A around origin, Cluster B far away.
	embeddings := [][]float32{
		{0, 0, 0, 0},
		{0.1, 0.1, 0, 0},
		{-0.1, 0.1, 0, 0},
		{10, 10, 0, 0},
		{10.1, 9.9, 0, 0},
		{9.9, 10.1, 0, 0},
	}

	xs, ys := PCA2D(embeddings)
	if len(xs) != 6 || len(ys) != 6 {
		t.Fatalf("expected 6 points, got xs=%d ys=%d", len(xs), len(ys))
	}

	// Points 0-2 should be close to each other, 3-5 should be close to each other.
	// The two groups should be well-separated in the 2D projection.
	centroidA := [2]float64{(xs[0] + xs[1] + xs[2]) / 3, (ys[0] + ys[1] + ys[2]) / 3}
	centroidB := [2]float64{(xs[3] + xs[4] + xs[5]) / 3, (ys[3] + ys[4] + ys[5]) / 3}

	dist := math.Sqrt(math.Pow(centroidA[0]-centroidB[0], 2) + math.Pow(centroidA[1]-centroidB[1], 2))
	if dist < 1.0 {
		t.Errorf("expected clusters to be well-separated, distance=%f", dist)
	}
}

func TestPCA2D_SinglePoint(t *testing.T) {
	embeddings := [][]float32{{1, 2, 3}}

	xs, ys := PCA2D(embeddings)
	if len(xs) != 1 || len(ys) != 1 {
		t.Fatalf("expected 1 point, got xs=%d ys=%d", len(xs), len(ys))
	}

	// Single point centered = origin.
	if xs[0] != 0 || ys[0] != 0 {
		t.Errorf("single point should project to origin, got (%f, %f)", xs[0], ys[0])
	}
}

func TestPCA2D_IdenticalPoints(t *testing.T) {
	embeddings := [][]float32{
		{5, 5, 5},
		{5, 5, 5},
		{5, 5, 5},
	}

	xs, ys := PCA2D(embeddings)
	if len(xs) != 3 || len(ys) != 3 {
		t.Fatalf("expected 3 points, got xs=%d ys=%d", len(xs), len(ys))
	}

	// All identical → all project to the same point (origin after centering).
	for i := range xs {
		if math.Abs(xs[i]) > 1e-6 || math.Abs(ys[i]) > 1e-6 {
			t.Errorf("identical points should all be at origin, point %d: (%f, %f)", i, xs[i], ys[i])
		}
	}
}

func TestPCA2D_768Dim(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	n := 50
	dim := 768
	embeddings := make([][]float32, n)
	for i := range embeddings {
		embeddings[i] = make([]float32, dim)
		for j := range embeddings[i] {
			embeddings[i][j] = rng.Float32()
		}
	}

	xs, ys := PCA2D(embeddings)
	if len(xs) != n || len(ys) != n {
		t.Fatalf("expected %d points, got xs=%d ys=%d", n, len(xs), len(ys))
	}

	// Verify no NaN or Inf values.
	for i := range xs {
		if math.IsNaN(xs[i]) || math.IsInf(xs[i], 0) {
			t.Errorf("xs[%d] is NaN or Inf: %f", i, xs[i])
		}
		if math.IsNaN(ys[i]) || math.IsInf(ys[i], 0) {
			t.Errorf("ys[%d] is NaN or Inf: %f", i, ys[i])
		}
	}
}

func TestPCA2D_Empty(t *testing.T) {
	xs, ys := PCA2D(nil)
	if xs != nil || ys != nil {
		t.Errorf("expected nil for empty input, got xs=%v ys=%v", xs, ys)
	}
}

func TestPCA3D_TwoSeparableClusters(t *testing.T) {
	embeddings := [][]float32{
		{0, 0, 0, 0},
		{0.1, 0.1, 0, 0},
		{10, 10, 0, 0},
		{10.1, 9.9, 0, 0},
	}

	xs, ys, zs := PCA3D(embeddings)
	if len(xs) != 4 || len(ys) != 4 || len(zs) != 4 {
		t.Fatalf("expected 4 points, got xs=%d ys=%d zs=%d", len(xs), len(ys), len(zs))
	}

	// Verify no NaN or Inf.
	for i := range xs {
		if math.IsNaN(xs[i]) || math.IsInf(xs[i], 0) ||
			math.IsNaN(ys[i]) || math.IsInf(ys[i], 0) ||
			math.IsNaN(zs[i]) || math.IsInf(zs[i], 0) {
			t.Errorf("point %d has NaN/Inf: (%f, %f, %f)", i, xs[i], ys[i], zs[i])
		}
	}
}

func TestPCA3D_Empty(t *testing.T) {
	xs, ys, zs := PCA3D(nil)
	if xs != nil || ys != nil || zs != nil {
		t.Errorf("expected nil for empty input")
	}
}
