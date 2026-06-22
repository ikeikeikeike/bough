package mcp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	memapi "github.com/ikeikeikeike/bough/plugins/memory/api"
)

// Server is the bough-side MCP server. It speaks JSON-RPC 2.0 over
// any io.Reader / io.Writer pair (= stdio in production, an
// io.Pipe in tests) and routes incoming methods through the
// dispatch table below.
//
// The server holds a MemoryBackend client (= discovered by the host)
// and the close func that disposes the backend subprocess. Round 4
// AI #1 zombie-process guard: when stdin closes, the server marks
// itself shut and invokes close(), which terminates the SQLite
// subprocess so the DB file lock is released.
type Server struct {
	backend memapi.MemoryBackend
	close   func()
	version string

	// allowWrite gates the state-changing tools (memory.store /
	// memory.forget). v0.6.0 shipped with this hard-wired false
	// (= "read-only first"); v0.6.1 reads the host's --allow-write
	// CLI flag into it. When false, the host-side behaviour is
	// indistinguishable from v0.6.0: tools/list omits the write
	// tools and tools/call refuses them with codeWriteForbidden.
	allowWrite bool

	// shut flips to 1 once Graceful Shutdown begins. New incoming
	// requests after shut=1 return a JSON-RPC error rather than
	// reaching the backend, so the host can drain in-flight RPCs
	// cleanly.
	shut atomic.Int32

	writeMu sync.Mutex // serialises stdout writes (JSON-RPC requires whole-message atomicity)

	// v0.7.0 O-1.7 write hardening surfaces. All three are
	// optional — when zero / empty the server behaves exactly as
	// v0.6.1 did. Round 5 AI B Q4 mitigation set covered:
	// dry-run (= --allow-write opt-in), per-tool permission, per-
	// worktree scope, rate limit, append-only audit log, schema
	// validation. v0.7.0 lands rate limit + audit log + scope
	// boundary; per-tool granular permission falls out from the
	// existing memory.store / memory.forget / memory.promote split
	// since promote stays refused unconditionally.
	auditMu       sync.Mutex
	auditPath     string
	rateLimitMu   sync.Mutex
	rateLimitMax  int
	rateWindow    time.Duration
	rateWindowEnd time.Time
	rateCount     int
	allowedScopes map[string]struct{}
}

// NewServer constructs a Server from an already-discovered backend
// and a close callback. The host invokes close exactly once at
// shutdown — the watchdog goroutine or a sentinel "shutdown" method
// — so the SQLite subprocess never lingers.
//
// allowWrite=false matches v0.6.0 behaviour (= read-only first); the
// v0.6.1 host flips it to true via the --allow-write CLI flag so MCP
// clients can drive memory.store / memory.forget through the same
// stdio surface as memory.query.
func NewServer(backend memapi.MemoryBackend, closeFn func(), version string, allowWrite bool) *Server {
	return &Server{
		backend:    backend,
		close:      closeFn,
		version:    version,
		allowWrite: allowWrite,
		rateWindow: time.Minute,
	}
}

// SetAuditLogPath turns on append-only JSONL auditing of every
// successful memory.store / memory.forget invocation. Each call
// produces one line under the supplied path. Round 5 AI B Q4 + AI A
// Q4: "audit log immutable append-only" + "Trace ID for provenance".
// Path empty disables auditing (= v0.6.1 default behaviour).
func (s *Server) SetAuditLogPath(path string) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	s.auditPath = path
}

// SetRateLimit bounds memory.store + memory.forget invocations to
// `max` per `window` (e.g. 60 per minute). Zero `max` disables the
// limit (= v0.6.1 default). Round 5 AI B Q4 mitigation #7.
func (s *Server) SetRateLimit(max int, window time.Duration) {
	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()
	s.rateLimitMax = max
	if window > 0 {
		s.rateWindow = window
	}
	s.rateWindowEnd = time.Time{}
	s.rateCount = 0
}

// SetAllowedScopes restricts memory.store / memory.forget calls to
// the named scope levels (e.g. ["worktree"]). Empty slice or nil
// = all scopes accepted. Round 5 AI B Q4 mitigation #4 + #5: per-
// worktree scope boundary keeps an MCP-side bug from accidentally
// promoting into repo / global memory without the operator's
// explicit consent.
func (s *Server) SetAllowedScopes(scopes []string) {
	if len(scopes) == 0 {
		s.allowedScopes = nil
		return
	}
	m := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		m[scope] = struct{}{}
	}
	s.allowedScopes = m
}

// checkRateLimit increments the counter and returns false when the
// caller exceeded the configured ceiling. Wraps on the configured
// window so a long-running session does not eventually choke
// itself.
func (s *Server) checkRateLimit() bool {
	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()
	if s.rateLimitMax <= 0 {
		return true
	}
	now := time.Now()
	if now.After(s.rateWindowEnd) {
		s.rateWindowEnd = now.Add(s.rateWindow)
		s.rateCount = 0
	}
	if s.rateCount >= s.rateLimitMax {
		return false
	}
	s.rateCount++
	return true
}

// scopeAllowed returns true when scope is within the configured
// allow-list, or when no allow-list has been configured.
func (s *Server) scopeAllowed(scopeLevel string) bool {
	if s.allowedScopes == nil {
		return true
	}
	_, ok := s.allowedScopes[scopeLevel]
	return ok
}

// appendAudit writes one JSONL line per successful write. Errors
// are not fatal to the caller — auditing is a side-channel and
// the operator would rather see the store succeed than block the
// MCP client because the audit file is on a full disk. We surface
// the failure on stderr instead.
func (s *Server) appendAudit(record map[string]any) {
	s.auditMu.Lock()
	path := s.auditPath
	s.auditMu.Unlock()
	if path == "" {
		return
	}
	record["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "bough-mcp-server: audit mkdir failed: %v\n", err)
		return
	}
	line, err := json.Marshal(record)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bough-mcp-server: audit marshal failed: %v\n", err)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bough-mcp-server: audit open failed: %v\n", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "bough-mcp-server: audit write failed: %v\n", err)
	}
}

// Run loops on r, dispatching each incoming line as a JSON-RPC
// request. Newline-delimited framing matches Claude Desktop's
// reference MCP transport. Return on EOF or ctx cancellation.
func (s *Server) Run(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := s.dispatchLine(ctx, line, w); err != nil {
			// dispatchLine already wrote a JSON-RPC error response;
			// surface non-protocol errors (= write failures) so the
			// caller (main.go) can decide whether to retry or abort.
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("mcp scanner: %w", err)
	}
	return nil
}

// Shutdown marks the server shut and disposes the backend subprocess.
// Idempotent — calling twice is safe, the second call short-circuits.
func (s *Server) Shutdown() {
	if !s.shut.CompareAndSwap(0, 1) {
		return
	}
	if s.close != nil {
		s.close()
	}
}

// dispatchLine parses one JSON-RPC request and routes to the
// per-method handler. Parsing errors send a Parse Error response;
// unknown methods send Method Not Found.
func (s *Server) dispatchLine(ctx context.Context, line []byte, w io.Writer) error {
	var req jsonrpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return s.writeError(w, nil, codeParseError, fmt.Sprintf("parse: %v", err))
	}
	if req.JSONRPC != "2.0" {
		return s.writeError(w, req.ID, codeInvalidRequest, fmt.Sprintf("jsonrpc=%q want 2.0", req.JSONRPC))
	}
	if s.shut.Load() != 0 {
		return s.writeError(w, req.ID, codeInternalError, "server shutting down")
	}
	switch req.Method {
	case "initialize":
		return s.handleInitialize(w, req)
	case "tools/list":
		return s.handleToolsList(w, req)
	case "tools/call":
		return s.handleToolsCall(ctx, w, req)
	case "resources/list":
		return s.handleResourcesList(w, req)
	case "resources/read":
		return s.handleResourcesRead(ctx, w, req)
	case "prompts/list":
		// v0.6 ships no prompts; respond with an empty list rather
		// than method-not-found so clients with default UIs do not
		// surface a hard error.
		return s.writeResult(w, req.ID, map[string]any{"prompts": []any{}})
	case "shutdown":
		s.Shutdown()
		return s.writeResult(w, req.ID, map[string]any{"ok": true})
	default:
		return s.writeError(w, req.ID, codeMethodNotFound, fmt.Sprintf("method %q not implemented in v0.6 (read-only first)", req.Method))
	}
}

// handleInitialize answers the spec's negotiation handshake. The
// payload advertises the bough_mcp_server vendor block so clients
// can probe v0.6's read-only boundary programmatically.
func (s *Server) handleInitialize(w io.Writer, req jsonrpcRequest) error {
	result := InitializeResult{
		ProtocolVersion: MCPSpecVersion,
		ServerInfo: ServerInfo{
			Name:    "bough-mcp-server",
			Version: s.version,
		},
		Capabilities: ServerCapabilities{
			Tools:     map[string]any{},
			Resources: map[string]any{},
			Prompts:   map[string]any{},
			BoughMCPServer: BoughMCPCapabilities{
				SpecVersion:        MCPSpecVersion,
				ReadOnly:           !s.allowWrite,
				StateChangingTools: s.allowWrite,
				HostVersion:        s.version,
			},
		},
	}
	return s.writeResult(w, req.ID, result)
}

// handleToolsList returns the tool catalogue. memory.query always
// ships; memory.store / memory.forget land in the list only when
// the host was launched with --allow-write (= v0.6.1).
func (s *Server) handleToolsList(w io.Writer, req jsonrpcRequest) error {
	tools := []ToolDefinition{
		{
			Name:        "memory.query",
			Description: "Search bough's memory backend within the configured scope. Read-only.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"term": map[string]any{
						"type":        "string",
						"description": "search term; empty string returns the configured-scope window",
					},
					"scope": map[string]any{
						"type":        "string",
						"description": "scope level (worktree | repo | global); defaults to worktree",
					},
					"max_results": map[string]any{
						"type":    "integer",
						"default": 12,
					},
				},
			},
		},
	}
	if s.allowWrite {
		tools = append(tools,
			ToolDefinition{
				Name:        "memory.store",
				Description: "Store a behavioural rule into bough's memory backend as a candidate. The host writes the row with state=candidate so a human approval step (`bough instinct approve <id>`) is required before it goes active. Side effect: persists a new row, writes a `mcp.store` event to the audit log, and may trigger backend-side deduplication.",
				InputSchema: map[string]any{
					"type":     "object",
					"required": []string{"rule"},
					"properties": map[string]any{
						"rule": map[string]any{
							"type":        "string",
							"description": "the behavioural rule string (= the canonical text of the instinct)",
						},
						"scope": map[string]any{
							"type":        "string",
							"description": "scope level (worktree | repo | global); defaults to worktree",
						},
						"source": map[string]any{
							"type":        "string",
							"description": "trace source classification; defaults to 'explicit_user_feedback'",
						},
					},
				},
			},
			ToolDefinition{
				Name:        "memory.forget",
				Description: "Soft-delete an instinct by ID. The backend keeps the row but flips its state to forgotten so subsequent queries skip it. Side effect: state mutation + a `mcp.forget` audit event.",
				InputSchema: map[string]any{
					"type":     "object",
					"required": []string{"id"},
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "the instinct ID to forget (= dedupe_key sha256 hash)",
						},
						"scope": map[string]any{
							"type":        "string",
							"description": "scope level (worktree | repo | global); defaults to worktree",
						},
						"reason": map[string]any{
							"type":        "string",
							"description": "audit reason explaining why this rule is being retired",
						},
					},
				},
			},
		)
	}
	return s.writeResult(w, req.ID, ToolsListResult{Tools: tools})
}

// handleToolsCall dispatches a tool invocation. v0.6 only supports
// memory.query; every other tool name returns Method Not Found and
// state-changing operations refuse with codeWriteForbidden.
func (s *Server) handleToolsCall(ctx context.Context, w io.Writer, req jsonrpcRequest) error {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.writeError(w, req.ID, codeInvalidRequest, fmt.Sprintf("params: %v", err))
	}
	switch params.Name {
	case "memory.query":
		return s.callMemoryQuery(ctx, w, req, params)
	case "memory.store":
		if !s.allowWrite {
			return s.writeError(w, req.ID, codeWriteForbidden, "memory.store is a state-changing tool; the host is running without --allow-write")
		}
		return s.callMemoryStore(ctx, w, req, params)
	case "memory.forget":
		if !s.allowWrite {
			return s.writeError(w, req.ID, codeWriteForbidden, "memory.forget is a state-changing tool; the host is running without --allow-write")
		}
		return s.callMemoryForget(ctx, w, req, params)
	case "memory.promote":
		// memory.promote needs the host coordinator (= Store(target) +
		// Forget(source) pair), not just the backend client this server
		// holds. v0.6.1 refuses it even with --allow-write; v0.7 wires
		// it through the coordinator alongside the Bootstrap layer.
		return s.writeError(w, req.ID, codeWriteForbidden, "memory.promote requires the host coordinator and lands in v0.7 alongside the Bootstrap layer")
	default:
		return s.writeError(w, req.ID, codeMethodNotFound, fmt.Sprintf("tool %q not registered", params.Name))
	}
}

// callMemoryQuery runs MemoryBackend.Query and renders the result
// as MCP text content. Errors at the backend layer surface as
// isError=true content rather than JSON-RPC errors so the MCP
// client sees them as tool failures, not protocol failures.
func (s *Server) callMemoryQuery(ctx context.Context, w io.Writer, req jsonrpcRequest, params ToolCallParams) error {
	scopeLevel := "worktree"
	if raw, ok := params.Arguments["scope"].(string); ok && raw != "" {
		scopeLevel = raw
	}
	term, _ := params.Arguments["term"].(string)
	maxResults := 12
	if raw, ok := params.Arguments["max_results"].(float64); ok && int(raw) > 0 {
		maxResults = int(raw)
	}
	qresp, err := s.backend.Query(ctx, &memapi.QueryReq{
		Term:       term,
		Scope:      memapi.Scope{Level: scopeLevel},
		MaxResults: maxResults,
		MaxTokens:  4000,
	})
	if err != nil {
		return s.writeResult(w, req.ID, ToolCallResult{
			Content: []ToolCallContent{{Type: "text", Text: fmt.Sprintf("memory.query failed: %v", err)}},
			IsError: true,
		})
	}
	var b strings.Builder
	if len(qresp.Results) == 0 {
		b.WriteString("(no memories matched the term in the configured scope)")
	}
	for _, r := range qresp.Results {
		fmt.Fprintf(&b, "- [%s] %s\n", r.Instinct.ID, r.Instinct.Rule)
	}
	return s.writeResult(w, req.ID, ToolCallResult{
		Content: []ToolCallContent{{Type: "text", Text: b.String()}},
	})
}

// callMemoryStore runs MemoryBackend.Store after stamping the
// canonical dedupe key (= sha256(rule | scope-level | worktree-id |
// repo-name)) and forcing state=candidate. The host coordinator's
// approval flow (`bough instinct approve <id>`) is what flips the
// row to active — v0.6.1 deliberately refuses to bypass that even
// when the MCP client carries explicit_user_feedback intent, because
// memory poisoning prevention (= round 1 design freeze) demands a
// human review step before active artifacts ship.
//
// Audit: every store writes a line to stderr (= the host's chosen
// log stream); v0.7 wires this through the coordinator so the
// events.jsonl audit log captures `mcp.store` events alongside the
// CLI-driven mint / ingest paths.
func (s *Server) callMemoryStore(ctx context.Context, w io.Writer, req jsonrpcRequest, params ToolCallParams) error {
	rule, _ := params.Arguments["rule"].(string)
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return s.writeError(w, req.ID, codeInvalidRequest, "memory.store: 'rule' argument is required and cannot be empty")
	}
	scopeLevel := "worktree"
	if raw, ok := params.Arguments["scope"].(string); ok && raw != "" {
		scopeLevel = raw
	}
	if !s.scopeAllowed(scopeLevel) {
		return s.writeError(w, req.ID, codeWriteForbidden, fmt.Sprintf("memory.store: scope %q is outside the server's allowed_scopes list", scopeLevel))
	}
	if !s.checkRateLimit() {
		return s.writeError(w, req.ID, codeWriteForbidden, "memory.store: rate limit exceeded; retry shortly")
	}
	source := "explicit_user_feedback"
	if raw, ok := params.Arguments["source"].(string); ok && raw != "" {
		source = raw
	}
	scope := memapi.Scope{Level: scopeLevel}
	dedupeKey := computeMemoryDedupeKey(rule, scope)
	fmt.Fprintf(os.Stderr, "bough-mcp-server: memory.store: scope=%s source=%s id=%s rule=%q\n",
		scopeLevel, source, dedupeKey, rule)
	storeResp, err := s.backend.Store(ctx, &memapi.StoreReq{
		Instinct: memapi.Instinct{
			ID:         dedupeKey,
			Rule:       rule,
			Scope:      scope,
			Source:     source,
			Confidence: 0.5, // host default; ConfidencePolicy clamps later when ingest runs
			State:      "candidate",
			CreatedAt:  time.Now().UTC(),
		},
		DedupeKey:       dedupeKey,
		SourceEventID:   "mcp/" + dedupeKey,
		UpsertSemantics: true,
	})
	if err != nil {
		return s.writeResult(w, req.ID, ToolCallResult{
			Content: []ToolCallContent{{Type: "text", Text: fmt.Sprintf("memory.store failed: %v", err)}},
			IsError: true,
		})
	}
	verb := "stored"
	if storeResp.WasUpsert {
		verb = "reinforced (existing dedupe key)"
	}
	s.appendAudit(map[string]any{
		"op":         "memory.store",
		"scope":      scopeLevel,
		"id":         storeResp.StoredID,
		"upsert":     storeResp.WasUpsert,
		"source":     source,
		"dedupe_key": dedupeKey,
		"rule":       rule,
	})
	text := fmt.Sprintf("%s as candidate. id=%s\n\nApprove with: bough instinct approve %s", verb, storeResp.StoredID, storeResp.StoredID)
	return s.writeResult(w, req.ID, ToolCallResult{
		Content: []ToolCallContent{{Type: "text", Text: text}},
	})
}

// callMemoryForget routes a soft-delete through MemoryBackend.Forget.
// The backend keeps the row but flips its state so subsequent
// queries skip it; this is recoverable through an Export → Import
// round trip with state="forgotten" preserved.
func (s *Server) callMemoryForget(ctx context.Context, w io.Writer, req jsonrpcRequest, params ToolCallParams) error {
	id, _ := params.Arguments["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return s.writeError(w, req.ID, codeInvalidRequest, "memory.forget: 'id' argument is required and cannot be empty")
	}
	scopeLevel := "worktree"
	if raw, ok := params.Arguments["scope"].(string); ok && raw != "" {
		scopeLevel = raw
	}
	if !s.scopeAllowed(scopeLevel) {
		return s.writeError(w, req.ID, codeWriteForbidden, fmt.Sprintf("memory.forget: scope %q is outside the server's allowed_scopes list", scopeLevel))
	}
	if !s.checkRateLimit() {
		return s.writeError(w, req.ID, codeWriteForbidden, "memory.forget: rate limit exceeded; retry shortly")
	}
	reason, _ := params.Arguments["reason"].(string)
	if reason == "" {
		reason = "mcp client forget"
	}
	fmt.Fprintf(os.Stderr, "bough-mcp-server: memory.forget: scope=%s id=%s reason=%q\n",
		scopeLevel, id, reason)
	if _, err := s.backend.Forget(ctx, &memapi.ForgetReq{
		ID:     id,
		Scope:  memapi.Scope{Level: scopeLevel},
		Reason: reason,
	}); err != nil {
		return s.writeResult(w, req.ID, ToolCallResult{
			Content: []ToolCallContent{{Type: "text", Text: fmt.Sprintf("memory.forget failed: %v", err)}},
			IsError: true,
		})
	}
	s.appendAudit(map[string]any{
		"op":     "memory.forget",
		"scope":  scopeLevel,
		"id":     id,
		"reason": reason,
	})
	return s.writeResult(w, req.ID, ToolCallResult{
		Content: []ToolCallContent{{Type: "text", Text: fmt.Sprintf("forgotten: id=%s reason=%q", id, reason)}},
	})
}

// computeMemoryDedupeKey mirrors internal/instinct.DedupeKey for the
// MCP server's write path. The two must stay in sync — the SQLite
// reference-fallback uses this hash as the row primary key, so any
// drift between the coordinator (= ingest / mint paths) and the
// MCP server would silently break dedupe across the two entry
// points. v0.7 lifts the helper into pkg/dedupe so the two callers
// share one definition; until then this inline copy ships with the
// rule "same input, same hash" comment block as the canonical
// crosscheck.
//
//	canonical input = lower(trim(rule)) | scope.Level | scope.WorktreeID | scope.RepoName
//	hash            = sha256
func computeMemoryDedupeKey(rule string, scope memapi.Scope) string {
	h := sha256.New()
	h.Write([]byte(strings.ToLower(strings.TrimSpace(rule))))
	h.Write([]byte("|"))
	h.Write([]byte(scope.Level))
	h.Write([]byte("|"))
	h.Write([]byte(scope.WorktreeID))
	h.Write([]byte("|"))
	h.Write([]byte(scope.RepoName))
	return hex.EncodeToString(h.Sum(nil))
}

// handleResourcesList exposes the static set of resource URIs the
// server publishes. v0.6 only lists scopes; v0.6.x will add
// per-scope resource entries.
func (s *Server) handleResourcesList(w io.Writer, req jsonrpcRequest) error {
	resources := []ResourceDescriptor{
		{
			URI:         "bough://memory/scopes",
			Name:        "Memory Scopes",
			Description: "List of scopes the configured bough memory backend holds.",
			MimeType:    "application/json",
		},
	}
	return s.writeResult(w, req.ID, ResourcesListResult{Resources: resources})
}

// handleResourcesRead serves the body for a known resource URI.
// Unknown URIs respond with Method Not Found.
func (s *Server) handleResourcesRead(ctx context.Context, w io.Writer, req jsonrpcRequest) error {
	var params ResourcesReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.writeError(w, req.ID, codeInvalidRequest, fmt.Sprintf("params: %v", err))
	}
	switch params.URI {
	case "bough://memory/scopes":
		return s.readScopesResource(ctx, w, req, params.URI)
	default:
		return s.writeError(w, req.ID, codeMethodNotFound, fmt.Sprintf("resource %q not registered", params.URI))
	}
}

// readScopesResource returns the static list of scope tiers the
// host honours. v0.6 deliberately does NOT include per-scope row
// counts (review #23 #6): producing a faithful count from a
// scope-level-only query requires the host to know its configured
// RepoName / WorktreeID, which neither the MCP server nor the
// backend's scopeID encoding can synthesise reliably. A "count" the
// host cannot guarantee would mislead operators when a half-broken
// backend silently degrades to zero. v0.6.x adds an explicit count
// API once Server learns about cfg.Repositories.
func (s *Server) readScopesResource(_ context.Context, w io.Writer, req jsonrpcRequest, uri string) error {
	body := map[string]any{
		"scopes": []map[string]string{
			{"level": "worktree", "description": "per-branch memory; tied to a specific worktree"},
			{"level": "repo", "description": "repo-tier memory shared across worktrees of the same repository"},
			{"level": "global", "description": "user-global memory that follows the operator across repositories"},
		},
		"note": "v0.6 exposes the scope tier list only. Use memory.query with scope='<level>' from a host that knows its repo / worktree identity to count rows.",
	}
	raw, _ := json.MarshalIndent(body, "", "  ")
	return s.writeResult(w, req.ID, ResourcesReadResult{
		Contents: []ResourceContent{{URI: uri, MimeType: "application/json", Text: string(raw)}},
	})
}

// writeResult serialises a JSON-RPC success response. Newline
// framing matches the wire convention.
func (s *Server) writeResult(w io.Writer, id json.RawMessage, result any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	resp := jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: result}
	raw, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("mcp marshal: %w", err)
	}
	return writeLine(w, raw)
}

// writeError serialises a JSON-RPC error response.
func (s *Server) writeError(w io.Writer, id json.RawMessage, code int, message string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonrpcError{Code: code, Message: message},
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("mcp marshal err: %w", err)
	}
	return writeLine(w, raw)
}

// writeLine appends a newline and writes atomically.
func writeLine(w io.Writer, payload []byte) error {
	if _, err := w.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("mcp write: %w", err)
	}
	return nil
}

// The round 4 AI #1 zombie-process guard runs INSIDE Server.Run:
// the bufio.Scanner returns false when stdin closes, Run returns,
// and the caller (cmd/bough-mcp-server/main.go) invokes Shutdown
// from a deferred call which kills the MemoryBackend subprocess.
//
// Earlier drafts spawned a parallel goroutine reading os.Stdin to
// detect EOF, but that goroutine raced Run's scanner for bytes and
// truncated every JSON-RPC frame mid-flight (review #23 #2 / #3).
// Run's own scanner.Scan() loop is the single reader; EOF detection
// happens there for free.
