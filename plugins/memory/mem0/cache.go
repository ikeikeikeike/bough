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
//
// Review #23 #13: namespace participates because the mem0 plugin's
// Provider.namespace is mixed into user_id at the wire layer. Two
// tenants sharing one Provider with different namespaces but the
// same Scope shape MUST NOT share cache entries — otherwise tenant
// B sees tenant A's rows after a Query hit.
type cacheKey struct {
	namespace     string
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
//
// Review #23 #12: gen is a monotonic generation counter that
// invalidateScope bumps. Query records the generation BEFORE its
// HTTP roundtrip; put refuses to write if the generation has
// moved (= a concurrent Store / Forget / Import invalidated the
// scope in the interim). Without this, a slow Query can race a
// Store and land the pre-Store response in cache, where it then
// shadows the fresh write for up to cacheTTL.
type queryCache struct {
	mu      sync.Mutex
	entries map[cacheKey]*cacheEntry
	order   []cacheKey // append-only LRU; oldest at index 0
	clock   func() time.Time
	gen     uint64
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

// currentGen returns the cache's invalidation generation. Query
// records this before issuing the HTTP roundtrip and hands it back
// to put so a concurrent invalidateScope can rescind the would-be
// write.
func (c *queryCache) currentGen() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// put inserts or refreshes an entry. Evicts the oldest when the
// cache exceeds cacheMaxEntries.
//
// Review #23 #12: capturedGen is the generation the caller saw
// before its HTTP roundtrip. If invalidateScope bumped gen in the
// interim, put silently drops the write — the response we have is
// stale relative to the user's intent, so storing it would shadow
// the fresh write for cacheTTL. Returns true on insert, false on
// the rescinded-by-invalidation path.
func (c *queryCache) put(k cacheKey, resp *memapi.QueryResp, capturedGen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != capturedGen {
		return false
	}
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
	return true
}

// invalidateScope drops every cached entry whose key targets the
// given Scope. Round 4 AI #1 + #2: this runs from Store / Forget /
// Import after a successful upstream operation so a subsequent
// Query never returns a row mem0 no longer holds.
//
// Review #23 #12: gen bumps unconditionally so an in-flight Query
// whose HTTP roundtrip is racing this call cannot write back a
// stale response. The increment is cheap enough that we do it
// even when entries is empty.
func (c *queryCache) invalidateScope(scope memapi.Scope) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
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

// cacheKeyFor builds the cache key from the wire request shape +
// the Provider's namespace. Method form so the namespace flows
// in without leaking through cacheKey-constructing callers.
func (p *Provider) cacheKeyFor(req *memapi.QueryReq) cacheKey {
	return cacheKey{
		namespace:     p.namespace,
		scopeLevel:    req.Scope.Level,
		worktreeID:    req.Scope.WorktreeID,
		repoName:      req.Scope.RepoName,
		term:          req.Term,
		maxResults:    req.MaxResults,
		maxTokens:     req.MaxTokens,
		minConfidence: req.MinConfidence,
	}
}
