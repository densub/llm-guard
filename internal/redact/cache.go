package redact

import (
	"crypto/sha256"
	"sync"
)

// DetectionCache stores redaction results keyed by input string content hash.
// It is safe for concurrent use and evicts least-recently-used entries when
// full.
type DetectionCache struct {
	mu         sync.Mutex
	maxEntries int
	entries    map[cacheKey]cacheEntry
	order      []cacheKey
}

type cacheKey struct {
	hash    [32]byte
	skipLLM bool
}

type cacheEntry struct {
	redacted   string
	categories []string
}

// NewDetectionCache returns a cache that holds up to maxEntries results.
// Pass maxEntries <= 0 to disable caching (returns nil).
func NewDetectionCache(maxEntries int) *DetectionCache {
	if maxEntries <= 0 {
		return nil
	}
	return &DetectionCache{
		maxEntries: maxEntries,
		entries:    make(map[cacheKey]cacheEntry, maxEntries),
		order:      make([]cacheKey, 0, maxEntries),
	}
}

// Get returns a cached redaction result for the given content hash and LLM
// skip flag. The second return value is false on miss.
func (c *DetectionCache) Get(hash [32]byte, skipLLM bool) (redacted string, categories []string, ok bool) {
	if c == nil {
		return "", nil, false
	}
	key := cacheKey{hash: hash, skipLLM: skipLLM}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return "", nil, false
	}
	c.touchLocked(key)
	return entry.redacted, append([]string(nil), entry.categories...), true
}

// Put stores a redaction result.
func (c *DetectionCache) Put(hash [32]byte, skipLLM bool, redacted string, categories []string) {
	if c == nil {
		return
	}
	key := cacheKey{hash: hash, skipLLM: skipLLM}
	cats := append([]string(nil), categories...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		if len(c.order) >= c.maxEntries {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
		c.order = append(c.order, key)
	} else {
		c.touchLocked(key)
	}
	c.entries[key] = cacheEntry{redacted: redacted, categories: cats}
}

func (c *DetectionCache) touchLocked(key cacheKey) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}

func contentHash(s string) [32]byte {
	return sha256.Sum256([]byte(s))
}
