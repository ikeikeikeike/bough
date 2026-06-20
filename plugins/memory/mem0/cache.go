package mem0

import (
	"sync"
	"time"

	memapi "github.com/ikeikeikeike/bough/plugins/memory/api"
)

// This file holds the Ν-1.1e read-through Query cache. The design
// follows round 4 AI #1 + #2 directly:
//
//   - Query-only: Store / Forget / Import never consult the cache.
//     Those paths invalidate every cached entry for the touched
//     scope so a subsequent Query sees the fresh upstream state.
//
//   - Short TTL (30 s): keeps mem0 updates from other bough
//     processes visible within one query round. Anything longer
//     would amplify the "two truths" hazard AI #1 flagged.
//
//   - LRU + max 512 entries: bounded memory cost so a long-running
//     bough session does not let the cache balloon. The LRU order
//     uses append + linear scan because 512 is small enough that
//     the constant factor of a simpler implementation beats a
//     hash-linked-list.

const (
	cacheTTL        = 30 * time.Second
	cacheMaxEntries = 512
)

// cacheKey identifies a cached Query response. Every parameter the
// host varies between two Query calls participates so two distinct
// queries never collide.
type cacheKey struct {
	scopeLevel    string
	worktreeID    string
	repoName      string
	term          string
	maxResults    int
	maxTokens     int
	minConfidence float64
}

// cacheEntry pairs the response with its absolute expiry so we can
// drop stale entries lazily on read.
type cacheEntry struct {
	resp   *memapi.QueryResp
	expiry time.Time
}

// queryCache is the bough mem0 plugin's bounded TTL + LRU cache.
// All methods are safe for concurrent use; the host sometimes
// drives Query / Store / Forget from different goroutines via the
// gRPC server.
type queryCache struct {
	mu      sync.Mutex
	entries map[cacheKey]*cacheEntry
	order   []cacheKey // append-only LRU; oldest at index 0
	clock   func() time.Time
}

// newQueryCache builds an empty cache with a real-clock now func.
// Tests inject a fake clock by setting clock directly.
func newQueryCache() *queryCache {
	return &queryCache{
		entries: make(map[cacheKey]*cacheEntry),
		clock:   func() time.Time { return time.Now() },
	}
}

// get returns a cached response if present and not expired. The
// LRU order is touched on every hit. Misses (= absent or expired)
// drop the entry so the caller can repopulate.
func (c *queryCache) get(k cacheKey) (*memapi.QueryResp, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok {
		return nil, false
	}
	if c.clock().After(e.expiry) {
		delete(c.entries, k)
		c.removeKeyLocked(k)
		return nil, false
	}
	// LRU touch.
	c.removeKeyLocked(k)
	c.order = append(c.order, k)
	return e.resp, true
}

// put inserts or refreshes an entry. Evicts the oldest when the
// cache exceeds cacheMaxEntries.
func (c *queryCache) put(k cacheKey, resp *memapi.QueryResp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[k]; exists {
		c.removeKeyLocked(k)
	}
	c.entries[k] = &cacheEntry{
		resp:   resp,
		expiry: c.clock().Add(cacheTTL),
	}
	c.order = append(c.order, k)
	for len(c.order) > cacheMaxEntries {
		victim := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, victim)
	}
}

// invalidateScope drops every cached entry whose key targets the
// given Scope. Round 4 AI #1 + #2: this runs from Store / Forget /
// Import after a successful upstream operation so a subsequent
// Query never returns a row mem0 no longer holds.
func (c *queryCache) invalidateScope(scope memapi.Scope) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) == 0 {
		return
	}
	remaining := make([]cacheKey, 0, len(c.order))
	for _, k := range c.order {
		if k.scopeLevel == scope.Level &&
			k.worktreeID == scope.WorktreeID &&
			k.repoName == scope.RepoName {
			delete(c.entries, k)
			continue
		}
		remaining = append(remaining, k)
	}
	c.order = remaining
}

// len is a test-only helper to assert the eviction policy.
func (c *queryCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// removeKeyLocked yanks one key from the LRU order slice. Caller
// already holds the mutex.
func (c *queryCache) removeKeyLocked(target cacheKey) {
	for i, k := range c.order {
		if k == target {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// cacheKeyForQueryReq builds the cache key from the wire request
// shape. Keeps the call site in Query() one-liner clean.
func cacheKeyForQueryReq(req *memapi.QueryReq) cacheKey {
	return cacheKey{
		scopeLevel:    req.Scope.Level,
		worktreeID:    req.Scope.WorktreeID,
		repoName:      req.Scope.RepoName,
		term:          req.Term,
		maxResults:    req.MaxResults,
		maxTokens:     req.MaxTokens,
		minConfidence: req.MinConfidence,
	}
}
