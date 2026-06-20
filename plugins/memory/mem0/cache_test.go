package mem0

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	memapi "github.com/ikeikeikeike/bough/plugins/memory/api"
)

// fixedClock returns a clock function that always reports `now`.
// Tests advance it by reassigning the variable.
func fixedClock(now *time.Time) func() time.Time {
	return func() time.Time { return *now }
}

// TestQueryCache_HitAndMiss pins the basic contract: a put then a
// get with the same key returns the cached value; a different key
// misses.
func TestQueryCache_HitAndMiss(t *testing.T) {
	c := newQueryCache()
	k1 := cacheKey{scopeLevel: "repo", repoName: "auba", term: "early"}
	k2 := cacheKey{scopeLevel: "repo", repoName: "auba", term: "late"}
	resp := &memapi.QueryResp{Results: []memapi.QueryResult{{Score: 0.9}}}
	c.put(k1, resp)
	if got, ok := c.get(k1); !ok || got != resp {
		t.Errorf("k1 should hit and return the put value")
	}
	if _, ok := c.get(k2); ok {
		t.Errorf("k2 should miss")
	}
}

// TestQueryCache_TTLExpiry advances the fake clock past cacheTTL
// and asserts the entry is dropped lazily on get.
func TestQueryCache_TTLExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := newQueryCache()
	c.clock = fixedClock(&now)
	k := cacheKey{term: "expired"}
	c.put(k, &memapi.QueryResp{})
	if _, ok := c.get(k); !ok {
		t.Fatal("entry should be live immediately after put")
	}
	now = now.Add(cacheTTL + 1*time.Second)
	if _, ok := c.get(k); ok {
		t.Errorf("entry should be evicted after TTL")
	}
	if c.len() != 0 {
		t.Errorf("cache should be empty after expiry: %d", c.len())
	}
}

// TestQueryCache_LRUEviction puts one more entry than the cap and
// asserts the oldest one was evicted.
func TestQueryCache_LRUEviction(t *testing.T) {
	c := newQueryCache()
	for i := 0; i < cacheMaxEntries+1; i++ {
		k := cacheKey{term: "k" + string(rune('0'+i%10)), maxResults: i}
		c.put(k, &memapi.QueryResp{})
	}
	if c.len() != cacheMaxEntries {
		t.Errorf("cache should be capped at %d entries: got %d", cacheMaxEntries, c.len())
	}
}

// TestQueryCache_InvalidateScope drops entries whose key targets
// the named scope and leaves other scopes intact.
func TestQueryCache_InvalidateScope(t *testing.T) {
	c := newQueryCache()
	target := cacheKey{scopeLevel: "worktree", worktreeID: "F-x", repoName: "auba", term: "a"}
	survivor := cacheKey{scopeLevel: "worktree", worktreeID: "F-y", repoName: "auba", term: "a"}
	c.put(target, &memapi.QueryResp{})
	c.put(survivor, &memapi.QueryResp{})
	c.invalidateScope(memapi.Scope{Level: "worktree", WorktreeID: "F-x", RepoName: "auba"})
	if _, ok := c.get(target); ok {
		t.Errorf("target entry should be evicted by invalidateScope")
	}
	if _, ok := c.get(survivor); !ok {
		t.Errorf("survivor entry should remain after invalidateScope")
	}
}

// TestProviderQuery_CacheHit verifies the Provider.Query path
// short-circuits on the second call. The mock server counts hits
// — a single underlying request is enough for two identical Query
// calls.
func TestProviderQuery_CacheHit(t *testing.T) {
	var serverHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHits.Add(1)
		_ = json.NewEncoder(w).Encode(mem0Results{Results: []mem0Memory{
			{ID: "x", Memory: "rule", Score: 0.9, Metadata: map[string]any{"bough_id": "x", "bough_rule": "rule"}},
		}})
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "", "")
	req := &memapi.QueryReq{
		Term:          "rule",
		Scope:         memapi.Scope{Level: "repo", RepoName: "auba"},
		MaxResults:    5,
		MaxTokens:     1000,
		MinConfidence: 0.0,
	}
	if _, err := p.Query(context.Background(), req); err != nil {
		t.Fatalf("Query#1: %v", err)
	}
	if _, err := p.Query(context.Background(), req); err != nil {
		t.Fatalf("Query#2: %v", err)
	}
	if got := serverHits.Load(); got != 1 {
		t.Errorf("server should be hit exactly once with cache live: got %d", got)
	}
}

// TestProviderStore_InvalidatesCache stores after a cached Query
// and asserts the next Query for the same scope re-hits the server.
func TestProviderStore_InvalidatesCache(t *testing.T) {
	var serverHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/memories/search/" {
			serverHits.Add(1)
			_ = json.NewEncoder(w).Encode(mem0Results{})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/memories/" {
			_ = json.NewEncoder(w).Encode(mem0Results{Results: []mem0Memory{{ID: "added", Event: "ADD"}}})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "", "")
	scope := memapi.Scope{Level: "repo", RepoName: "auba"}
	req := &memapi.QueryReq{
		Term:       "rule",
		Scope:      scope,
		MaxResults: 5,
		MaxTokens:  1000,
	}
	// 1st Query primes the cache.
	if _, err := p.Query(context.Background(), req); err != nil {
		t.Fatalf("Query#1: %v", err)
	}
	// 2nd Query hits the cache.
	if _, err := p.Query(context.Background(), req); err != nil {
		t.Fatalf("Query#2: %v", err)
	}
	if got := serverHits.Load(); got != 1 {
		t.Fatalf("before Store: server should be hit once, got %d", got)
	}
	// Store on the same scope invalidates the cache.
	if _, err := p.Store(context.Background(), &memapi.StoreReq{
		Instinct: memapi.Instinct{ID: "x", Rule: "y", Scope: scope},
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	// 3rd Query should now miss the cache and re-hit the server.
	if _, err := p.Query(context.Background(), req); err != nil {
		t.Fatalf("Query#3: %v", err)
	}
	if got := serverHits.Load(); got != 2 {
		t.Errorf("after Store invalidate: server should be hit twice, got %d", got)
	}
}
