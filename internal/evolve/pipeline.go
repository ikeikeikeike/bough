package evolve

import (
	"context"
	"fmt"
	"time"

	"github.com/ikeikeikeike/bough/pkg/schema"
	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// Pipeline orchestrates the 4 gates plus the LLM judge step between
// Gate 3 and Gate 4. The order mirrors ECC Python v3:
//
//	Gate 1 schema   → drop malformed
//	Gate 2 heuristic→ drop low-quality
//	Gate 3 cluster  → group similar
//	LLM judge       → PASS / DOUBT / FAIL per cluster
//	Gate 4 candidate→ stamp + emit state=candidate Instincts
//
// PromptVersion is the prompts/<version>.txt key the judge cache
// includes in its SHA256 — bump it every time the prompt template
// or model id changes.
//
// Now is overridable from tests to keep golden diff byte-stable.
type Pipeline struct {
	Judge         api.JudgeClient
	Priors        []PriorLabel
	PromptVersion string
	ModelID       string
	Now           func() time.Time
}

// Result aggregates the per-batch outcome the audit log + bough
// doctor surface together. Dropped/Clustered/Verdicts/Candidates
// counts let the operator see at a glance how many observations
// became instincts vs were filtered out where.
type Result struct {
	InputCount     int
	Dropped        int
	Clusters       int
	Verdicts       map[api.VerdictKind]int
	Candidates     []schema.InstinctCandidate
	GateErrors     []GateError
	PerClusterAudit []ClusterAudit
}

// GateError records a TraceBundle that failed Gate 1 or Gate 2 so
// the audit dir can surface what was dropped and why.
type GateError struct {
	BundleID string
	Gate     string
	Reason   string
}

// ClusterAudit pairs a Cluster with the verdict the LLM judge
// returned for it, so the audit dir captures the full decision
// trail per cluster.
type ClusterAudit struct {
	ClusterID string
	Size      int
	Verdict   api.JudgeVerdict
}

// Run executes the full pipeline over the input batch and returns
// the aggregated Result. Always returns a non-nil Result; the
// Candidates slice may be empty if every cluster verdict was FAIL.
func (p Pipeline) Run(ctx context.Context, bundles []schema.TraceBundle) (Result, error) {
	if p.Judge == nil {
		return Result{}, fmt.Errorf("evolve.Pipeline.Run: Judge is nil")
	}
	if p.PromptVersion == "" {
		return Result{}, fmt.Errorf("evolve.Pipeline.Run: PromptVersion is empty (= cache key would collide across runs)")
	}
	if p.ModelID == "" {
		return Result{}, fmt.Errorf("evolve.Pipeline.Run: ModelID is empty (= cache key would collide across runs)")
	}
	now := p.Now
	if now == nil {
		now = time.Now
	}

	result := Result{
		InputCount: len(bundles),
		Verdicts:   map[api.VerdictKind]int{},
	}

	// Gate 1 + Gate 2 sweep.
	survivors := bundles[:0:0]
	for _, b := range bundles {
		if err := Gate1Schema(b); err != nil {
			result.Dropped++
			result.GateErrors = append(result.GateErrors, GateError{BundleID: b.ID, Gate: "gate1_schema", Reason: err.Error()})
			continue
		}
		hv := Gate2Heuristic(b)
		if !hv.Pass {
			result.Dropped++
			result.GateErrors = append(result.GateErrors, GateError{BundleID: b.ID, Gate: "gate2_heuristic", Reason: hv.Reason})
			continue
		}
		survivors = append(survivors, b)
	}

	// Gate 3 clustering.
	clusters := Gate3Cluster(survivors, p.Priors)
	result.Clusters = len(clusters)

	// LLM judge + Gate 4.
	for _, c := range clusters {
		req := buildJudgeRequest(c, p.PromptVersion, p.ModelID)
		verdict, err := p.Judge.Judge(ctx, req)
		if err != nil {
			// Treat judge errors as DOUBT so the operator can
			// review rather than silently dropping evidence.
			verdict = api.JudgeVerdict{
				Verdict:    api.VerdictDoubt,
				Confidence: 0.3,
				Reason:     fmt.Sprintf("judge error fell through to DOUBT: %v", err),
			}
		}
		result.Verdicts[verdict.Verdict]++
		result.PerClusterAudit = append(result.PerClusterAudit, ClusterAudit{
			ClusterID: c.ID,
			Size:      len(c.Members),
			Verdict:   verdict,
		})
		candidates := Gate4Candidate(c, verdict, now())
		result.Candidates = append(result.Candidates, candidates...)
	}
	return result, nil
}

func buildJudgeRequest(c Cluster, promptVer, modelID string) api.JudgeRequest {
	ids := make([]string, 0, len(c.Members))
	for _, m := range c.Members {
		ids = append(ids, m.ID)
	}
	return api.JudgeRequest{
		PromptVersion:       promptVer,
		ModelID:             modelID,
		ClusterMemberIDs:    ids,
		ClusterMemberHashes: c.MemberHashes,
		NearestPriorLabel:   c.NearestPriorLabel,
		NearestPriorDesc:    c.NearestPriorDesc,
		Temperature:         0,
		MaxOutputTokens:     1024,
	}
}
