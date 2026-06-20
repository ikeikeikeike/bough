package mem0

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// This file holds the HTTP transport layer (Ν-1.1d) the mem0
// adapter uses to speak the mem0 v1 REST API. doJSON is the single
// entry point every per-RPC method routes through so authentication,
// content-type negotiation, and error decoding stay consistent.
//
// Round 4 AI #1 noted that transparent retry is risky on writes
// without idempotency tokens. The host computes DedupeKey +
// SourceEventID; mem0 does not natively honour them (its Capabilities
// advertise reflects that). doJSON therefore intentionally does NOT
// retry — the caller layer decides whether a 5xx is replayable based
// on the request shape, which keeps Store on the fail-fast policy
// the CONTRACT.md prescribes.

// mem0 wire types ---------------------------------------------------

// mem0Message mirrors mem0's chat-formatted memory payload. bough
// always feeds a single "user" role message holding the rule body —
// mem0's intent extraction collapses that into a memory record.
type mem0Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// mem0AddReq is the body for POST /api/v1/memories/. user_id is
// the long-lived identity; session_id, when non-empty, scopes the
// memory to a worktree branch.
type mem0AddReq struct {
	Messages  []mem0Message  `json:"messages"`
	UserID    string         `json:"user_id"`
	SessionID string         `json:"session_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// mem0Memory is a row mem0 returns from add / search / list calls.
// Score is populated only by search responses; CreatedAt comes from
// list responses. Metadata round-trips bough's Instinct fields the
// host already serialised on Store.
type mem0Memory struct {
	ID        string         `json:"id"`
	Memory    string         `json:"memory"`
	UserID    string         `json:"user_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	Score     float64        `json:"score,omitempty"`
	Event     string         `json:"event,omitempty"` // ADD / UPDATE / DELETE / NONE
}

// mem0SearchReq is the body for POST /api/v1/memories/search/.
type mem0SearchReq struct {
	Query     string `json:"query"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// mem0Results is the shape every list-ish endpoint returns
// (add / search / get-all). Keeping it generic lets the per-method
// caller decode once and walk Results.
type mem0Results struct {
	Results []mem0Memory `json:"results"`
}

// HTTP transport ----------------------------------------------------

// doJSON marshals body (nil for GET / DELETE), wires the API key
// when configured, fires the request through the Provider's
// http.Client, and either decodes the response JSON into respBody
// or returns a structured error.
//
// HTTP 204 / a nil respBody both skip JSON decode. Non-2xx is
// converted into an error that includes the upstream body so the
// caller has a chance at diagnosing a misconfigured mem0 instance.
func (p *Provider) doJSON(ctx context.Context, method, path string, body, respBody any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("mem0 doJSON marshal %s: %w", path, err)
		}
		reader = bytes.NewReader(raw)
	}
	url := strings.TrimRight(p.endpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return fmt.Errorf("mem0 doJSON build %s: %w", path, err)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Token "+p.apiKey)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("mem0 %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mem0 %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if respBody == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(respBody); err != nil && err != io.EOF {
		return fmt.Errorf("mem0 %s %s decode: %w", method, path, err)
	}
	return nil
}

// estimateTokens mirrors the sqlite reference-fallback's heuristic
// so the host's RetrieveBudget aggregator behaves identically
// across backends. ~4 chars per English token, minimum 1.
func estimateTokens(rule, why, howToApply string) int {
	n := (len(rule) + len(why) + len(howToApply)) / 4
	if n < 1 {
		n = 1
	}
	return n
}
