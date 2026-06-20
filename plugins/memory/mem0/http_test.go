package mem0

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	memapi "github.com/ikeikeikeike/bough/plugins/memory/api"
)

// providerForTest wires a Provider against an httptest.Server so
// the Ν-1.1d HTTP layer can be exercised without touching the real
// mem0 cloud. cfg lets the caller override apiKey / namespace per
// test.
func providerForTest(t *testing.T, srv *httptest.Server, apiKey, namespace string) *Provider {
	t.Helper()
	p, err := New(Config{
		Endpoint:  srv.URL,
		APIKey:    apiKey,
		Namespace: namespace,
		Timeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// TestDoJSON_AuthAndContentType pins the wire headers every per-
// RPC method depends on: the API key flows as `Authorization:
// Token <key>` and bodies are tagged application/json.
func TestDoJSON_AuthAndContentType(t *testing.T) {
	var seenAuth, seenCT, seenAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenCT = r.Header.Get("Content-Type")
		seenAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "my-token", "")
	body := map[string]string{"q": "early returns"}
	if err := p.doJSON(context.Background(), http.MethodPost, "/api/v1/test/", body, nil); err != nil {
		t.Fatalf("doJSON: %v", err)
	}
	if seenAuth != "Token my-token" {
		t.Errorf("auth header: got %q want %q", seenAuth, "Token my-token")
	}
	if seenCT != "application/json" {
		t.Errorf("content-type: got %q want application/json", seenCT)
	}
	if seenAccept != "application/json" {
		t.Errorf("accept: got %q want application/json", seenAccept)
	}
}

// TestDoJSON_NoAuthWhenEmpty asserts a missing API key omits the
// header rather than sending an empty Token (= self-hosted setup).
func TestDoJSON_NoAuthWhenEmpty(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "", "")
	if err := p.doJSON(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("doJSON: %v", err)
	}
	if seenAuth != "" {
		t.Errorf("auth header should be omitted: got %q", seenAuth)
	}
}

// TestDoJSON_ErrorIncludesUpstreamBody asserts non-2xx is surfaced
// with the upstream body so misconfigured mem0 instances yield
// diagnosable errors.
func TestDoJSON_ErrorIncludesUpstreamBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "missing user_id")
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "", "")
	err := p.doJSON(context.Background(), http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("expected error on 400 status")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("error should mention HTTP 400: %v", err)
	}
	if !strings.Contains(err.Error(), "missing user_id") {
		t.Errorf("error should include upstream body: %v", err)
	}
}

// TestStore_SendsAddRequest verifies Store POSTs the add-memories
// shape with the worktree-derived user_id / session_id and packs
// the Instinct fields into metadata for round-trip Import.
func TestStore_SendsAddRequest(t *testing.T) {
	var (
		seenPath string
		seenBody mem0AddReq
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		_ = json.NewEncoder(w).Encode(mem0Results{Results: []mem0Memory{{ID: "mem-1", Event: "ADD"}}})
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "tok", "")
	scope := memapi.Scope{Level: "worktree", WorktreeID: "F-add", RepoName: "auba"}
	inst := memapi.Instinct{
		ID:         "rule-add",
		Rule:       "prefer early returns",
		Scope:      scope,
		Source:     "explicit_user_feedback",
		Confidence: 0.7,
		State:      "active",
	}
	resp, err := p.Store(context.Background(), &memapi.StoreReq{
		Instinct: inst, DedupeKey: "dk-1", SourceEventID: "evt-1",
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if seenPath != "/api/v1/memories/" {
		t.Errorf("path: got %q want /api/v1/memories/", seenPath)
	}
	if !strings.HasPrefix(seenBody.UserID, "repo/") {
		t.Errorf("user_id should be repo-scoped: got %q", seenBody.UserID)
	}
	if seenBody.SessionID != "worktree/F-add" {
		t.Errorf("session_id: got %q want worktree/F-add", seenBody.SessionID)
	}
	if resp.StoredID != "mem-1" {
		t.Errorf("stored_id should adopt mem0's id: got %q", resp.StoredID)
	}
	if resp.WasUpsert {
		t.Errorf("event=ADD should not be reported as upsert")
	}
	// Metadata is the round-trip surface; pick a couple of fields.
	if got := seenBody.Metadata["bough_id"]; got != "rule-add" {
		t.Errorf("metadata bough_id: got %v want rule-add", got)
	}
	if got := seenBody.Metadata["bough_dedupe_key"]; got != "dk-1" {
		t.Errorf("metadata bough_dedupe_key: got %v want dk-1", got)
	}
	if got := seenBody.Metadata["bough_source_event_id"]; got != "evt-1" {
		t.Errorf("metadata bough_source_event_id: got %v want evt-1", got)
	}
}

// TestStore_EventUPDATE_IsUpsert exercises mem0's event tag semantics:
// UPDATE means the row pre-existed; the host treats that as upsert.
func TestStore_EventUPDATE_IsUpsert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mem0Results{Results: []mem0Memory{{ID: "mem-1", Event: "UPDATE"}}})
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "", "")
	resp, err := p.Store(context.Background(), &memapi.StoreReq{
		Instinct: memapi.Instinct{ID: "x", Rule: "y", Scope: memapi.Scope{Level: "repo", RepoName: "auba"}},
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if !resp.WasUpsert {
		t.Errorf("event=UPDATE should be reported as upsert")
	}
}

// TestQuery_SendsSearchRequest_AppliesMinConfidence verifies the
// Query path sends a search request and drops results below the
// host's MinConfidence floor.
func TestQuery_SendsSearchRequest_AppliesMinConfidence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/memories/search/" {
			t.Errorf("path: got %q want /api/v1/memories/search/", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(mem0Results{Results: []mem0Memory{
			{ID: "high", Memory: "rule a", Score: 0.9, Metadata: map[string]any{"bough_id": "high", "bough_rule": "rule a"}},
			{ID: "low", Memory: "rule b", Score: 0.2, Metadata: map[string]any{"bough_id": "low", "bough_rule": "rule b"}},
		}})
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "", "")
	qr, err := p.Query(context.Background(), &memapi.QueryReq{
		Term:          "rule",
		Scope:         memapi.Scope{Level: "worktree", RepoName: "auba", WorktreeID: "F-q"},
		MaxResults:    10,
		MaxTokens:     1000,
		MinConfidence: 0.5,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(qr.Results) != 1 {
		t.Fatalf("min_confidence should drop low-score row: got %d results", len(qr.Results))
	}
	if qr.Results[0].Instinct.ID != "high" {
		t.Errorf("expected the high-score row to survive, got %q", qr.Results[0].Instinct.ID)
	}
}

// TestForget_SendsDelete verifies Forget hits the delete endpoint
// with a path-escaped id.
func TestForget_SendsDelete(t *testing.T) {
	var seenMethod, seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		// RawPath holds the wire bytes (with %-escapes) when the URL
		// parser had to decode something; otherwise it is empty and
		// Path already matches the wire.
		if r.URL.RawPath != "" {
			seenPath = r.URL.RawPath
		} else {
			seenPath = r.URL.Path
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "", "")
	if _, err := p.Forget(context.Background(), &memapi.ForgetReq{ID: "abc/1"}); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if seenMethod != http.MethodDelete {
		t.Errorf("method: got %q want DELETE", seenMethod)
	}
	if seenPath != "/api/v1/memories/abc%2F1/" {
		t.Errorf("path: got %q want /api/v1/memories/abc%%2F1/", seenPath)
	}
}

// TestExportImport_RoundTrip_YAML drives the YAML round trip
// against a mem0 mock that just echoes one row back from a list
// call. The Import path then re-Stores the row; the test asserts
// the wire payload's id field is preserved.
func TestExportImport_RoundTrip_YAML(t *testing.T) {
	var storedIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/memories/"):
			_ = json.NewEncoder(w).Encode(mem0Results{Results: []mem0Memory{
				{
					ID:     "round-1",
					Memory: "round-trip rule",
					Metadata: map[string]any{
						"bough_id":          "round-1",
						"bough_rule":        "round-trip rule",
						"bough_scope_level": "worktree",
						"bough_source":      "test_failure",
						"bough_state":       "active",
						"bough_confidence":  0.6,
					},
				},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/memories/":
			var body mem0AddReq
			_ = json.NewDecoder(r.Body).Decode(&body)
			if id, ok := body.Metadata["bough_id"].(string); ok {
				storedIDs = append(storedIDs, id)
			}
			_ = json.NewEncoder(w).Encode(mem0Results{Results: []mem0Memory{{ID: "round-1", Event: "ADD"}}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	p := providerForTest(t, srv, "", "")
	scope := memapi.Scope{Level: "worktree", RepoName: "auba", WorktreeID: "F-rt"}
	exp, err := p.Export(context.Background(), &memapi.ExportReq{Format: "yaml", Scope: scope})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !strings.Contains(string(exp.Payload), "round-1") {
		t.Errorf("export payload should mention round-1: %s", string(exp.Payload))
	}
	imp, err := p.Import(context.Background(), &memapi.ImportReq{Format: "yaml", Payload: exp.Payload})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if (imp.ImportedCount + imp.UpsertedCount) != 1 {
		t.Errorf("import should restore 1 row: got %+v", imp)
	}
	if len(storedIDs) != 1 || storedIDs[0] != "round-1" {
		t.Errorf("import should send 1 add-memories with the original id: got %v", storedIDs)
	}
}
