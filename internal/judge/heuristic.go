// Package judge implements the three JudgeClient backends behind the
// plugins/capability/api/llm.go interface: HeuristicJudgeClient (no
// LLM, deterministic), ReplayJudgeClient (fixture playback), and a
// ClaudeJudgeClient stub deferred to v0.7.2.
//
// The HeuristicJudgeClient is the v0.7.1 default because round 5
// reviewers asked the safety floor to ship without an LLM call. It
// uses cluster size, hash diversity, and prior-label proximity to
// decide PASS / DOUBT / FAIL with bounded confidence.
package judge

import (
	"context"
	"fmt"
	"strings"
	"time"

	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// HeuristicJudgeClient evaluates clusters without calling any LLM.
// It is deterministic by construction and serves as the default for
// CI runs, offline machines, and cost-sensitive deployments.
//
// Decision tree:
//
//	cluster size >= 3 + hash diversity >= 2 → PASS  (confidence 0.7-0.9)
//	cluster size == 2 + hashes differ       → DOUBT (confidence 0.5)
//	cluster size == 1                       → DOUBT (singleton, 0.4)
//	all members share the same hash         → FAIL  (dup, 0.95)
//	cluster size == 0                       → FAIL  (empty)
//
// When NearestPriorLabel is set the heuristic prefers to reuse it
// (= ReusePriorLabel: true) over recommending a fresh name. This
// keeps the memory backend label space stable across runs.
type HeuristicJudgeClient struct {
	now func() time.Time
}

// NewHeuristicJudgeClient returns a HeuristicJudgeClient. The clock
// can be overridden for tests via SetClock.
func NewHeuristicJudgeClient() *HeuristicJudgeClient {
	return &HeuristicJudgeClient{now: time.Now}
}

// SetClock overrides the time source used to stamp JudgeVerdict
// timestamps. Tests pin this to a fixture value so golden diffs
// stay byte-stable.
func (h *HeuristicJudgeClient) SetClock(now func() time.Time) {
	h.now = now
}

// Name returns the canonical backend name used in CLI flags and
// audit records.
func (h *HeuristicJudgeClient) Name() string { return "heuristic" }

// Judge applies the decision tree documented on HeuristicJudgeClient.
// The Reason field embeds the count + diversity numbers so an
// operator reading `.evolve/judgements/<key>.json` can trace why a
// verdict landed where it did.
func (h *HeuristicJudgeClient) Judge(_ context.Context, req api.JudgeRequest) (api.JudgeVerdict, error) {
	ts := h.now().UTC().Format(time.RFC3339Nano)
	size := len(req.ClusterMemberIDs)
	diversity := uniqueCount(req.ClusterMemberHashes)
	reusePrior := req.NearestPriorLabel != ""

	verdict := api.JudgeVerdict{
		ReusePriorLabel: reusePrior,
		TimestampUTC:    ts,
	}
	if reusePrior {
		verdict.RecommendedLabel = req.NearestPriorLabel
	}

	switch {
	case size == 0:
		verdict.Verdict = api.VerdictFail
		verdict.Confidence = 0.95
		verdict.Reason = "empty cluster: no instinct members to promote"
	case diversity == 1 && size > 1:
		verdict.Verdict = api.VerdictFail
		verdict.Confidence = 0.95
		verdict.Reason = fmt.Sprintf("duplicate cluster: %d members share the same hash", size)
	case size == 1:
		verdict.Verdict = api.VerdictDoubt
		verdict.Confidence = 0.4
		verdict.Reason = "singleton cluster: insufficient evidence to promote without LLM judge"
	case size == 2:
		verdict.Verdict = api.VerdictDoubt
		verdict.Confidence = 0.5
		verdict.Reason = "pair cluster: borderline sample, surfaces for operator review"
	case size >= 3 && diversity >= 2:
		conf := 0.7 + 0.05*float64(min(size, 6)-3)
		if conf > 0.9 {
			conf = 0.9
		}
		verdict.Verdict = api.VerdictPass
		verdict.Confidence = conf
		verdict.Reason = fmt.Sprintf("triplet+ cluster with %d distinct hashes across %d members", diversity, size)
	default:
		verdict.Verdict = api.VerdictDoubt
		verdict.Confidence = 0.45
		verdict.Reason = fmt.Sprintf("cluster size=%d diversity=%d: heuristic uncertain", size, diversity)
	}

	if verdict.RecommendedLabel == "" {
		verdict.RecommendedLabel = synthesisLabel(req)
	}
	return verdict, nil
}

func uniqueCount(xs []string) int {
	seen := map[string]struct{}{}
	for _, x := range xs {
		seen[x] = struct{}{}
	}
	return len(seen)
}

// synthesisLabel returns a deterministic fallback label when the
// heuristic has no prior to reuse. The shape mirrors the upstream
// ECC convention (`heuristic_<modelid_short>_<promptver_short>`)
// so audit records stay greppable.
func synthesisLabel(req api.JudgeRequest) string {
	model := shortToken(req.ModelID, 16)
	prompt := shortToken(req.PromptVersion, 12)
	return fmt.Sprintf("heuristic_%s_%s", model, prompt)
}

func shortToken(s string, max int) string {
	s = strings.ReplaceAll(s, " ", "_")
	if len(s) <= max {
		return s
	}
	return s[:max]
}
