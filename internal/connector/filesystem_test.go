package connector

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestScanFindsFiles(t *testing.T) {
	dir := t.TempDir()

	// Create some test files.
	writeFile(t, dir, "readme.md", "# Hello")
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, filepath.Join(dir, "sub"), "data.txt", "some data")

	c := NewFilesystemConnector(dir)
	docs, deleted, err := c.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(deleted) != 0 {
		t.Errorf("expected no deleted paths, got %d", len(deleted))
	}

	if len(docs) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(docs))
	}

	// Verify fields on one document.
	found := false
	for _, d := range docs {
		if filepath.Base(d.Path) == "main.go" {
			found = true
			if d.SourceType != "filesystem" {
				t.Errorf("expected SourceType 'filesystem', got %q", d.SourceType)
			}
			if d.FileType != "go" {
				t.Errorf("expected FileType 'go', got %q", d.FileType)
			}
			if d.Checksum == "" {
				t.Error("expected non-empty checksum")
			}
			if string(d.Content) != "package main" {
				t.Errorf("unexpected content: %q", string(d.Content))
			}
			expectedURI := "file://" + filepath.Join(dir, "main.go")
			if d.SourceURI != expectedURI {
				t.Errorf("expected SourceURI %q, got %q", expectedURI, d.SourceURI)
			}
		}
	}
	if !found {
		t.Error("main.go not found in scan results")
	}
}

func TestScanSkipsHiddenAndNodeModules(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "visible.txt", "visible")
	writeFile(t, filepath.Join(dir, ".hidden"), "secret.txt", "hidden")
	writeFile(t, filepath.Join(dir, "node_modules", "pkg"), "index.js", "module")
	writeFile(t, filepath.Join(dir, "__pycache__"), "cache.pyc", "bytecode")
	writeFile(t, dir, ".dotfile", "dotfile content")

	c := NewFilesystemConnector(dir)
	docs, _, err := c.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(docs) != 1 {
		names := make([]string, len(docs))
		for i, d := range docs {
			names[i] = d.Path
		}
		t.Fatalf("expected 1 document (visible.txt), got %d: %v", len(docs), names)
	}

	if filepath.Base(docs[0].Path) != "visible.txt" {
		t.Errorf("expected visible.txt, got %s", docs[0].Path)
	}
}

func TestScanSkipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "code.go", "package main")
	writeFile(t, dir, "image.png", "fake png data")
	writeFile(t, dir, "archive.zip", "fake zip data")

	c := NewFilesystemConnector(dir)
	docs, _, err := c.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if filepath.Base(docs[0].Path) != "code.go" {
		t.Errorf("expected code.go, got %s", docs[0].Path)
	}
}

func TestScanIncrementalSkipsUnchanged(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "a.txt", "content a")
	writeFile(t, dir, "b.txt", "content b")

	c := NewFilesystemConnector(dir)

	// First scan: get all documents.
	docs, _, err := c.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("first scan failed: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}

	// Build known map from first scan.
	known := make(map[string]string)
	for _, d := range docs {
		known[d.Path] = d.Checksum
	}

	// Second scan with known checksums — nothing changed.
	docs2, deleted, err := c.Scan(context.Background(), known)
	if err != nil {
		t.Fatalf("second scan failed: %v", err)
	}
	if len(docs2) != 0 {
		t.Errorf("expected 0 changed documents, got %d", len(docs2))
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deleted paths, got %d", len(deleted))
	}

	// Modify one file and scan again.
	writeFile(t, dir, "a.txt", "content a modified")
	docs3, deleted, err := c.Scan(context.Background(), known)
	if err != nil {
		t.Fatalf("third scan failed: %v", err)
	}
	if len(docs3) != 1 {
		t.Fatalf("expected 1 changed document, got %d", len(docs3))
	}
	if filepath.Base(docs3[0].Path) != "a.txt" {
		t.Errorf("expected a.txt changed, got %s", docs3[0].Path)
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deleted paths, got %d", len(deleted))
	}
}

func TestScanDetectsDeletedFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "keep.txt", "keep")
	writeFile(t, dir, "remove.txt", "remove")

	c := NewFilesystemConnector(dir)

	// First scan.
	docs, _, err := c.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("first scan failed: %v", err)
	}

	known := make(map[string]string)
	for _, d := range docs {
		known[d.Path] = d.Checksum
	}

	// Delete one file.
	removePath := filepath.Join(dir, "remove.txt")
	if err := os.Remove(removePath); err != nil {
		t.Fatalf("removing file: %v", err)
	}

	// Second scan should report the deleted file.
	_, deleted, err := c.Scan(context.Background(), known)
	if err != nil {
		t.Fatalf("second scan failed: %v", err)
	}

	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted path, got %d", len(deleted))
	}
	if deleted[0] != removePath {
		t.Errorf("expected deleted path %q, got %q", removePath, deleted[0])
	}
}

func TestScanSkipsLargeFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "small.txt", "small")

	// Create a file larger than 1MB.
	largePath := filepath.Join(dir, "large.txt")
	f, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, maxFileSize+1)
	if _, err := f.Write(buf); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	c := NewFilesystemConnector(dir)
	docs, _, err := c.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("expected 1 document (small only), got %d", len(docs))
	}
}

func TestScanChecksumCorrectness(t *testing.T) {
	dir := t.TempDir()

	content := "hello world"
	writeFile(t, dir, "test.txt", content)

	c := NewFilesystemConnector(dir)
	docs, _, err := c.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	if docs[0].Checksum != expected {
		t.Errorf("checksum mismatch: got %q, want %q", docs[0].Checksum, expected)
	}
}

func TestName(t *testing.T) {
	c := NewFilesystemConnector("/tmp")
	if c.Name() != "filesystem" {
		t.Errorf("expected 'filesystem', got %q", c.Name())
	}
}

func TestConnectorInterface(t *testing.T) {
	// Compile-time check that FilesystemConnector satisfies the Connector interface.
	var _ Connector = (*FilesystemConnector)(nil)
}

// writeFile creates parent directories and writes content to a file.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
