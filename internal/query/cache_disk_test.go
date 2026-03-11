package query

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/store"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// fakeDiskStore is a simple in-memory implementation of DiskStore for testing.
type fakeDiskStore struct {
	entries map[string]fakeDiskEntry
}

type fakeDiskEntry struct {
	queryText    string
	concise      bool
	fragmentSigs string
	answerJSON   []byte
	createdAt    time.Time
}

func newFakeDiskStore() *fakeDiskStore {
	return &fakeDiskStore{entries: make(map[string]fakeDiskEntry)}
}

func (f *fakeDiskStore) GetCachedAnswer(_ context.Context, cacheKey string, maxAge time.Duration) (*store.CachedAnswer, error) {
	e, ok := f.entries[cacheKey]
	if !ok {
		return nil, nil
	}
	if maxAge > 0 && time.Since(e.createdAt) > maxAge {
		return nil, nil
	}
	return &store.CachedAnswer{
		AnswerJSON:   e.answerJSON,
		FragmentSigs: e.fragmentSigs,
		CreatedAt:    e.createdAt,
	}, nil
}

func (f *fakeDiskStore) PutCachedAnswer(_ context.Context, cacheKey, queryText string, concise bool, fragmentSigs string, answer []byte) error {
	f.entries[cacheKey] = fakeDiskEntry{
		queryText:    queryText,
		concise:      concise,
		fragmentSigs: fragmentSigs,
		answerJSON:   answer,
		createdAt:    time.Now(),
	}
	return nil
}

func (f *fakeDiskStore) PruneCacheEntries(_ context.Context, maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)
	for k, e := range f.entries {
		if e.createdAt.Before(cutoff) {
			delete(f.entries, k)
		}
	}
	return nil
}

func TestCache_DiskWriteThrough(t *testing.T) {
	disk := newFakeDiskStore()
	c := NewCache(10*time.Minute, 256, disk)
	ctx := context.Background()

	fragments := []model.SourceFragment{
		{ID: "f1", Checksum: "abc"},
	}
	answer := &model.Answer{Content: "disk answer"}

	// Put should write to both memory and disk.
	c.Put("test query", true, fragments, answer, ctx)

	// Verify it's on disk.
	key := cacheKey("test query", true)
	if _, ok := disk.entries[key]; !ok {
		t.Fatal("expected entry on disk")
	}

	// Verify the JSON is valid.
	var decoded model.Answer
	if err := json.Unmarshal(disk.entries[key].answerJSON, &decoded); err != nil {
		t.Fatalf("unmarshal disk entry: %v", err)
	}
	if decoded.Content != "disk answer" {
		t.Errorf("disk content = %q, want %q", decoded.Content, "disk answer")
	}
}

func TestCache_DiskFallback(t *testing.T) {
	disk := newFakeDiskStore()
	ctx := context.Background()

	fragments := []model.SourceFragment{
		{ID: "f1", Checksum: "abc"},
	}
	answer := &model.Answer{Content: "persisted"}

	// Populate disk via one cache instance.
	c1 := NewCache(10*time.Minute, 256, disk)
	c1.Put("my query", false, fragments, answer, ctx)

	// Create a fresh cache (simulates new CLI process) with same disk.
	c2 := NewCache(10*time.Minute, 256, disk)

	// Memory miss, should fall back to disk.
	got := c2.Get("my query", false, fragments, ctx)
	if got == nil {
		t.Fatal("expected disk cache hit")
	}
	if got.Content != "persisted" {
		t.Errorf("got %q, want %q", got.Content, "persisted")
	}

	// After disk hit, entry should be promoted to memory.
	// Get without ctx should find it in memory now.
	got2 := c2.Get("my query", false, fragments)
	if got2 == nil {
		t.Fatal("expected memory cache hit after promotion")
	}
}

func TestCache_DiskMissOnFragmentChange(t *testing.T) {
	disk := newFakeDiskStore()
	ctx := context.Background()

	fragments := []model.SourceFragment{
		{ID: "f1", Checksum: "abc"},
	}
	answer := &model.Answer{Content: "old"}

	c1 := NewCache(10*time.Minute, 256, disk)
	c1.Put("query", true, fragments, answer, ctx)

	// New process with changed fragments.
	c2 := NewCache(10*time.Minute, 256, disk)
	changed := []model.SourceFragment{
		{ID: "f1", Checksum: "xyz"},
	}

	got := c2.Get("query", true, changed, ctx)
	if got != nil {
		t.Fatal("expected disk cache miss due to fragment change")
	}
}

func TestCache_NoDiskNoPanic(t *testing.T) {
	// Without disk, Get/Put with context should not panic.
	c := NewCache(10*time.Minute, 256)
	ctx := context.Background()

	fragments := []model.SourceFragment{{ID: "f1", Checksum: "a"}}
	c.Put("q", true, fragments, &model.Answer{Content: "x"}, ctx)
	got := c.Get("q", true, fragments, ctx)
	if got == nil {
		t.Fatal("expected memory hit")
	}
}
