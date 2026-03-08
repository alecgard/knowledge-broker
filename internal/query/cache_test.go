package query

import (
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/model"
)

func TestCache_HitAndMiss(t *testing.T) {
	c := NewCache(10*time.Minute, 256)

	fragments := []model.SourceFragment{
		{ID: "f1", Checksum: "abc"},
		{ID: "f2", Checksum: "def"},
	}
	answer := &model.Answer{Content: "cached answer"}

	// Miss before put.
	if got := c.Get("how does retry work?", true, fragments); got != nil {
		t.Fatal("expected cache miss")
	}

	// Put and hit.
	c.Put("how does retry work?", true, fragments, answer)
	if got := c.Get("how does retry work?", true, fragments); got == nil {
		t.Fatal("expected cache hit")
	} else if got.Content != "cached answer" {
		t.Fatalf("got %q, want %q", got.Content, "cached answer")
	}

	// Different query = miss.
	if got := c.Get("how does auth work?", true, fragments); got != nil {
		t.Fatal("expected cache miss for different query")
	}

	// Same query, different concise flag = miss.
	if got := c.Get("how does retry work?", false, fragments); got != nil {
		t.Fatal("expected cache miss for different concise flag")
	}
}

func TestCache_InvalidatedByFragmentChange(t *testing.T) {
	c := NewCache(10*time.Minute, 256)

	fragments := []model.SourceFragment{
		{ID: "f1", Checksum: "abc"},
	}
	answer := &model.Answer{Content: "cached"}

	c.Put("query", true, fragments, answer)

	// Same fragments = hit.
	if got := c.Get("query", true, fragments); got == nil {
		t.Fatal("expected cache hit")
	}

	// Changed checksum = miss (fragment was re-ingested).
	changed := []model.SourceFragment{
		{ID: "f1", Checksum: "xyz"},
	}
	if got := c.Get("query", true, changed); got != nil {
		t.Fatal("expected cache miss after fragment change")
	}

	// Different fragment set = miss.
	different := []model.SourceFragment{
		{ID: "f1", Checksum: "abc"},
		{ID: "f3", Checksum: "new"},
	}
	if got := c.Get("query", true, different); got != nil {
		t.Fatal("expected cache miss for different fragment set")
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	c := NewCache(1*time.Millisecond, 256)

	fragments := []model.SourceFragment{{ID: "f1", Checksum: "abc"}}
	c.Put("query", true, fragments, &model.Answer{Content: "old"})

	time.Sleep(5 * time.Millisecond)

	if got := c.Get("query", true, fragments); got != nil {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestCache_EvictsOldest(t *testing.T) {
	c := NewCache(10*time.Minute, 2) // max 2 entries

	f := []model.SourceFragment{{ID: "f1", Checksum: "a"}}

	c.Put("q1", true, f, &model.Answer{Content: "a1"})
	time.Sleep(time.Millisecond)
	c.Put("q2", true, f, &model.Answer{Content: "a2"})
	time.Sleep(time.Millisecond)
	c.Put("q3", true, f, &model.Answer{Content: "a3"}) // should evict q1

	if got := c.Get("q1", true, f); got != nil {
		t.Fatal("expected q1 to be evicted")
	}
	if got := c.Get("q2", true, f); got == nil {
		t.Fatal("expected q2 to still be cached")
	}
	if got := c.Get("q3", true, f); got == nil {
		t.Fatal("expected q3 to still be cached")
	}
}

func TestCache_Clear(t *testing.T) {
	c := NewCache(10*time.Minute, 256)
	f := []model.SourceFragment{{ID: "f1", Checksum: "a"}}

	c.Put("q1", true, f, &model.Answer{Content: "a1"})
	c.PutFastPath("q1", true, &model.Answer{Content: "fast a1"})
	c.Clear()

	if got := c.Get("q1", true, f); got != nil {
		t.Fatal("expected cache miss after clear")
	}
	if got := c.GetFastPath("q1", true); got != nil {
		t.Fatal("expected fast-path cache miss after clear")
	}
}

func TestFastPathCache_HitAndMiss(t *testing.T) {
	c := NewCache(10*time.Minute, 256)
	answer := &model.Answer{Content: "fast answer"}

	// Miss before put.
	if got := c.GetFastPath("how does retry work?", true); got != nil {
		t.Fatal("expected fast-path cache miss")
	}

	// Put and hit.
	c.PutFastPath("how does retry work?", true, answer)
	if got := c.GetFastPath("how does retry work?", true); got == nil {
		t.Fatal("expected fast-path cache hit")
	} else if got.Content != "fast answer" {
		t.Fatalf("got %q, want %q", got.Content, "fast answer")
	}

	// Different query = miss.
	if got := c.GetFastPath("how does auth work?", true); got != nil {
		t.Fatal("expected fast-path cache miss for different query")
	}

	// Same query, different concise flag = miss.
	if got := c.GetFastPath("how does retry work?", false); got != nil {
		t.Fatal("expected fast-path cache miss for different concise flag")
	}
}

func TestFastPathCache_TTLExpiry(t *testing.T) {
	c := NewCache(10*time.Minute, 256)
	// Override fast TTL to something tiny for testing.
	c.fastMaxAge = 1 * time.Millisecond

	c.PutFastPath("query", true, &model.Answer{Content: "old"})

	time.Sleep(5 * time.Millisecond)

	if got := c.GetFastPath("query", true); got != nil {
		t.Fatal("expected fast-path cache miss after TTL expiry")
	}
}

func TestFastPathCache_EvictsOldest(t *testing.T) {
	c := NewCache(10*time.Minute, 2) // max 2 entries

	c.PutFastPath("q1", true, &model.Answer{Content: "a1"})
	time.Sleep(time.Millisecond)
	c.PutFastPath("q2", true, &model.Answer{Content: "a2"})
	time.Sleep(time.Millisecond)
	c.PutFastPath("q3", true, &model.Answer{Content: "a3"}) // should evict q1

	if got := c.GetFastPath("q1", true); got != nil {
		t.Fatal("expected q1 to be evicted from fast-path cache")
	}
	if got := c.GetFastPath("q2", true); got == nil {
		t.Fatal("expected q2 to still be in fast-path cache")
	}
	if got := c.GetFastPath("q3", true); got == nil {
		t.Fatal("expected q3 to still be in fast-path cache")
	}
}
