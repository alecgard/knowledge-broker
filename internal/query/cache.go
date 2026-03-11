package query

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/store"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// DiskStore is the optional persistence backend for the query cache.
type DiskStore interface {
	GetCachedAnswer(ctx context.Context, cacheKey string, maxAge time.Duration) (*store.CachedAnswer, error)
	PutCachedAnswer(ctx context.Context, cacheKey, queryText string, concise bool, fragmentSigs string, answer []byte) error
	PruneCacheEntries(ctx context.Context, maxAge time.Duration) error
}

type cacheEntry struct {
	answer       *model.Answer
	fragmentSigs string // hash of fragment IDs+checksums used to produce this answer
	createdAt    time.Time
}

// fastCacheEntry is a lightweight cache entry for the fast-path cache.
// It stores an answer without fragment validation, using a shorter TTL.
type fastCacheEntry struct {
	answer    *model.Answer
	createdAt time.Time
}

// Cache stores exact-match query results keyed by query text + concise flag.
// Entries are invalidated when the underlying fragments change.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	maxAge  time.Duration
	maxSize int

	fastEntries map[string]fastCacheEntry
	fastMaxAge  time.Duration

	disk     DiskStore
	putCount atomic.Int64
	logger   *slog.Logger
}

// NewCache creates a query cache. maxAge controls TTL, maxSize caps entries.
// An optional DiskStore enables write-through to a persistent backend.
func NewCache(maxAge time.Duration, maxSize int, disk ...DiskStore) *Cache {
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	if maxSize <= 0 {
		maxSize = 256
	}
	c := &Cache{
		entries:     make(map[string]cacheEntry, maxSize),
		maxAge:      maxAge,
		maxSize:     maxSize,
		fastEntries: make(map[string]fastCacheEntry, maxSize),
		fastMaxAge:  2 * time.Minute,
	}
	if len(disk) > 0 && disk[0] != nil {
		c.disk = disk[0]
	}
	return c
}

// SetLogger sets the logger for disk cache debug messages.
func (c *Cache) SetLogger(l *slog.Logger) {
	c.logger = l
}

// cacheKey builds a key from the query text and concise flag.
func cacheKey(query string, concise bool) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%v", query, concise)))
	return fmt.Sprintf("%x", h[:16])
}

// fragmentSig builds a signature from the fragment IDs and checksums.
// If the fragments returned by vector search change, the cache entry is stale.
func fragmentSig(fragments []model.SourceFragment) string {
	h := sha256.New()
	for _, f := range fragments {
		fmt.Fprintf(h, "%s:%s\n", f.ID, f.Checksum)
	}
	return fmt.Sprintf("%x", h.Sum(nil)[:16])
}

// Get returns a cached answer if one exists for this query and the underlying
// fragments haven't changed. Returns nil on miss.
// When a disk backend is configured and ctx is non-nil, it checks disk on
// in-memory miss and promotes hits to the memory cache.
func (c *Cache) Get(query string, concise bool, currentFragments []model.SourceFragment, ctx ...context.Context) *model.Answer {
	key := cacheKey(query, concise)

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if ok {
		// Check TTL.
		if time.Since(entry.createdAt) > c.maxAge {
			ok = false
		} else if entry.fragmentSigs != fragmentSig(currentFragments) {
			ok = false
		}
	}

	if ok {
		return entry.answer
	}

	// On in-memory miss, try disk if available.
	if c.disk != nil && len(ctx) > 0 && ctx[0] != nil {
		return c.getFromDisk(ctx[0], key, query, concise, currentFragments)
	}

	return nil
}

// getFromDisk checks the disk cache and promotes hits to in-memory.
func (c *Cache) getFromDisk(ctx context.Context, key string, queryText string, concise bool, currentFragments []model.SourceFragment) *model.Answer {
	cached, err := c.disk.GetCachedAnswer(ctx, key, c.maxAge)
	if err != nil {
		if c.logger != nil {
			c.logger.LogAttrs(ctx, slog.LevelDebug, "disk cache error", slog.String("error", err.Error()))
		}
		return nil
	}
	if cached == nil {
		return nil
	}

	// Validate fragment signatures.
	currentSigs := fragmentSig(currentFragments)
	if cached.FragmentSigs != currentSigs {
		if c.logger != nil {
			c.logger.LogAttrs(ctx, slog.LevelDebug, "disk cache miss", slog.String("reason", "fragments changed"))
		}
		return nil
	}

	// Deserialize the answer.
	var answer model.Answer
	if err := json.Unmarshal(cached.AnswerJSON, &answer); err != nil {
		if c.logger != nil {
			c.logger.LogAttrs(ctx, slog.LevelDebug, "disk cache unmarshal error", slog.String("error", err.Error()))
		}
		return nil
	}

	if c.logger != nil {
		c.logger.LogAttrs(ctx, slog.LevelDebug, "disk cache hit",
			slog.String("query", queryText),
			slog.Duration("age", time.Since(cached.CreatedAt)),
		)
	}

	// Promote to in-memory cache.
	c.mu.Lock()
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}
	c.entries[key] = cacheEntry{
		answer:       &answer,
		fragmentSigs: cached.FragmentSigs,
		createdAt:    cached.CreatedAt,
	}
	c.mu.Unlock()

	return &answer
}

// Put stores an answer in the cache. If a disk backend is configured,
// the entry is also written through to disk.
func (c *Cache) Put(query string, concise bool, fragments []model.SourceFragment, answer *model.Answer, ctx ...context.Context) {
	c.mu.Lock()

	// Evict oldest entries if at capacity.
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	key := cacheKey(query, concise)
	sigs := fragmentSig(fragments)
	c.entries[key] = cacheEntry{
		answer:       answer,
		fragmentSigs: sigs,
		createdAt:    time.Now(),
	}
	c.mu.Unlock()

	// Write-through to disk.
	if c.disk != nil && len(ctx) > 0 && ctx[0] != nil {
		answerJSON, err := json.Marshal(answer)
		if err == nil {
			_ = c.disk.PutCachedAnswer(ctx[0], key, query, concise, sigs, answerJSON)
		}

		// Opportunistic prune: every 10th Put, clean entries older than 1 hour.
		if c.putCount.Add(1)%10 == 0 {
			go func() {
				_ = c.disk.PruneCacheEntries(ctx[0], 1*time.Hour)
			}()
		}
	}
}

// GetFastPath returns a cached answer if one exists for this exact query text
// and hasn't exceeded the fast-path TTL. This skips fragment validation --
// use a shorter TTL than the main cache.
func (c *Cache) GetFastPath(query string, concise bool) *model.Answer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(query, concise)
	entry, ok := c.fastEntries[key]
	if !ok {
		return nil
	}
	if time.Since(entry.createdAt) > c.fastMaxAge {
		return nil
	}
	return entry.answer
}

// PutFastPath stores an answer in the fast-path cache.
func (c *Cache) PutFastPath(query string, concise bool, answer *model.Answer) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.fastEntries) >= c.maxSize {
		c.evictOldestFast()
	}

	key := cacheKey(query, concise)
	c.fastEntries[key] = fastCacheEntry{
		answer:    answer,
		createdAt: time.Now(),
	}
}

// evictOldestFrom removes the oldest entry (by createdAt) from a map.
// getTime extracts the creation time from a value. Must be called with lock held.
func evictOldestFrom[V any](m map[string]V, getTime func(V) time.Time) {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range m {
		t := getTime(v)
		if oldestKey == "" || t.Before(oldestTime) {
			oldestKey = k
			oldestTime = t
		}
	}
	if oldestKey != "" {
		delete(m, oldestKey)
	}
}

func (c *Cache) evictOldestFast() {
	evictOldestFrom(c.fastEntries, func(e fastCacheEntry) time.Time { return e.createdAt })
}

func (c *Cache) evictOldest() {
	evictOldestFrom(c.entries, func(e cacheEntry) time.Time { return e.createdAt })
}

// Clear removes all cached entries.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]cacheEntry, c.maxSize)
	c.fastEntries = make(map[string]fastCacheEntry, c.maxSize)
}
