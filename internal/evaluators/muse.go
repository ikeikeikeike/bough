package evaluators

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	api "github.com/ikeikeikeike/bough/plugins/evaluator/api"
)

// MUSE implements the autoskill lifecycle evaluator strategy: track
// per-artifact usage signals (last-used timestamp, hit count, recent
// regression count) and recommend prune when the artifact has
// gone stale or carries a regression history.
//
// The strategy is the MUSE-Autoskill paper's lifecycle idea:
// skills that nobody invokes + skills that recently failed are the
// most-likely candidates for removal. Surviving skills accumulate
// utility from continued use.
type MUSE struct{}

// Name returns the canonical evaluator name.
func (MUSE) Name() string { return "muse" }

// museContext is the shape EvaluatorContextJSON carries. The host
// pulls these signals from the memory backend's audit log before
// invoking.
type museContext struct {
	HitCount     int    `json:"hit_count"`
	RegressionN  int    `json:"regression_count"`
	LastUsedUTC  string `json:"last_used_utc"`
	NowUTC       string `json:"now_utc"`
}

// MUSEStaleDays is the recency threshold beyond which an artifact
// with zero recent hits is considered stale and prunable.
const MUSEStaleDays = 90

// Evaluate decision:
//
//	regression_count ≥ 3                         → prune (= broken)
//	hit_count == 0 AND last_used > 90 days ago   → prune (= stale)
//	hit_count ≥ 10 AND no recent regressions     → promote (= valuable)
//	otherwise                                    → keep
func (m MUSE) Evaluate(_ context.Context, req *api.EvaluateReq) (*api.EvaluateResp, error) {
	a, err := decodeArtifact(req.ArtifactJSON)
	if err != nil {
		return nil, fmt.Errorf("muse: decode artifact: %w", err)
	}
	var ctx museContext
	if len(req.EvaluatorContextJSON) > 0 {
		_ = json.Unmarshal(req.EvaluatorContextJSON, &ctx)
	}
	now := parseTimeOrNow(ctx.NowUTC)
	last := parseTimeOrZero(ctx.LastUsedUTC)
	daysSince := -1
	if !last.IsZero() {
		daysSince = int(now.Sub(last).Hours() / 24)
	}

	outcome := api.OutcomeKeep
	utility := 0.6
	reasons := []string{}

	switch {
	case ctx.RegressionN >= 3:
		outcome = api.OutcomePrune
		utility = 0.1
		reasons = append(reasons, fmt.Sprintf("regression count %d ≥ 3 → prune", ctx.RegressionN))
	case ctx.HitCount == 0 && daysSince > MUSEStaleDays:
		outcome = api.OutcomePrune
		utility = 0.15
		reasons = append(reasons, fmt.Sprintf("no hits + last used %d days ago > %d → stale prune", daysSince, MUSEStaleDays))
	case ctx.HitCount >= 10 && ctx.RegressionN == 0:
		outcome = api.OutcomePromote
		utility = 0.9
		reasons = append(reasons, fmt.Sprintf("hit_count %d ≥ 10 + no regressions → promote", ctx.HitCount))
	default:
		reasons = append(reasons, fmt.Sprintf("hit_count=%d regression=%d days_since=%d → keep", ctx.HitCount, ctx.RegressionN, daysSince))
	}
	payload, _ := json.Marshal(struct {
		HitCount         int    `json:"hit_count"`
		RegressionCount  int    `json:"regression_count"`
		DaysSinceLastUse int    `json:"days_since_last_use"`
		Verdict          string `json:"verdict"`
	}{ctx.HitCount, ctx.RegressionN, daysSince, string(outcome)})
	return &api.EvaluateResp{
		Outcome:              outcome,
		Utility:              utility,
		ConfidenceDelta:      utility - a.Confidence,
		ShouldPrune:          outcome == api.OutcomePrune,
		Explanation:          joinReasons(reasons),
		EvaluatorPayloadJSON: payload,
	}, nil
}

func parseTimeOrNow(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now().UTC()
}

func parseTimeOrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func joinReasons(rs []string) string {
	out := ""
	for i, r := range rs {
		if i > 0 {
			out += "; "
		}
		out += r
	}
	return out
}
