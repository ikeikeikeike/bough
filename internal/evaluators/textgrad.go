package evaluators

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/ikeikeikeike/bough/plugins/evaluator/api"
)

// TextGrad implements the gradient evaluator strategy: take the
// current artifact + its prior version, compute a token-level
// "gradient" (= Jaccard distance over the steps/constraints sets),
// and recommend promote / keep / revise based on the signed
// magnitude.
//
// The "gradient" idea (Yuksekgonul et al., 2024) frames LLM
// optimisation as gradient descent over text. bough's evaluator
// surface only needs the score; the upstream paper's iterative
// loop is the operator's call to make.
type TextGrad struct{}

// Name returns the canonical evaluator name.
func (TextGrad) Name() string { return "textgrad" }

// textgradContext is the shape EvaluatorContextJSON carries for
// the TextGrad evaluator. Prior is the previous version's
// description + steps + constraints concatenated; bough host
// fetches it from the memory backend before invoking.
type textgradContext struct {
	Prior string `json:"prior"`
}

// Evaluate decision:
//
//	gradient > 0.7   → promote (= major divergence in good direction
//	                   if utility floor satisfied)
//	0.3 ≤ grad ≤ 0.7 → keep
//	gradient < 0.3   → revise (= negligible change, drift not signal)
//	no prior         → keep (= cannot compute gradient, defer)
func (t TextGrad) Evaluate(_ context.Context, req *api.EvaluateReq) (*api.EvaluateResp, error) {
	a, err := decodeArtifact(req.ArtifactJSON)
	if err != nil {
		return nil, fmt.Errorf("textgrad: decode artifact: %w", err)
	}
	var ctx textgradContext
	if len(req.EvaluatorContextJSON) > 0 {
		_ = json.Unmarshal(req.EvaluatorContextJSON, &ctx)
	}
	currText := strings.Join(append([]string{a.Description}, append(a.Steps, a.Constraints...)...), "\n")
	if ctx.Prior == "" {
		return &api.EvaluateResp{
			Outcome:     api.OutcomeKeep,
			Utility:     0.5,
			Explanation: "no prior version supplied; cannot compute gradient — keep",
		}, nil
	}
	grad := jaccardDistance(tokenSet(currText), tokenSet(ctx.Prior))
	var outcome api.EvaluationOutcome
	var explanation string
	var utility float64
	switch {
	case grad > 0.7:
		outcome = api.OutcomePromote
		utility = 0.85
		explanation = fmt.Sprintf("gradient %.2f > 0.7 → major signal, promote", grad)
	case grad >= 0.3:
		outcome = api.OutcomeKeep
		utility = 0.6
		explanation = fmt.Sprintf("gradient %.2f in [0.3, 0.7] → keep", grad)
	default:
		outcome = api.OutcomeRevise
		utility = 0.3
		explanation = fmt.Sprintf("gradient %.2f < 0.3 → negligible change, revise", grad)
	}
	payload, _ := json.Marshal(struct {
		Gradient float64 `json:"gradient"`
		HasPrior bool    `json:"has_prior"`
	}{Gradient: grad, HasPrior: true})
	return &api.EvaluateResp{
		Outcome:              outcome,
		Utility:              utility,
		ConfidenceDelta:      utility - a.Confidence,
		Explanation:          explanation,
		EvaluatorPayloadJSON: payload,
	}, nil
}

func tokenSet(s string) map[string]struct{} {
	out := map[string]struct{}{}
	lower := strings.ToLower(s)
	cur := strings.Builder{}
	flush := func() {
		w := cur.String()
		cur.Reset()
		if len(w) < 2 {
			return
		}
		out[w] = struct{}{}
	}
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func jaccardDistance(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return 1.0 - float64(inter)/float64(union)
}
