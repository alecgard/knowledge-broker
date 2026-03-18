package query

import (
	"strings"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
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
			RawContent:       "This is the content of fragment one.",
			SourceType:    "filesystem",
			SourcePath:    "/docs/readme.md",
			SourceURI:     "file:///docs/readme.md",
			ContentDate:  now.Add(-24 * time.Hour),
			Author:        "alice",
			FileType:      "markdown",
			ConfidenceAdj: 0.1,
		},
		{
			ID:           "frag-002",
			RawContent:      "Second fragment content here.",
			SourceType:   "github",
			SourcePath:   "repo/src/main.go",
			SourceURI:    "https://github.com/org/repo/blob/main/src/main.go",
			ContentDate: now.Add(-48 * time.Hour),
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
					RawContent:      "test",
					SourcePath:   "/test",
					ContentDate: tt.modified,
				},
			}
			prompt := BuildSystemPrompt(fragments, false)
			if !strings.Contains(prompt, tt.contains+" ago") && !strings.Contains(prompt, "(today)") {
				t.Errorf("expected prompt to contain %q age indicator", tt.contains)
			}
		})
	}
}

func TestBuildLocalPrompt_NoMetaBlocks(t *testing.T) {
	now := time.Now()
	fragments := []model.SourceFragment{
		{
			ID:          "frag-local-1",
			RawContent:  "Authentication uses OAuth2 tokens.",
			SourceType:  "filesystem",
			SourceName:  "docs",
			SourcePath:  "/docs/auth.md",
			SourceURI:   "file:///docs/auth.md",
			ContentDate: now.Add(-24 * time.Hour),
			Author:      "alice",
			FileType:    "markdown",
		},
		{
			ID:         "frag-local-2",
			RawContent: "Roles are defined in the config file.",
			SourceType: "github",
			SourcePath: "repo/src/roles.go",
			SourceURI:  "https://github.com/org/repo/blob/main/src/roles.go",
			ContentDate: now.Add(-48 * time.Hour),
			FileType:   "go",
		},
	}

	prompt := BuildLocalPrompt(fragments)

	// Must NOT contain structured output markers.
	if strings.Contains(prompt, "---KB_META---") {
		t.Error("local prompt must not contain ---KB_META--- marker")
	}
	if strings.Contains(prompt, "---KB_META_END---") {
		t.Error("local prompt must not contain ---KB_META_END--- marker")
	}

	// Must contain Knowledge Broker identity.
	if !strings.Contains(prompt, "Knowledge Broker") {
		t.Error("prompt should mention Knowledge Broker")
	}

	// Must contain fragment content.
	if !strings.Contains(prompt, "Authentication uses OAuth2 tokens.") {
		t.Error("prompt should contain first fragment content")
	}
	if !strings.Contains(prompt, "Roles are defined in the config file.") {
		t.Error("prompt should contain second fragment content")
	}

	// Must use numbered citations [1], [2].
	if !strings.Contains(prompt, "[1]") {
		t.Error("prompt should contain [1] numbered source")
	}
	if !strings.Contains(prompt, "[2]") {
		t.Error("prompt should contain [2] numbered source")
	}

	// Must contain source paths.
	if !strings.Contains(prompt, "/docs/auth.md") {
		t.Error("prompt should contain source path")
	}

	// Must contain source name for the fragment that has one.
	if !strings.Contains(prompt, "(docs)") {
		t.Error("prompt should contain source name")
	}

	// Must contain the "no JSON" instruction.
	if !strings.Contains(prompt, "Do NOT emit any JSON") {
		t.Error("prompt should instruct against JSON output")
	}

	// Must contain author for the fragment that has one.
	if !strings.Contains(prompt, "Author: alice") {
		t.Error("prompt should contain author for first fragment")
	}
}

func TestBuildLocalPrompt_NoFragments(t *testing.T) {
	prompt := BuildLocalPrompt(nil)
	if !strings.Contains(prompt, "Knowledge Broker") {
		t.Error("prompt should mention Knowledge Broker even with no fragments")
	}
	if strings.Contains(prompt, "### Fragment:") {
		t.Error("prompt should not contain fragment headers when no fragments provided")
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
