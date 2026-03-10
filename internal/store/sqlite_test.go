package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
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

func TestSearchByVectorFiltered(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	frags := []model.SourceFragment{
		{
			ID: "f1", Content: "alpha", SourceType: "git", SourceName: "org/repo-a",
			SourcePath: "/1", SourceURI: "g:///1",
			LastModified: now, FileType: "txt", Checksum: "a",
			Embedding: []float32{1, 0, 0, 0},
		},
		{
			ID: "f2", Content: "beta", SourceType: "git", SourceName: "org/repo-b",
			SourcePath: "/2", SourceURI: "g:///2",
			LastModified: now, FileType: "txt", Checksum: "b",
			Embedding: []float32{0.9, 0.1, 0, 0},
		},
		{
			ID: "f3", Content: "gamma", SourceType: "git", SourceName: "org/repo-c",
			SourcePath: "/3", SourceURI: "g:///3",
			LastModified: now, FileType: "txt", Checksum: "c",
			Embedding: []float32{0.8, 0.2, 0, 0},
		},
	}

	if err := s.UpsertFragments(ctx, frags); err != nil {
		t.Fatal(err)
	}

	// Filter to repo-b only — should return only f2.
	results, err := s.SearchByVectorFiltered(ctx, []float32{1, 0, 0, 0}, 10, []string{"org/repo-b"})
	if err != nil {
		t.Fatalf("SearchByVectorFiltered: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "f2" {
		t.Errorf("expected f2, got %s", results[0].ID)
	}

	// Filter to repo-a and repo-c — should return f1 and f3.
	results, err = s.SearchByVectorFiltered(ctx, []float32{1, 0, 0, 0}, 10, []string{"org/repo-a", "org/repo-c"})
	if err != nil {
		t.Fatalf("SearchByVectorFiltered: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// f1 should be closest (embedding [1,0,0,0]).
	if results[0].ID != "f1" {
		t.Errorf("expected f1 first, got %s", results[0].ID)
	}

	// Empty source names — should delegate to SearchByVector (return all 3).
	results, err = s.SearchByVectorFiltered(ctx, []float32{1, 0, 0, 0}, 10, nil)
	if err != nil {
		t.Fatalf("SearchByVectorFiltered empty: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results with no filter, got %d", len(results))
	}

	// Filter to non-existent source — should return empty.
	results, err = s.SearchByVectorFiltered(ctx, []float32{1, 0, 0, 0}, 10, []string{"no/such-repo"})
	if err != nil {
		t.Fatalf("SearchByVectorFiltered no match: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for unknown source, got %d", len(results))
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

func TestDeleteFragmentsBySource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	frags := []model.SourceFragment{
		{
			ID: "f1", Content: "a", SourceType: "filesystem", SourceName: "proj1",
			SourcePath: "/a.txt", SourceURI: "f:///a", LastModified: now,
			FileType: "txt", Checksum: "aaa", Embedding: []float32{1, 0, 0, 0},
		},
		{
			ID: "f2", Content: "b", SourceType: "filesystem", SourceName: "proj1",
			SourcePath: "/b.txt", SourceURI: "f:///b", LastModified: now,
			FileType: "txt", Checksum: "bbb", Embedding: []float32{0, 1, 0, 0},
		},
		{
			ID: "f3", Content: "c", SourceType: "github", SourceName: "repo1",
			SourcePath: "/c.txt", SourceURI: "f:///c", LastModified: now,
			FileType: "txt", Checksum: "ccc", Embedding: []float32{0, 0, 1, 0},
		},
	}

	if err := s.UpsertFragments(ctx, frags); err != nil {
		t.Fatal(err)
	}

	// Delete all fragments for filesystem/proj1.
	if err := s.DeleteFragmentsBySource(ctx, "filesystem", "proj1"); err != nil {
		t.Fatal(err)
	}

	// f1 and f2 should be gone, f3 should remain.
	got, err := s.GetFragments(ctx, []string{"f1", "f2", "f3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "f3" {
		t.Errorf("expected only f3 remaining, got %v", got)
	}

	// Embeddings for f3 should still work in search.
	results, err := s.SearchByVector(ctx, []float32{0, 0, 1, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "f3" {
		t.Errorf("expected f3 in search results, got %v", results)
	}
}

func TestDeleteSource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	src := model.Source{
		SourceType: "filesystem",
		SourceName: "proj1",
		Config:     map[string]string{"path": "/tmp"},
		LastIngest: time.Now().UTC().Truncate(time.Second),
	}

	if err := s.RegisterSource(ctx, src); err != nil {
		t.Fatal(err)
	}

	// Verify it exists.
	sources, _ := s.ListSources(ctx)
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}

	// Delete it.
	if err := s.DeleteSource(ctx, "filesystem", "proj1"); err != nil {
		t.Fatal(err)
	}

	// Should be gone.
	sources, _ = s.ListSources(ctx)
	if len(sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(sources))
	}
}

func TestConcurrentUpserts(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "concurrent.db")

	const numWorkers = 8
	const fragsPerWorker = 10

	now := time.Now().UTC().Truncate(time.Second)
	errs := make(chan error, numWorkers)

	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			s, err := NewSQLiteStore(dbPath, 4)
			if err != nil {
				errs <- fmt.Errorf("worker %d open: %w", workerID, err)
				return
			}
			defer s.Close()

			var frags []model.SourceFragment
			for i := 0; i < fragsPerWorker; i++ {
				id := fmt.Sprintf("w%d-f%d", workerID, i)
				frags = append(frags, model.SourceFragment{
					ID: id, Content: fmt.Sprintf("content-%s", id),
					SourceType: "test", SourcePath: fmt.Sprintf("/w%d/%d.txt", workerID, i),
					SourceURI: "test://", LastModified: now,
					FileType: "txt", Checksum: fmt.Sprintf("ck-%s", id),
					Embedding: []float32{float32(workerID), float32(i), 0, 0},
				})
			}
			errs <- s.UpsertFragments(context.Background(), frags)
		}(w)
	}

	for i := 0; i < numWorkers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent upsert error: %v", err)
		}
	}

	// Verify all fragments were stored.
	s, err := NewSQLiteStore(dbPath, 4)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	counts, err := s.CountFragmentsBySource(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	want := numWorkers * fragsPerWorker
	if total != want {
		t.Errorf("expected %d total fragments, got %d", want, total)
	}
}

func TestConcurrentUpsertSameID(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dedup.db")

	now := time.Now().UTC().Truncate(time.Second)
	const numWorkers = 8
	errs := make(chan error, numWorkers)

	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			s, err := NewSQLiteStore(dbPath, 4)
			if err != nil {
				errs <- fmt.Errorf("worker %d open: %w", workerID, err)
				return
			}
			defer s.Close()

			frag := model.SourceFragment{
				ID: "shared-id", Content: fmt.Sprintf("version-%d", workerID),
				SourceType: "test", SourcePath: "/shared.txt",
				SourceURI: "test://", LastModified: now,
				FileType: "txt", Checksum: fmt.Sprintf("v%d", workerID),
				Embedding: []float32{1, 0, 0, 0},
			}
			errs <- s.UpsertFragments(context.Background(), []model.SourceFragment{frag})
		}(w)
	}

	for i := 0; i < numWorkers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent upsert same ID error: %v", err)
		}
	}

	// Verify exactly one fragment exists with that ID.
	s, err := NewSQLiteStore(dbPath, 4)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	got, err := s.GetFragments(context.Background(), []string{"shared-id"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 fragment, got %d", len(got))
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rw.db")

	now := time.Now().UTC().Truncate(time.Second)

	// Seed some initial data.
	s, err := NewSQLiteStore(dbPath, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("seed-%d", i)
		err := s.UpsertFragments(context.Background(), []model.SourceFragment{{
			ID: id, Content: "seed", SourceType: "test", SourcePath: fmt.Sprintf("/%d.txt", i),
			SourceURI: "test://", LastModified: now, FileType: "txt", Checksum: "s",
			Embedding: []float32{1, 0, 0, 0},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	const numWorkers = 8
	errs := make(chan error, numWorkers*2)

	// Writers.
	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			ws, err := NewSQLiteStore(dbPath, 4)
			if err != nil {
				errs <- err
				return
			}
			defer ws.Close()

			id := fmt.Sprintf("new-%d", workerID)
			errs <- ws.UpsertFragments(context.Background(), []model.SourceFragment{{
				ID: id, Content: "new", SourceType: "test",
				SourcePath: fmt.Sprintf("/new/%d.txt", workerID),
				SourceURI: "test://", LastModified: now, FileType: "txt", Checksum: "n",
				Embedding: []float32{0, 1, 0, 0},
			}})
		}(w)
	}

	// Readers.
	for r := 0; r < numWorkers; r++ {
		go func() {
			rs, err := NewSQLiteStore(dbPath, 4)
			if err != nil {
				errs <- err
				return
			}
			defer rs.Close()

			_, err = rs.SearchByVector(context.Background(), []float32{1, 0, 0, 0}, 5)
			errs <- err
		}()
	}

	for i := 0; i < numWorkers*2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent read/write error: %v", err)
		}
	}
}

// Ensure the interface is satisfied at compile time.
var _ Store = (*SQLiteStore)(nil)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
