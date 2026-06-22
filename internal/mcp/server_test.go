package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	memapi "github.com/ikeikeikeike/bough/plugins/memory/api"
)

// fakeBackend mirrors the host-side fake from internal/instinct;
// only Query is exercised by the v0.6 read-only MCP surface.
type fakeBackend struct {
	results []memapi.Instinct
	queries int
}

func (f *fakeBackend) Health(_ context.Context, _ *memapi.HealthReq) (*memapi.HealthResp, error) {
	return &memapi.HealthResp{}, nil
}

func (f *fakeBackend) Capabilities(_ context.Context) (*memapi.CapabilitiesResp, error) {
	return &memapi.CapabilitiesResp{}, nil
}

func (f *fakeBackend) Store(_ context.Context, _ *memapi.StoreReq) (*memapi.StoreResp, error) {
	return &memapi.StoreResp{}, nil
}

func (f *fakeBackend) Query(_ context.Context, _ *memapi.QueryReq) (*memapi.QueryResp, error) {
	f.queries++
	out := make([]memapi.QueryResult, len(f.results))
	for i, r := range f.results {
		out[i] = memapi.QueryResult{Instinct: r}
	}
	return &memapi.QueryResp{Results: out}, nil
}

func (f *fakeBackend) Forget(_ context.Context, _ *memapi.ForgetReq) (*memapi.ForgetResp, error) {
	return &memapi.ForgetResp{}, nil
}

func (f *fakeBackend) Export(_ context.Context, _ *memapi.ExportReq) (*memapi.ExportResp, error) {
	return &memapi.ExportResp{}, nil
}

func (f *fakeBackend) Import(_ context.Context, _ *memapi.ImportReq) (*memapi.ImportResp, error) {
	return &memapi.ImportResp{}, nil
}

// runRequest dispatches a single JSON-RPC line through Server.
// Returns the unmarshalled response so each test can assert on the
// shape directly.
func runRequest(t *testing.T, s *Server, line string) jsonrpcResponse {
	t.Helper()
	var buf bytes.Buffer
	if err := s.dispatchLine(context.Background(), []byte(line), &buf); err != nil {
		t.Fatalf("dispatchLine: %v", err)
	}
	var resp jsonrpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nraw: %s", err, buf.String())
	}
	return resp
}

func TestServer_Initialize_AdvertiseReadOnly(t *testing.T) {
	s := NewServer(&fakeBackend{}, func() {}, "v0.6.0-test", false)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if resp.Error != nil {
		t.Fatalf("initialize: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var got InitializeResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.ProtocolVersion != MCPSpecVersion {
		t.Errorf("protocolVersion: got %q want %q", got.ProtocolVersion, MCPSpecVersion)
	}
	if got.Capabilities.BoughMCPServer.ReadOnly != true {
		t.Errorf("bough_mcp_server.read_only should be true on v0.6")
	}
	if got.Capabilities.BoughMCPServer.StateChangingTools {
		t.Errorf("bough_mcp_server.state_changing_tools should be false on v0.6")
	}
}

func TestServer_ToolsList_ReadOnly(t *testing.T) {
	s := NewServer(&fakeBackend{}, func() {}, "v", false)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if resp.Error != nil {
		t.Fatalf("tools/list: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var got ToolsListResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "memory.query" {
		t.Errorf("v0.6 should expose memory.query only: %+v", got.Tools)
	}
}

func TestServer_ToolsCall_StateChangingRefused(t *testing.T) {
	s := NewServer(&fakeBackend{}, func() {}, "v", false)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory.store"}}`)
	if resp.Error == nil {
		t.Fatal("memory.store should refuse on v0.6")
	}
	if resp.Error.Code != codeWriteForbidden {
		t.Errorf("error code: got %d want %d", resp.Error.Code, codeWriteForbidden)
	}
	if !strings.Contains(resp.Error.Message, "--allow-write") {
		t.Errorf("error should mention --allow-write opt-in: %q", resp.Error.Message)
	}
}

func TestServer_ToolsCall_MemoryQueryRoundTrip(t *testing.T) {
	fb := &fakeBackend{results: []memapi.Instinct{{
		ID:   "rule-1",
		Rule: "prefer early returns",
	}}}
	s := NewServer(fb, func() {}, "v", false)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"memory.query","arguments":{"term":"early"}}}`)
	if resp.Error != nil {
		t.Fatalf("memory.query: %+v", resp.Error)
	}
	if fb.queries != 1 {
		t.Errorf("backend should be queried once: %d", fb.queries)
	}
	raw, _ := json.Marshal(resp.Result)
	var got ToolCallResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.IsError {
		t.Errorf("memory.query should not flag isError: %+v", got)
	}
	if len(got.Content) != 1 || !strings.Contains(got.Content[0].Text, "rule-1") {
		t.Errorf("content should render the row id: %+v", got.Content)
	}
}

func TestServer_ResourcesList(t *testing.T) {
	s := NewServer(&fakeBackend{}, func() {}, "v", false)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":5,"method":"resources/list"}`)
	if resp.Error != nil {
		t.Fatalf("resources/list: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var got ResourcesListResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Resources) != 1 || got.Resources[0].URI != "bough://memory/scopes" {
		t.Errorf("v0.6 should expose bough://memory/scopes only: %+v", got.Resources)
	}
}

// fakeWriteBackend records Store / Forget calls so the --allow-write
// path tests can verify the MCP server actually drove the backend
// rather than just rendering an OK message.
type fakeWriteBackend struct {
	fakeBackend
	stores  []*memapi.StoreReq
	forgets []*memapi.ForgetReq
}

func (f *fakeWriteBackend) Store(_ context.Context, req *memapi.StoreReq) (*memapi.StoreResp, error) {
	f.stores = append(f.stores, req)
	return &memapi.StoreResp{StoredID: req.DedupeKey, WasUpsert: false}, nil
}

func (f *fakeWriteBackend) Forget(_ context.Context, req *memapi.ForgetReq) (*memapi.ForgetResp, error) {
	f.forgets = append(f.forgets, req)
	return &memapi.ForgetResp{}, nil
}

// TestServer_ToolsList_AllowWriteExposesStoreAndForget pins the
// catalogue against the --allow-write surface: memory.query stays,
// memory.store and memory.forget land, memory.promote is still
// withheld until v0.7 (= it needs the host coordinator).
func TestServer_ToolsList_AllowWriteExposesStoreAndForget(t *testing.T) {
	s := NewServer(&fakeWriteBackend{}, func() {}, "v", true)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":10,"method":"tools/list"}`)
	if resp.Error != nil {
		t.Fatalf("tools/list: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var got ToolsListResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	names := make(map[string]bool, len(got.Tools))
	for _, tool := range got.Tools {
		names[tool.Name] = true
	}
	if !names["memory.query"] {
		t.Errorf("memory.query should always ship: %+v", got.Tools)
	}
	if !names["memory.store"] {
		t.Errorf("memory.store should ship with --allow-write: %+v", got.Tools)
	}
	if !names["memory.forget"] {
		t.Errorf("memory.forget should ship with --allow-write: %+v", got.Tools)
	}
	if names["memory.promote"] {
		t.Errorf("memory.promote should NOT ship until v0.7 (needs the coordinator): %+v", got.Tools)
	}
}

// TestServer_Initialize_AdvertiseWritable pins the Capabilities
// vendor block so MCP clients can probe the writable surface
// programmatically when the host runs with --allow-write.
func TestServer_Initialize_AdvertiseWritable(t *testing.T) {
	s := NewServer(&fakeWriteBackend{}, func() {}, "v0.6.1-test", true)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":11,"method":"initialize"}`)
	if resp.Error != nil {
		t.Fatalf("initialize: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var got InitializeResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Capabilities.BoughMCPServer.ReadOnly {
		t.Errorf("read_only should be false with --allow-write")
	}
	if !got.Capabilities.BoughMCPServer.StateChangingTools {
		t.Errorf("state_changing_tools should be true with --allow-write")
	}
}

// TestServer_ToolsCall_MemoryStoreWritesCandidate exercises the
// memory.store path through a recording backend: the MCP server
// must stamp state=candidate and reuse the canonical dedupe key
// sha256(rule | scope) so cross-entry-point dedupe holds.
func TestServer_ToolsCall_MemoryStoreWritesCandidate(t *testing.T) {
	fb := &fakeWriteBackend{}
	s := NewServer(fb, func() {}, "v", true)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"memory.store","arguments":{"rule":"prefer early returns","scope":"worktree"}}}`)
	if resp.Error != nil {
		t.Fatalf("memory.store: %+v", resp.Error)
	}
	if got := len(fb.stores); got != 1 {
		t.Fatalf("backend.Store should be invoked once: got %d", got)
	}
	stored := fb.stores[0]
	if stored.Instinct.State != "candidate" {
		t.Errorf("state should be candidate to require approval: got %q", stored.Instinct.State)
	}
	if stored.Instinct.Rule != "prefer early returns" {
		t.Errorf("rule mismatch: got %q", stored.Instinct.Rule)
	}
	if stored.DedupeKey == "" {
		t.Errorf("dedupe_key should be stamped by the server")
	}
	if stored.Instinct.ID != stored.DedupeKey {
		t.Errorf("instinct ID should equal dedupe_key for a fresh row: got id=%q dedupe=%q",
			stored.Instinct.ID, stored.DedupeKey)
	}
	if !strings.Contains(stored.SourceEventID, "mcp/") {
		t.Errorf("source_event_id should carry the mcp/ provenance prefix: %q", stored.SourceEventID)
	}
}

// TestServer_ToolsCall_MemoryForgetSoftDeletes exercises the
// memory.forget path through the recording backend.
func TestServer_ToolsCall_MemoryForgetSoftDeletes(t *testing.T) {
	fb := &fakeWriteBackend{}
	s := NewServer(fb, func() {}, "v", true)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"memory.forget","arguments":{"id":"rule-xyz","reason":"obsolete"}}}`)
	if resp.Error != nil {
		t.Fatalf("memory.forget: %+v", resp.Error)
	}
	if got := len(fb.forgets); got != 1 {
		t.Fatalf("backend.Forget should be invoked once: got %d", got)
	}
	if fb.forgets[0].ID != "rule-xyz" {
		t.Errorf("ID mismatch: got %q", fb.forgets[0].ID)
	}
	if fb.forgets[0].Reason != "obsolete" {
		t.Errorf("reason should propagate to the backend: got %q", fb.forgets[0].Reason)
	}
}

// TestServer_ToolsCall_MemoryPromoteStillRefused pins the v0.7
// deferral: even with --allow-write, memory.promote must refuse
// because it needs the host coordinator, not just the backend.
func TestServer_ToolsCall_MemoryPromoteStillRefused(t *testing.T) {
	s := NewServer(&fakeWriteBackend{}, func() {}, "v", true)
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"memory.promote","arguments":{"id":"x","to":"repo"}}}`)
	if resp.Error == nil {
		t.Fatal("memory.promote should still refuse on v0.6.1 (= needs coordinator, lands in v0.7)")
	}
	if resp.Error.Code != codeWriteForbidden {
		t.Errorf("error code: got %d want %d", resp.Error.Code, codeWriteForbidden)
	}
	if !strings.Contains(resp.Error.Message, "v0.7") {
		t.Errorf("error should mention v0.7 deferral: %q", resp.Error.Message)
	}
}

func TestServer_ShutdownIdempotent(t *testing.T) {
	calls := 0
	s := NewServer(&fakeBackend{}, func() { calls++ }, "v", false)
	s.Shutdown()
	s.Shutdown()
	if calls != 1 {
		t.Errorf("close should fire exactly once: %d", calls)
	}
	// Subsequent dispatches should refuse cleanly.
	resp := runRequest(t, s, `{"jsonrpc":"2.0","id":99,"method":"tools/list"}`)
	if resp.Error == nil {
		t.Errorf("post-shutdown dispatch should return error")
	}
}
