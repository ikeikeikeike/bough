package api

import "context"

// This file lifts the JudgeClient contract out into plugins/capability/
// api/ so v0.8+ can graduate it into a gRPC plugin slot without
// rewriting the evolve pipeline. v0.7.1 keeps the three reference
// implementations inside the bough core (internal/judge/), but the
// *interface* lives here so a future
// `bough-plugin-capability-judge-<vendor>` binary can satisfy the same
// shape over the wire (round 5 reviewer non-negotiable: the LLM
// surface must be swappable per deployment).

// VerdictKind narrows the JudgeVerdict.Verdict field to the canonical
// three states. PASS = promote into memory backend as state=candidate.
// DOUBT = surface in `bough doctor` for operator review. FAIL = drop.
type VerdictKind string

const (
	VerdictPass  VerdictKind = "PASS"
	VerdictDoubt VerdictKind = "DOUBT"
	VerdictFail  VerdictKind = "FAIL"
)

// JudgeRequest is the deterministic input a JudgeClient receives.
// The cache key is sha256(PromptVersion | ModelID | ClusterMemberIDs |
// ClusterMemberHashes | NearestPriorLabel | NearestPriorDescription)
// so two identical requests produce identical verdicts.
//
// Temperature is enforced to 0 by callers — the field is here for
// audit replay; do not parameterise it.
type JudgeRequest struct {
	PromptVersion       string   `json:"prompt_version"`
	ModelID             string   `json:"model_id"`
	ClusterMemberIDs    []string `json:"cluster_member_ids"`
	ClusterMemberHashes []string `json:"cluster_member_hashes"`
	NearestPriorLabel   string   `json:"nearest_prior_label,omitempty"`
	NearestPriorDesc    string   `json:"nearest_prior_description,omitempty"`
	Temperature         float64  `json:"temperature"`
	MaxOutputTokens     int      `json:"max_output_tokens"`
}

// JudgeVerdict is the structured output a JudgeClient returns.
// The evolve pipeline JSON-schema-validates this shape before
// promoting the cluster into the memory backend.
type JudgeVerdict struct {
	Verdict          VerdictKind `json:"verdict"`
	Confidence       float64     `json:"confidence"`
	Reason           string      `json:"reason"`
	RecommendedLabel string      `json:"recommended_label,omitempty"`
	ReusePriorLabel  bool        `json:"reuse_prior_label"`
	CostEstimateUSD  float64     `json:"cost_estimate_usd"`
	TimestampUTC     string      `json:"timestamp_utc"`
}

// JudgeClient evaluates an instinct cluster and emits a verdict.
//
// All implementations must be deterministic for cache hits: the same
// JudgeRequest must produce the same JudgeVerdict on every call.
// Implementations that wrap a live LLM (ClaudeJudgeClient) achieve
// this by enforcing temperature=0 + max_output_tokens fixed. The
// HeuristicJudgeClient is pure function. The ReplayJudgeClient
// replays prerecorded fixtures.
//
// Name() must return the same string the operator passes to
// `bough bootstrap --judge <name>`. Reserved names: "claude",
// "heuristic", "replay".
type JudgeClient interface {
	Name() string
	Judge(ctx context.Context, req JudgeRequest) (JudgeVerdict, error)
}
