package query

import (
	"strings"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/model"
)

func TestBuildSystemPrompt_NoFragments(t *testing.T) {
	prompt := BuildSystemPrompt(nil, false)
	if prompt == "" {
		t.Fatal("expected non-empty prompt for nil fragments")
	}
	// Should still have instructions
	if !strings.Contains(prompt, "Knowledge Broker") {
		t.Error("prompt should mention Knowledge Broker")
	}
	if !strings.Contains(prompt, "KB_META") {
		t.Error("prompt should contain KB_META instruction")
	}
	// Should have no fragment headers
	if strings.Contains(prompt, "### Fragment:") {
		t.Error("prompt should not contain fragment headers when no fragments provided")
	}
}

func TestBuildSystemPrompt_WithFragments(t *testing.T) {
	now := time.Now()
	fragments := []model.SourceFragment{
		{
			ID:            "frag-001",
			Content:       "This is the content of fragment one.",
			SourceType:    "filesystem",
			SourcePath:    "/docs/readme.md",
			SourceURI:     "file:///docs/readme.md",
			LastModified:  now.Add(-24 * time.Hour),
			Author:        "alice",
			FileType:      "markdown",
			ConfidenceAdj: 0.1,
		},
		{
			ID:           "frag-002",
			Content:      "Second fragment content here.",
			SourceType:   "github",
			SourcePath:   "repo/src/main.go",
			SourceURI:    "https://github.com/org/repo/blob/main/src/main.go",
			LastModified: now.Add(-48 * time.Hour),
			FileType:     "go",
		},
	}

	prompt := BuildSystemPrompt(fragments, false)

	// Should include both fragment IDs
	if !strings.Contains(prompt, "frag-001") {
		t.Error("prompt should contain frag-001")
	}
	if !strings.Contains(prompt, "frag-002") {
		t.Error("prompt should contain frag-002")
	}

	// Should include content
	if !strings.Contains(prompt, "This is the content of fragment one.") {
		t.Error("prompt should contain fragment one content")
	}
	if !strings.Contains(prompt, "Second fragment content here.") {
		t.Error("prompt should contain fragment two content")
	}

	// Should include source paths
	if !strings.Contains(prompt, "/docs/readme.md") {
		t.Error("prompt should contain first source path")
	}
	if !strings.Contains(prompt, "repo/src/main.go") {
		t.Error("prompt should contain second source path")
	}

	// Should include author only for the fragment that has one
	if !strings.Contains(prompt, "Author: alice") {
		t.Error("prompt should contain author for frag-001")
	}

	// Should include confidence adjustment only for the fragment that has one
	if !strings.Contains(prompt, "Confidence adjustment: 0.10") {
		t.Error("prompt should contain confidence adjustment for frag-001")
	}

	// Should include file type
	if !strings.Contains(prompt, "markdown") {
		t.Error("prompt should contain file type markdown")
	}

	// Should include structured output instructions
	if !strings.Contains(prompt, "---KB_META---") {
		t.Error("prompt should contain KB_META start delimiter")
	}
	if !strings.Contains(prompt, "---KB_META_END---") {
		t.Error("prompt should contain KB_META end delimiter")
	}

	// Should include confidence signal instructions
	if !strings.Contains(prompt, "Freshness") {
		t.Error("prompt should explain freshness signal")
	}
	if !strings.Contains(prompt, "Corroboration") {
		t.Error("prompt should explain corroboration signal")
	}
	if !strings.Contains(prompt, "Consistency") {
		t.Error("prompt should explain consistency signal")
	}
	if !strings.Contains(prompt, "Authority") {
		t.Error("prompt should explain authority signal")
	}
}

func TestBuildSystemPrompt_AgeFormatting(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		modified time.Time
		contains string
	}{
		{"today", now, "today"},
		{"1 day", now.Add(-25 * time.Hour), "1 day"},
		{"5 days", now.Add(-5 * 24 * time.Hour), "5 days"},
		{"1 month", now.Add(-35 * 24 * time.Hour), "1 month"},
		{"3 months", now.Add(-95 * 24 * time.Hour), "3 months"},
		{"1 year", now.Add(-400 * 24 * time.Hour), "1 year"},
		{"2 years", now.Add(-800 * 24 * time.Hour), "2 years"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fragments := []model.SourceFragment{
				{
					ID:           "test-frag",
					Content:      "test",
					SourcePath:   "/test",
					LastModified: tt.modified,
				},
			}
			prompt := BuildSystemPrompt(fragments, false)
			if !strings.Contains(prompt, tt.contains+" ago") && !strings.Contains(prompt, "(today)") {
				t.Errorf("expected prompt to contain %q age indicator", tt.contains)
			}
		})
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{0, "today"},
		{12 * time.Hour, "today"},
		{25 * time.Hour, "1 day"},
		{3 * 24 * time.Hour, "3 days"},
		{29 * 24 * time.Hour, "29 days"},
		{31 * 24 * time.Hour, "1 month"},
		{90 * 24 * time.Hour, "3 months"},
		{365 * 24 * time.Hour, "1 year"},
		{730 * 24 * time.Hour, "2 years"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatAge(tt.duration)
			if got != tt.expected {
				t.Errorf("formatAge(%v) = %q, want %q", tt.duration, got, tt.expected)
			}
		})
	}
}
