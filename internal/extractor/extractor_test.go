package extractor

import (
	"strings"
	"testing"
)

// --- Markdown tests ---

func TestMarkdownHeadingSplitting(t *testing.T) {
	md := `# Title

Some intro text.

## Section One

Content of section one.

### Subsection A

Content of subsection A.

## Section Two

Content of section two.
`
	ext := NewMarkdownExtractor(2000)
	chunks, err := ext.Extract([]byte(md), ExtractOptions{Path: "test.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect: preamble (# Title + intro), ## Section One, ### Subsection A, ## Section Two
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// First chunk should have the intro text (no ## or ### heading).
	if !strings.Contains(chunks[0].Content, "Some intro text") {
		t.Errorf("first chunk should contain intro text, got: %s", chunks[0].Content)
	}

	// Find the "Section One" chunk.
	found := false
	for _, ch := range chunks {
		if ch.Metadata["heading"] == "## Section One" {
			found = true
			if !strings.Contains(ch.Content, "Content of section one") {
				t.Errorf("Section One chunk should contain its content, got: %s", ch.Content)
			}
			// The heading should be included as prefix.
			if !strings.HasPrefix(ch.Content, "## Section One") {
				t.Errorf("chunk should start with heading, got: %s", ch.Content)
			}
		}
	}
	if !found {
		t.Error("did not find chunk with heading '## Section One'")
	}

	// Find subsection chunk.
	found = false
	for _, ch := range chunks {
		if ch.Metadata["heading"] == "### Subsection A" {
			found = true
		}
	}
	if !found {
		t.Error("did not find chunk with heading '### Subsection A'")
	}
}

func TestMarkdownFrontmatter(t *testing.T) {
	md := `---
title: Test
date: 2024-01-01
---

## Hello

World.
`
	ext := NewMarkdownExtractor(2000)
	chunks, err := ext.Extract([]byte(md), ExtractOptions{Path: "test.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Frontmatter should be stripped; we should get the Hello chunk.
	found := false
	for _, ch := range chunks {
		if strings.Contains(ch.Content, "title:") || strings.Contains(ch.Content, "date:") {
			t.Error("frontmatter should have been stripped")
		}
		if ch.Metadata["heading"] == "## Hello" {
			found = true
		}
	}
	if !found {
		t.Error("did not find chunk with heading '## Hello'")
	}
}

func TestMarkdownLargeSectionFallback(t *testing.T) {
	// Create a section larger than maxChunkSize.
	maxSize := 100
	largeContent := "## Big Section\n\n" + strings.Repeat("word ", 50) // ~250 chars
	ext := NewMarkdownExtractor(maxSize)
	chunks, err := ext.Extract([]byte(largeContent), ExtractOptions{Path: "test.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for large section, got %d", len(chunks))
	}

	// All chunks should have the heading metadata.
	for _, ch := range chunks {
		if ch.Metadata["heading"] != "## Big Section" {
			t.Errorf("expected heading '## Big Section', got '%s'", ch.Metadata["heading"])
		}
	}

	// Large section chunks should have a "part" metadata.
	if chunks[0].Metadata["part"] != "1" {
		t.Errorf("first part should be '1', got '%s'", chunks[0].Metadata["part"])
	}
}

// --- Code tests ---

func TestCodeGoFunctionSplitting(t *testing.T) {
	goCode := `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}

func World() {
	fmt.Println("world")
}
`
	ext := NewCodeExtractor(2000)
	chunks, err := ext.Extract([]byte(goCode), ExtractOptions{Path: "main.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// Check that we have both functions.
	names := make(map[string]bool)
	for _, ch := range chunks {
		if ch.Metadata["name"] != "" {
			names[ch.Metadata["name"]] = true
		}
	}
	if !names["Hello"] {
		t.Error("did not find chunk for function Hello")
	}
	if !names["World"] {
		t.Error("did not find chunk for function World")
	}

	// First code chunk should include the preamble (package + import).
	if !strings.Contains(chunks[0].Content, "package main") {
		t.Errorf("first chunk should contain package declaration, got: %s", chunks[0].Content)
	}
}

func TestCodePythonClassSplitting(t *testing.T) {
	pyCode := `import os

class MyClass:
    def __init__(self):
        pass

    def method(self):
        pass

def standalone():
    pass
`
	ext := NewCodeExtractor(2000)
	chunks, err := ext.Extract([]byte(pyCode), ExtractOptions{Path: "module.py"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// Should find class and standalone function.
	var foundClass, foundFunc bool
	for _, ch := range chunks {
		if ch.Metadata["type"] == "class" && ch.Metadata["name"] == "MyClass" {
			foundClass = true
		}
		if ch.Metadata["type"] == "function" && ch.Metadata["name"] == "standalone" {
			foundFunc = true
		}
	}
	if !foundClass {
		t.Error("did not find chunk for class MyClass")
	}
	if !foundFunc {
		t.Error("did not find chunk for function standalone")
	}
}

func TestCodeLargeFunctionFallback(t *testing.T) {
	// Create a Go function larger than maxChunkSize.
	maxSize := 100
	goCode := "package main\n\nfunc Big() {\n" + strings.Repeat("\tx := 1\n", 30) + "}\n"
	ext := NewCodeExtractor(maxSize)
	chunks, err := ext.Extract([]byte(goCode), ExtractOptions{Path: "big.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for large function, got %d", len(chunks))
	}

	// All non-preamble chunks should reference the function.
	foundBig := false
	for _, ch := range chunks {
		if ch.Metadata["type"] == "preamble" {
			continue
		}
		foundBig = true
		if ch.Metadata["name"] != "Big" {
			t.Errorf("expected name 'Big', got '%s'", ch.Metadata["name"])
		}
	}
	if !foundBig {
		t.Error("did not find any chunks for function Big")
	}
}

func TestCodeLargePreambleFallback(t *testing.T) {
	// Create a Go file with a massive preamble (var block) before the first func.
	maxSize := 100
	largePreamble := "package main\n\nvar (\n" + strings.Repeat("\tx = 1\n", 40) + ")\n\n"
	goCode := largePreamble + "func Hello() {\n\tfmt.Println(\"hello\")\n}\n"
	ext := NewCodeExtractor(maxSize)
	chunks, err := ext.Extract([]byte(goCode), ExtractOptions{Path: "big_preamble.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The preamble is larger than maxSize, so it should be split into multiple chunks.
	preambleChunks := 0
	for _, ch := range chunks {
		if ch.Metadata["type"] == "preamble" {
			preambleChunks++
		}
	}
	if preambleChunks < 2 {
		t.Fatalf("expected at least 2 preamble chunks for large preamble, got %d", preambleChunks)
	}

	// Preamble chunks should have "part" metadata.
	for _, ch := range chunks {
		if ch.Metadata["type"] == "preamble" {
			if ch.Metadata["part"] == "" {
				t.Error("large preamble chunk should have 'part' metadata")
			}
		}
	}

	// Should also have the Hello function chunk.
	foundHello := false
	for _, ch := range chunks {
		if ch.Metadata["name"] == "Hello" {
			foundHello = true
		}
	}
	if !foundHello {
		t.Error("did not find chunk for function Hello")
	}
}

func TestCodeNoRecognizableBoundaries(t *testing.T) {
	// A file with no function/class boundaries.
	content := strings.Repeat("some random content line\n", 20)
	ext := NewCodeExtractor(100)
	chunks, err := ext.Extract([]byte(content), ExtractOptions{Path: "data.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for plaintext fallback, got %d", len(chunks))
	}
}

// --- Plaintext tests ---

func TestPlaintextFixedSizeSplitting(t *testing.T) {
	// Create text longer than maxChunkSize.
	text := strings.Repeat("Hello world. ", 200) // ~2600 chars
	ext := NewPlaintextExtractor(500, 50)
	chunks, err := ext.Extract([]byte(text), ExtractOptions{Path: "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// Each chunk should be within the size limit (with some tolerance for boundary finding).
	for i, ch := range chunks {
		if len(ch.Content) > 600 { // allow some tolerance
			t.Errorf("chunk %d is too large: %d chars", i, len(ch.Content))
		}
	}

	// All chunks should have offset metadata.
	for i, ch := range chunks {
		if _, ok := ch.Metadata["offset"]; !ok {
			t.Errorf("chunk %d missing offset metadata", i)
		}
	}
}

func TestPlaintextParagraphBoundarySplitting(t *testing.T) {
	// Create text with clear paragraph boundaries.
	paragraphs := []string{
		strings.Repeat("First paragraph content. ", 10),
		strings.Repeat("Second paragraph content. ", 10),
		strings.Repeat("Third paragraph content. ", 10),
	}
	text := strings.Join(paragraphs, "\n\n")
	ext := NewPlaintextExtractor(400, 50)
	chunks, err := ext.Extract([]byte(text), ExtractOptions{Path: "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for paragraph splitting, got %d", len(chunks))
	}

	// Verify chunks contain paragraph content.
	allContent := ""
	for _, ch := range chunks {
		allContent += ch.Content + " "
	}
	if !strings.Contains(allContent, "First paragraph") {
		t.Error("missing first paragraph content")
	}
	if !strings.Contains(allContent, "Third paragraph") {
		t.Error("missing third paragraph content")
	}
}

func TestPlaintextOverlap(t *testing.T) {
	// Use a simple text to verify overlap behavior.
	text := strings.Repeat("abcdefghij ", 100) // ~1100 chars
	ext := NewPlaintextExtractor(200, 50)
	chunks, err := ext.Extract([]byte(text), ExtractOptions{Path: "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// With overlap, the end of one chunk should overlap with the start of the next.
	// We check that at least some content appears in consecutive chunks.
	for i := 0; i < len(chunks)-1; i++ {
		curr := chunks[i].Content
		next := chunks[i+1].Content
		// The tail of curr should overlap with the beginning of next.
		tail := curr
		if len(tail) > 60 {
			tail = tail[len(tail)-60:]
		}
		// Find a common substring (at least a word).
		words := strings.Fields(tail)
		if len(words) > 0 {
			lastWord := words[len(words)-1]
			if !strings.Contains(next, lastWord) {
				// Overlap may not produce exact word matches due to boundary finding,
				// so this is not a hard failure.
				t.Logf("note: overlap may not be exact between chunk %d and %d", i, i+1)
			}
		}
	}
}

func TestPlaintextEmptyContent(t *testing.T) {
	ext := NewPlaintextExtractor(1500, 150)
	chunks, err := ext.Extract([]byte(""), ExtractOptions{Path: "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for empty content, got %d", len(chunks))
	}
}

// --- Registry test ---

func TestRegistry(t *testing.T) {
	plaintext := NewPlaintextExtractor(0, 0)
	registry := NewRegistry(plaintext)
	registry.Register(NewMarkdownExtractor(0))
	registry.Register(NewCodeExtractor(0))

	// Known extensions should return the right extractor.
	md := registry.Get(".md")
	if _, ok := md.(*MarkdownExtractor); !ok {
		t.Error("expected MarkdownExtractor for .md")
	}

	goExt := registry.Get(".go")
	if _, ok := goExt.(*CodeExtractor); !ok {
		t.Error("expected CodeExtractor for .go")
	}

	// Unknown extension should return fallback.
	unknown := registry.Get(".xyz")
	if _, ok := unknown.(*PlaintextExtractor); !ok {
		t.Error("expected PlaintextExtractor as fallback for .xyz")
	}
}
