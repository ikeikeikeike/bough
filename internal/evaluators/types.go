// Package evaluators ships in-process SkillEvaluator implementations
// for the four research-paper-derived strategies the bough roadmap
// has been carrying through v0.5 → v0.7:
//
//	gepa       — reflective prompt optimiser (= detects redundancy /
//	             scope creep / over-generalisation in artifact text
//	             and recommends revise)
//	textgrad   — gradient evaluator (= compares an artifact against
//	             its prior version, scores based on divergence /
//	             signal-to-noise)
//	muse       — autoskill lifecycle evaluator (= tracks last-used,
//	             hit count, recent regression count; recommends prune
//	             for stale artifacts)
//	skillaudit — paired-trajectory auditor (= compares two artifacts
//	             in the same family, recommends keep on the survivor)
//
// v0.8 ships them as in-process Go implementations behind the
// existing plugins/evaluator/api.SkillEvaluator interface so a host
// can call them without spawning a gRPC plugin process. v0.9+ may
// graduate them into standalone plugin binaries; the interface
// boundary already lives in plugins/evaluator/api/.
//
// All four implementations are deterministic for the same input.
// Where they need historical context (= TextGrad needs the prior
// artifact; MUSE needs the usage log; SkillAudit needs the peer
// artifact) the host passes it through EvaluatorContextJSON; the
// evaluator parses what it needs.
package evaluators

import (
	"encoding/json"

	api "github.com/ikeikeikeike/bough/plugins/evaluator/api"
	"github.com/ikeikeikeike/bough/pkg/schema"
)

// artifactProbe is the minimal field set every evaluator needs to
// pull out of EvaluateReq.ArtifactJSON. We unmarshal once into this
// shape and pass it to the per-evaluator decision function so each
// evaluator stays narrowly scoped.
type artifactProbe struct {
	ID          string                            `json:"ID"`
	Kind        schema.CapabilityArtifactKind     `json:"Kind"`
	Name        string                            `json:"Name"`
	Description string                            `json:"Description"`
	Confidence  float64                           `json:"Confidence"`
	Constraints []string                          `json:"Constraints"`
	Steps       []string                          `json:"Steps"`
	Inputs      []string                          `json:"Inputs"`
	Outputs     []string                          `json:"Outputs"`
	Payload     json.RawMessage                   `json:"Payload"`
}

func decodeArtifact(buf []byte) (artifactProbe, error) {
	var p artifactProbe
	if len(buf) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(buf, &p); err != nil {
		return p, err
	}
	return p, nil
}

// Ensure all evaluators satisfy the contract at compile time.
var (
	_ api.SkillEvaluator = (*GEPA)(nil)
	_ api.SkillEvaluator = (*TextGrad)(nil)
	_ api.SkillEvaluator = (*MUSE)(nil)
	_ api.SkillEvaluator = (*SkillAudit)(nil)
)
