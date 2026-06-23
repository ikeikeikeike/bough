package evaluators

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	api "github.com/ikeikeikeike/bough/plugins/evaluator/api"
)

func mkArtifactJSON(t *testing.T, desc string, steps, constraints []string, conf float64) []byte {
	t.Helper()
	buf, err := json.Marshal(map[string]interface{}{
		"Name":        "test-artifact",
		"Description": desc,
		"Steps":       steps,
		"Constraints": constraints,
		"Confidence":  conf,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return buf
}

func TestGEPA_WellScopedKeeps(t *testing.T) {
	req := &api.EvaluateReq{ArtifactJSON: mkArtifactJSON(t,
		"Tight rule with clear scope",
		[]string{"step one"},
		[]string{"only when on PostToolUse"},
		0.7,
	)}
	resp, err := GEPA{}.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("GEPA: %v", err)
	}
	if resp.Outcome != api.OutcomeKeep {
		t.Errorf("Outcome = %q, want keep (= %+v)", resp.Outcome, resp)
	}
}

func TestGEPA_OvergeneralRevises(t *testing.T) {
	req := &api.EvaluateReq{ArtifactJSON: mkArtifactJSON(t,
		"This rule is often useful, usually applies, generally in many cases.",
		nil, nil, 0.5,
	)}
	resp, err := GEPA{}.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("GEPA: %v", err)
	}
	if resp.Outcome != api.OutcomeRevise {
		t.Errorf("Outcome = %q, want revise (= overgeneral)", resp.Outcome)
	}
}

func TestTextGrad_NoPriorKeeps(t *testing.T) {
	req := &api.EvaluateReq{
		ArtifactJSON: mkArtifactJSON(t, "x", []string{"y"}, []string{"z"}, 0.6),
	}
	resp, err := TextGrad{}.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("TextGrad: %v", err)
	}
	if resp.Outcome != api.OutcomeKeep {
		t.Errorf("Outcome = %q, want keep", resp.Outcome)
	}
}

func TestTextGrad_LargeDivergencePromotes(t *testing.T) {
	priorBuf, _ := json.Marshal(textgradContext{Prior: "completely different prior text alphabet beta gamma"})
	req := &api.EvaluateReq{
		ArtifactJSON:         mkArtifactJSON(t, "new shape entirely", []string{"different step"}, []string{"different constraint"}, 0.6),
		EvaluatorContextJSON: priorBuf,
	}
	resp, err := TextGrad{}.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("TextGrad: %v", err)
	}
	if resp.Outcome != api.OutcomePromote {
		t.Errorf("Outcome = %q, want promote (gradient should be high)", resp.Outcome)
	}
}

func TestMUSE_StalePrunes(t *testing.T) {
	ctxBuf, _ := json.Marshal(museContext{
		HitCount:    0,
		LastUsedUTC: "2025-01-01T00:00:00Z", // 18+ months ago
		NowUTC:      "2026-06-23T00:00:00Z",
	})
	req := &api.EvaluateReq{
		ArtifactJSON:         mkArtifactJSON(t, "x", nil, nil, 0.5),
		EvaluatorContextJSON: ctxBuf,
	}
	resp, err := MUSE{}.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("MUSE: %v", err)
	}
	if resp.Outcome != api.OutcomePrune || !resp.ShouldPrune {
		t.Errorf("expected prune, got %q (shouldPrune=%t)", resp.Outcome, resp.ShouldPrune)
	}
}

func TestMUSE_HighUsePromotes(t *testing.T) {
	ctxBuf, _ := json.Marshal(museContext{
		HitCount:    25,
		RegressionN: 0,
		LastUsedUTC: "2026-06-22T00:00:00Z",
		NowUTC:      "2026-06-23T00:00:00Z",
	})
	req := &api.EvaluateReq{
		ArtifactJSON:         mkArtifactJSON(t, "x", nil, nil, 0.6),
		EvaluatorContextJSON: ctxBuf,
	}
	resp, err := MUSE{}.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("MUSE: %v", err)
	}
	if resp.Outcome != api.OutcomePromote {
		t.Errorf("expected promote, got %q (= %s)", resp.Outcome, resp.Explanation)
	}
}

func TestSkillAudit_NoPeerKeeps(t *testing.T) {
	req := &api.EvaluateReq{
		ArtifactJSON: mkArtifactJSON(t, "x", nil, nil, 0.7),
	}
	resp, err := SkillAudit{}.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("SkillAudit: %v", err)
	}
	if resp.Outcome != api.OutcomeKeep {
		t.Errorf("Outcome = %q, want keep", resp.Outcome)
	}
}

func TestSkillAudit_HighOverlapBetterConfKeeps(t *testing.T) {
	peer := mkArtifactJSON(t, "Tight rule with clear scope", []string{"step one"}, []string{"only on PostToolUse"}, 0.5)
	ctxBuf, _ := json.Marshal(map[string]interface{}{
		"peer":            json.RawMessage(peer),
		"peer_confidence": 0.5,
	})
	req := &api.EvaluateReq{
		ArtifactJSON:         mkArtifactJSON(t, "Tight rule with clear scope", []string{"step one"}, []string{"only on PostToolUse"}, 0.85),
		EvaluatorContextJSON: ctxBuf,
	}
	resp, err := SkillAudit{}.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("SkillAudit: %v", err)
	}
	if resp.Outcome != api.OutcomeKeep {
		t.Errorf("Outcome = %q, want keep current (= %s)", resp.Outcome, resp.Explanation)
	}
	if !strings.Contains(resp.Explanation, "prune peer") {
		t.Errorf("explanation should recommend pruning peer: %s", resp.Explanation)
	}
}

func TestEvaluators_AllNamed(t *testing.T) {
	names := map[string]string{
		GEPA{}.Name():       "gepa",
		TextGrad{}.Name():   "textgrad",
		MUSE{}.Name():       "muse",
		SkillAudit{}.Name(): "skillaudit",
	}
	for got, want := range names {
		if got != want {
			t.Errorf("evaluator name = %q, want %q", got, want)
		}
	}
}
