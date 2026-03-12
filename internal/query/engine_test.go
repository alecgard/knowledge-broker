package query

import (
	"math"
	"testing"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func TestCombineEmbeddings_ZeroWeight(t *testing.T) {
	a := []float32{3, 4}
	b := []float32{10, 20}
	combined := combineEmbeddings(a, b, 0.0)

	// With weight 0, the result should be just normalized(a).
	norm := float32(math.Sqrt(float64(3*3 + 4*4))) // 5
	wantX := 3.0 / float64(norm)
	wantY := 4.0 / float64(norm)

	if math.Abs(float64(combined[0])-wantX) > 1e-5 || math.Abs(float64(combined[1])-wantY) > 1e-5 {
		t.Fatalf("expected normalized a, got [%f, %f]", combined[0], combined[1])
	}
}

func TestCombineEmbeddings_IsNormalized(t *testing.T) {
	a := []float32{1, 2, 3, 4}
	b := []float32{5, 6, 7, 8}
	combined := combineEmbeddings(a, b, 0.3)

	var norm float64
	for _, v := range combined {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 1e-5 {
		t.Fatalf("expected unit norm, got %f", norm)
	}
}

func TestComputeOverallTrust(t *testing.T) {
	b := model.ConfidenceBreakdown{
		Freshness:     0.94,
		Corroboration: 0.85,
		Consistency:   1.00,
		Authority:     0.95,
	}
	w := model.DefaultTrustWeights()
	got := ComputeOverallTrust(b, w)
	// 0.94*0.20 + 0.85*0.25 + 1.00*0.30 + 0.95*0.25
	// = 0.188 + 0.2125 + 0.30 + 0.2375 = 0.938 → rounded to 0.94
	want := 0.94
	if got != want {
		t.Errorf("ComputeOverallTrust() = %v, want %v", got, want)
	}
}

func TestCombineEmbeddings_ShiftsDirection(t *testing.T) {
	// a points along x, b along y. Combining should produce a vector
	// that has a positive y component (shifted toward b).
	a := []float32{1, 0}
	b := []float32{0, 1}
	combined := combineEmbeddings(a, b, 0.3)

	if combined[1] <= 0 {
		t.Fatalf("expected positive y component from topic blending, got %f", combined[1])
	}
	// The x component should still dominate since weight < 1.
	if combined[0] <= combined[1] {
		t.Fatalf("expected x > y since weight is 0.3, got x=%f y=%f", combined[0], combined[1])
	}
}
