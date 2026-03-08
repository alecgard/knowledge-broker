package query

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/model"
)

type cacheEntry struct {
	answer       *model.Answer
	fragmentSigs string // hash of fragment IDs+checksums used to produce this answer
	createdAt    time.Time
}

// Cache stores exact-match query results keyed by query text + concise flag.
// Entries are invalidated when the underlying fragments change.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	maxAge  time.Duration
	maxSize int
}

// NewCache creates a query cache. maxAge controls TTL, maxSize caps entries.
func NewCache(maxAge time.Duration, maxSize int) *Cache {
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	if maxSize <= 0 {
		maxSize = 256
	}
	return &Cache{
		entries: make(map[string]cacheEntry, maxSize),
		maxAge:  maxAge,
		maxSize: maxSize,
	}
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
func (c *Cache) Get(query string, concise bool, currentFragments []model.SourceFragment) *model.Answer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(query, concise)
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}

	// Check TTL.
	if time.Since(entry.createdAt) > c.maxAge {
		return nil
	}

	// Check that the fragments haven't changed.
	if entry.fragmentSigs != fragmentSig(currentFragments) {
		return nil
	}

	return entry.answer
}

// Put stores an answer in the cache.
func (c *Cache) Put(query string, concise bool, fragments []model.SourceFragment, answer *model.Answer) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest entries if at capacity.
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	key := cacheKey(query, concise)
	c.entries[key] = cacheEntry{
		answer:       answer,
		fragmentSigs: fragmentSig(fragments),
		createdAt:    time.Now(),
	}
}

// Clear removes all cached entries.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]cacheEntry, c.maxSize)
}

func (c *Cache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for k, e := range c.entries {
		if oldestKey == "" || e.createdAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.createdAt
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
