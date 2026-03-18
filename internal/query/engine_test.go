package query

import (
	"math"
	"reflect"
	"testing"
	"time"

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

func TestParseCitations(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "no citations",
			text: "This is plain text with no citations.",
			want: nil,
		},
		{
			name: "single citation",
			text: "The retry logic uses backoff [abc12345].",
			want: []string{"abc12345"},
		},
		{
			name: "multiple citations",
			text: "Found in [abc12345] and confirmed by [def67890].",
			want: []string{"abc12345", "def67890"},
		},
		{
			name: "duplicate citations",
			text: "See [abc12345] and also [abc12345] again.",
			want: []string{"abc12345"},
		},
		{
			name: "16-char hex id",
			text: "Source [abcdef0123456789] confirms this.",
			want: []string{"abcdef0123456789"},
		},
		{
			name: "short id ignored",
			text: "Not a citation [abc].",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCitations(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseCitations() = %v, want %v", got, tt.want)
			}
		})
	}
}

func testFragments() []model.SourceFragment {
	now := time.Now()
	return []model.SourceFragment{
		{
			ID:          "abcdef0123456789",
			RawContent:  "fragment one content",
			SourceType:  "filesystem",
			SourceName:  "project-a",
			SourcePath:  "/docs/readme.md",
			SourceURI:   "file:///docs/readme.md",
			ContentDate: now.Add(-24 * time.Hour),
			IngestedAt:  now.Add(-1 * time.Hour),
			FileType:    ".md",
		},
		{
			ID:          "1234567890abcdef",
			RawContent:  "fragment two content",
			SourceType:  "github",
			SourceName:  "project-b",
			SourcePath:  "src/main.go",
			SourceURI:   "https://github.com/org/repo/blob/main/src/main.go",
			ContentDate: now.Add(-48 * time.Hour),
			IngestedAt:  now.Add(-2 * time.Hour),
			FileType:    ".go",
		},
	}
}

func TestBuildAnswer_PlainText(t *testing.T) {
	frags := testFragments()
	response := "The answer is X [abcdef0123456789] and Y [1234567890abcdef]."
	answer := buildAnswer(response, frags)

	if answer.Content != response {
		t.Errorf("expected content to be unchanged, got %q", answer.Content)
	}
	if answer.Confidence.Overall == 0 {
		t.Error("expected non-zero overall confidence")
	}
	if answer.Confidence.Breakdown.Freshness == 0 {
		t.Error("expected non-zero freshness")
	}
	if len(answer.Sources) != 2 {
		t.Errorf("expected 2 sources from citations, got %d", len(answer.Sources))
	}
}

func TestBuildAnswer_NoCitations_FallsBackToAllFragments(t *testing.T) {
	frags := testFragments()
	response := "The answer is based on the available documents."
	answer := buildAnswer(response, frags)

	if len(answer.Sources) != len(frags) {
		t.Errorf("expected %d sources (all fragments), got %d", len(frags), len(answer.Sources))
	}
}

func TestBuildAnswer_StripsLegacyMeta(t *testing.T) {
	frags := testFragments()
	response := `The answer is X.

---KB_META---
{"confidence":{"overall":0.84,"breakdown":{"freshness":0.9,"corroboration":0.8,"consistency":0.95,"authority":0.7}},"sources":[],"contradictions":[]}
---KB_META_END---`

	answer := buildAnswer(response, frags)

	if answer.Content != "The answer is X." {
		t.Errorf("expected legacy meta stripped, got %q", answer.Content)
	}
	// Confidence should be server-computed, not from the legacy block.
	if answer.Confidence.Overall == 0 {
		t.Error("expected non-zero server-computed confidence")
	}
}

func TestBuildAnswer_CitedSubsetOnly(t *testing.T) {
	frags := testFragments()
	// Only cite the first fragment.
	response := "The answer is X [abcdef0123456789]."
	answer := buildAnswer(response, frags)

	if len(answer.Sources) != 1 {
		t.Fatalf("expected 1 source from citation, got %d", len(answer.Sources))
	}
	if answer.Sources[0].FragmentID != "abcdef0123456789" {
		t.Errorf("expected cited fragment, got %q", answer.Sources[0].FragmentID)
	}
}

func TestComputeConfidence(t *testing.T) {
	frags := testFragments()
	conf := computeConfidence(frags)

	if conf.Overall == 0 {
		t.Error("expected non-zero overall confidence")
	}
	if conf.Breakdown.Freshness == 0 {
		t.Error("expected non-zero freshness")
	}
	if conf.Breakdown.Corroboration == 0 {
		t.Error("expected non-zero corroboration")
	}
	if conf.Breakdown.Consistency == 0 {
		t.Error("expected non-zero consistency")
	}
	if conf.Breakdown.Authority == 0 {
		t.Error("expected non-zero authority")
	}
	// Two different source names = corroboration 0.6.
	if conf.Breakdown.Corroboration != 0.6 {
		t.Errorf("expected corroboration 0.6 for 2 sources, got %v", conf.Breakdown.Corroboration)
	}
}
