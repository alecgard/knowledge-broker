package extractor

import (
	"strings"
	"testing"
)

func TestJupyterBasicNotebook(t *testing.T) {
	notebook := `{
  "cells": [
    {
      "cell_type": "markdown",
      "source": ["# My Notebook\n", "\n", "This is the introduction."]
    },
    {
      "cell_type": "code",
      "source": ["import pandas as pd\n", "df = pd.read_csv('data.csv')"]
    },
    {
      "cell_type": "markdown",
      "source": ["## Results\n", "\n", "Here are the results."]
    },
    {
      "cell_type": "code",
      "source": ["print(df.head())"]
    },
    {
      "cell_type": "raw",
      "source": ["This should be skipped"]
    }
  ]
}`

	ext := NewJupyterExtractor(2000)
	chunks, err := ext.Extract([]byte(notebook), ExtractOptions{Path: "analysis.ipynb"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have chunks for markdown and code cells but not raw.
	// Small consecutive cells of the same type may be merged.
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	// Verify no raw cell content.
	for _, ch := range chunks {
		if strings.Contains(ch.Content, "This should be skipped") {
			t.Error("raw cell content should not appear in chunks")
		}
	}

	// Verify we have both markdown and code cells.
	hasMarkdown := false
	hasCode := false
	for _, ch := range chunks {
		switch ch.Metadata["cell_type"] {
		case "markdown":
			hasMarkdown = true
		case "code":
			hasCode = true
		}
	}
	if !hasMarkdown {
		t.Error("expected at least one markdown chunk")
	}
	if !hasCode {
		t.Error("expected at least one code chunk")
	}

	// Check cell_number metadata exists.
	for _, ch := range chunks {
		if ch.Metadata["cell_number"] == "" {
			t.Error("chunk missing cell_number metadata")
		}
	}
}

func TestJupyterEmptyNotebook(t *testing.T) {
	notebook := `{"cells": []}`
	ext := NewJupyterExtractor(2000)
	chunks, err := ext.Extract([]byte(notebook), ExtractOptions{Path: "empty.ipynb"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 empty chunk, got %d", len(chunks))
	}
}

func TestJupyterEmptyCells(t *testing.T) {
	notebook := `{
  "cells": [
    {"cell_type": "code", "source": [""]},
    {"cell_type": "markdown", "source": ["  "]}
  ]
}`
	ext := NewJupyterExtractor(2000)
	chunks, err := ext.Extract([]byte(notebook), ExtractOptions{Path: "blank.ipynb"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty/whitespace cells should be skipped, returning a single empty chunk.
	if len(chunks) != 1 {
		t.Fatalf("expected 1 empty chunk, got %d", len(chunks))
	}
}

func TestJupyterLargeCell(t *testing.T) {
	// Create a code cell larger than maxChunkSize.
	largeCode := strings.Repeat("x = 1\n", 100)
	notebook := `{
  "cells": [
    {"cell_type": "code", "source": ["` + strings.ReplaceAll(largeCode, "\n", "\\n") + `"]}
  ]
}`
	ext := NewJupyterExtractor(100)
	chunks, err := ext.Extract([]byte(notebook), ExtractOptions{Path: "big.ipynb"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for large cell, got %d", len(chunks))
	}

	// Split chunks should have "part" metadata.
	for _, ch := range chunks {
		if ch.Metadata["part"] == "" {
			t.Error("split chunk missing part metadata")
		}
	}
}

func TestJupyterMergeSmallCells(t *testing.T) {
	// Several small markdown cells should be merged.
	notebook := `{
  "cells": [
    {"cell_type": "markdown", "source": ["Line 1"]},
    {"cell_type": "markdown", "source": ["Line 2"]},
    {"cell_type": "markdown", "source": ["Line 3"]},
    {"cell_type": "code", "source": ["x = 1"]}
  ]
}`
	ext := NewJupyterExtractor(2000)
	chunks, err := ext.Extract([]byte(notebook), ExtractOptions{Path: "small.ipynb"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The 3 small markdown cells should be merged into 1, plus 1 code cell = 2.
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (merged markdown + code), got %d", len(chunks))
	}

	// The merged chunk should contain all three lines.
	found := false
	for _, ch := range chunks {
		if ch.Metadata["cell_type"] == "markdown" {
			found = true
			if !strings.Contains(ch.Content, "Line 1") || !strings.Contains(ch.Content, "Line 3") {
				t.Errorf("merged chunk should contain all lines, got: %s", ch.Content)
			}
		}
	}
	if !found {
		t.Error("expected a merged markdown chunk")
	}
}

func TestJupyterInvalidJSON(t *testing.T) {
	ext := NewJupyterExtractor(2000)
	_, err := ext.Extract([]byte("not json"), ExtractOptions{Path: "bad.ipynb"})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
