package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/model"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := NewSQLiteStore(dbPath, 4)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewSQLiteStore(t *testing.T) {
	s := newTestStore(t)
	if s.embeddingDim != 4 {
		t.Errorf("expected dim 4, got %d", s.embeddingDim)
	}
}

func TestMetadataValidation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// First open sets metadata.
	s1, err := NewSQLiteStore(dbPath, 4)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s1.Close()

	// Second open with same dim succeeds.
	s2, err := NewSQLiteStore(dbPath, 4)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()

	// Third open with different dim fails.
	_, err = NewSQLiteStore(dbPath, 8)
	if err == nil {
		t.Fatal("expected error for mismatched dimension")
	}
}

func TestUpsertAndGetFragments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	frags := []model.SourceFragment{
		{
			ID: "f1", Content: "hello world", SourceType: "filesystem",
			SourcePath: "/a/b.txt", SourceURI: "file:///a/b.txt",
			LastModified: now, Author: "alice", FileType: "txt",
			Checksum: "abc123", Embedding: []float32{1, 0, 0, 0},
		},
		{
			ID: "f2", Content: "goodbye world", SourceType: "filesystem",
			SourcePath: "/a/c.txt", SourceURI: "file:///a/c.txt",
			LastModified: now, Author: "bob", FileType: "txt",
			Checksum: "def456", Embedding: []float32{0, 1, 0, 0},
		},
	}

	if err := s.UpsertFragments(ctx, frags); err != nil {
		t.Fatalf("UpsertFragments: %v", err)
	}

	got, err := s.GetFragments(ctx, []string{"f1", "f2"})
	if err != nil {
		t.Fatalf("GetFragments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 fragments, got %d", len(got))
	}

	// Verify content was stored.
	byID := map[string]model.SourceFragment{}
	for _, f := range got {
		byID[f.ID] = f
	}
	if byID["f1"].Content != "hello world" {
		t.Errorf("f1 content = %q", byID["f1"].Content)
	}
	if byID["f2"].Author != "bob" {
		t.Errorf("f2 author = %q", byID["f2"].Author)
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	frag := model.SourceFragment{
		ID: "f1", Content: "v1", SourceType: "filesystem",
		SourcePath: "/a.txt", SourceURI: "file:///a.txt",
		LastModified: now, FileType: "txt", Checksum: "c1",
		Embedding: []float32{1, 0, 0, 0},
	}

	if err := s.UpsertFragments(ctx, []model.SourceFragment{frag}); err != nil {
		t.Fatal(err)
	}

	frag.Content = "v2"
	frag.Checksum = "c2"
	if err := s.UpsertFragments(ctx, []model.SourceFragment{frag}); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetFragments(ctx, []string{"f1"})
	if len(got) != 1 || got[0].Content != "v2" {
		t.Errorf("expected updated content v2, got %v", got)
	}
}

func TestSearchByVector(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	frags := []model.SourceFragment{
		{
			ID: "f1", Content: "one", SourceType: "fs",
			SourcePath: "/1", SourceURI: "f:///1",
			LastModified: now, FileType: "txt", Checksum: "a",
			Embedding: []float32{1, 0, 0, 0},
		},
		{
			ID: "f2", Content: "two", SourceType: "fs",
			SourcePath: "/2", SourceURI: "f:///2",
			LastModified: now, FileType: "txt", Checksum: "b",
			Embedding: []float32{0, 1, 0, 0},
		},
		{
			ID: "f3", Content: "three", SourceType: "fs",
			SourcePath: "/3", SourceURI: "f:///3",
			LastModified: now, FileType: "txt", Checksum: "c",
			Embedding: []float32{0, 0, 1, 0},
		},
	}

	if err := s.UpsertFragments(ctx, frags); err != nil {
		t.Fatal(err)
	}

	// Search near f1's embedding.
	results, err := s.SearchByVector(ctx, []float32{0.9, 0.1, 0, 0}, 2)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// f1 should be closest.
	if results[0].ID != "f1" {
		t.Errorf("expected f1 first, got %s", results[0].ID)
	}
}

func TestGetChecksums(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	frags := []model.SourceFragment{
		{
			ID: "f1", Content: "a", SourceType: "filesystem",
			SourcePath: "/a.txt", SourceURI: "f:///a", LastModified: now,
			FileType: "txt", Checksum: "aaa",
		},
		{
			ID: "f2", Content: "b", SourceType: "github",
			SourcePath: "/b.txt", SourceURI: "f:///b", LastModified: now,
			FileType: "txt", Checksum: "bbb",
		},
	}

	if err := s.UpsertFragments(ctx, frags); err != nil {
		t.Fatal(err)
	}

	cs, err := s.GetChecksums(ctx, "filesystem", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs["/a.txt"] != "aaa" {
		t.Errorf("unexpected checksums: %v", cs)
	}
}

func TestDeleteByPaths(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	frags := []model.SourceFragment{
		{
			ID: "f1", Content: "a", SourceType: "filesystem",
			SourcePath: "/a.txt", SourceURI: "f:///a", LastModified: now,
			FileType: "txt", Checksum: "aaa", Embedding: []float32{1, 0, 0, 0},
		},
		{
			ID: "f2", Content: "b", SourceType: "filesystem",
			SourcePath: "/b.txt", SourceURI: "f:///b", LastModified: now,
			FileType: "txt", Checksum: "bbb", Embedding: []float32{0, 1, 0, 0},
		},
	}

	if err := s.UpsertFragments(ctx, frags); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteByPaths(ctx, "filesystem", "", []string{"/a.txt"}); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetFragments(ctx, []string{"f1", "f2"})
	if len(got) != 1 || got[0].ID != "f2" {
		t.Errorf("expected only f2 remaining, got %v", got)
	}
}

func TestGetFragmentsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetFragments(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestDeleteByPathsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Should be a no-op, not an error.
	if err := s.DeleteByPaths(ctx, "filesystem", "", nil); err != nil {
		t.Fatal(err)
	}
}

// Ensure the interface is satisfied at compile time.
var _ Store = (*SQLiteStore)(nil)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
