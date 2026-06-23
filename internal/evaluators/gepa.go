package evaluators

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/ikeikeikeike/bough/plugins/evaluator/api"
)

// GEPA implements the reflective prompt optimiser strategy: read
// the artifact text and flag scope creep, redundancy, and over-
// generalisation. Recommends revise when any anti-pattern fires;
// keep when the text is well-scoped.
//
// The strategy is the research idea (Reflexion / GEPA loop) lifted
// into bough's evaluator surface. The heuristic is intentionally
// shallow: signals that the upstream paper shows are reliable
// proxies for "this skill / instinct is too vague to be useful".
type GEPA struct{}

// Name returns the canonical evaluator name. Used by `bough
// evaluate --evaluator <name>`.
func (GEPA) Name() string { return "gepa" }

// MaxArtifactTokensBeforeReviseGEPA fires the revise verdict when
// the artifact text exceeds this token count. The threshold mirrors
// the upstream GEPA paper's "too long → distill" heuristic (~ 800
// tokens normalised to plain-text word count).
const MaxArtifactTokensBeforeReviseGEPA = 800

// VaguenessTokensGEPA are the words that mark an over-generalised
// rule. A single occurrence is fine; ≥ 3 across the artifact text
// is the threshold.
var VaguenessTokensGEPA = []string{
	"often", "sometimes", "usually", "generally", "typically",
	"in some cases", "in many cases", "it depends",
}

// Evaluate runs the GEPA decision tree:
//
//	tokens > MaxArtifactTokensBeforeReviseGEPA  → revise (compress)
//	vagueness tokens ≥ 3                        → revise (tighten)
//	artifact has no Constraints / Inputs        → revise (specify)
//	otherwise                                   → keep
func (g GEPA) Evaluate(_ context.Context, req *api.EvaluateReq) (*api.EvaluateResp, error) {
	a, err := decodeArtifact(req.ArtifactJSON)
	if err != nil {
		return nil, fmt.Errorf("gepa: decode artifact: %w", err)
	}
	text := strings.Join(append(append([]string{a.Description}, a.Steps...), a.Constraints...), "\n")
	tokens := approxTokens(text)
	vag := countVagueness(text)
	reasons := []string{}
	outcome := api.OutcomeKeep
	utility := 0.7

	if tokens > MaxArtifactTokensBeforeReviseGEPA {
		outcome = api.OutcomeRevise
		utility = 0.4
		reasons = append(reasons, fmt.Sprintf("token count %d > %d (= compress)", tokens, MaxArtifactTokensBeforeReviseGEPA))
	}
	if vag >= 3 {
		outcome = api.OutcomeRevise
		utility = 0.35
		reasons = append(reasons, fmt.Sprintf("vague token count = %d (≥ 3 → tighten)", vag))
	}
	if len(a.Constraints) == 0 && len(a.Inputs) == 0 {
		outcome = api.OutcomeRevise
		if utility > 0.45 {
			utility = 0.45
		}
		reasons = append(reasons, "no Constraints + no Inputs declared (= over-general)")
	}
	if outcome == api.OutcomeKeep {
		reasons = append(reasons, fmt.Sprintf("well-scoped (tokens=%d, vague=%d)", tokens, vag))
	}
	payload, _ := json.Marshal(struct {
		Tokens          int      `json:"tokens"`
		VaguenessHits   int      `json:"vagueness_hits"`
		AppliedSignals  []string `json:"applied_signals"`
	}{Tokens: tokens, VaguenessHits: vag, AppliedSignals: reasons})
	return &api.EvaluateResp{
		Outcome:              outcome,
		Utility:              utility,
		ConfidenceDelta:      utility - a.Confidence,
		Explanation:          strings.Join(reasons, "; "),
		EvaluatorPayloadJSON: payload,
	}, nil
}

func approxTokens(s string) int {
	if s == "" {
		return 0
	}
	count := 0
	inToken := false
	for _, r := range s {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if isLetter {
			if !inToken {
				count++
				inToken = true
			}
		} else {
			inToken = false
		}
	}
	return count
}

func countVagueness(s string) int {
	lower := strings.ToLower(s)
	n := 0
	for _, tok := range VaguenessTokensGEPA {
		n += strings.Count(lower, tok)
	}
	return n
}
