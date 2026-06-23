package judge

import (
	"context"
	"errors"
	"fmt"
	"os"

	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// ClaudeJudgeClient is the live LLM-backed JudgeClient. v0.7.1 ships
// the surface (config wiring, audit format, cost meter contract) but
// keeps the Anthropic SDK integration deferred to v0.7.2 so the
// safety-first v0.7.1 release ships without adding a vendor SDK to
// the dependency tree.
//
// Operators who want a live judge on v0.7.1 can either:
//   - set the backend to "heuristic" (= default) and accept the
//     deterministic rule-based pipeline, or
//   - run their own external compile of bough that wires
//     anthropic-sdk-go and overrides the Judge() body.
//
// v0.7.2 lands the Anthropic SDK + rate-limit + retry harness, at
// which point this stub becomes the production wiring.
type ClaudeJudgeClient struct {
	apiKey  string
	modelID string
}

// ErrClaudeNotWired is returned by Judge until v0.7.2 lands the
// Anthropic SDK integration. The error is informational so a
// pipeline can fall through to HeuristicJudgeClient cleanly.
var ErrClaudeNotWired = errors.New("ClaudeJudgeClient: live API integration deferred to v0.7.2 — use 'heuristic' or 'replay' backend in v0.7.1")

// NewClaudeJudgeClient returns a ClaudeJudgeClient. The API key is
// read from the ANTHROPIC_API_KEY environment variable so the value
// never lands in audit records or .bough.yaml.
//
// v0.7.1 stub: the constructor accepts the model ID but Judge()
// returns ErrClaudeNotWired until v0.7.2 wires Anthropic SDK.
func NewClaudeJudgeClient(modelID string) *ClaudeJudgeClient {
	return &ClaudeJudgeClient{
		apiKey:  os.Getenv("ANTHROPIC_API_KEY"),
		modelID: modelID,
	}
}

// Name returns "claude" — the canonical backend name used in CLI
// flags and audit records.
func (c *ClaudeJudgeClient) Name() string { return "claude" }

// Judge will hit the Anthropic Messages API once v0.7.2 lands the
// SDK. Until then it returns ErrClaudeNotWired so the pipeline
// caller can decide to fall through to a deterministic backend.
func (c *ClaudeJudgeClient) Judge(_ context.Context, _ api.JudgeRequest) (api.JudgeVerdict, error) {
	if c.apiKey == "" {
		return api.JudgeVerdict{}, fmt.Errorf("%w (ANTHROPIC_API_KEY also unset)", ErrClaudeNotWired)
	}
	return api.JudgeVerdict{}, ErrClaudeNotWired
}
