package evolve

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/ikeikeikeike/bough/pkg/schema"
	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// Gate4Candidate stamps a Cluster + JudgeVerdict into a list of
// state=candidate InstinctCandidate rows ready for memory backend
// Store. One InstinctCandidate per cluster member (not per cluster)
// so the memory backend can dedupe on its own DedupeKey.
//
// Verdicts:
//
//	PASS  → emit InstinctCandidate for every member, state=candidate
//	DOUBT → emit but Confidence is clamped low (= operator must
//	         explicitly `bough instinct approve` before active)
//	FAIL  → drop (= no Store)
//
// The returned slice is empty (not nil) when the verdict is FAIL
// so callers can range over it without nil checks.
func Gate4Candidate(c Cluster, verdict api.JudgeVerdict, now time.Time) []schema.InstinctCandidate {
	if verdict.Verdict == api.VerdictFail {
		return []schema.InstinctCandidate{}
	}
	confidence := verdict.Confidence
	if verdict.Verdict == api.VerdictDoubt && confidence > 0.5 {
		confidence = 0.5
	}
	label := verdict.RecommendedLabel
	if label == "" {
		label = c.NearestPriorLabel
	}
	out := make([]schema.InstinctCandidate, 0, len(c.Members))
	for _, m := range c.Members {
		body := strings.TrimSpace(m.Content)
		candidate := schema.InstinctCandidate{
			ID:         deriveCandidateID(c.ID, m.ID),
			Rule:       body,
			Why:        verdict.Reason,
			HowToApply: label,
			Domain:     []string{string(m.Source)},
			Scope:      m.Scope,
			Source:     m.Source,
			Confidence: confidence,
			State:      schema.InstinctStateCandidate,
			SourceTraces: []string{m.ID},
			CreatedAt:  now.UTC(),
			DedupeKey:  dedupeKey(body, m.Scope),
		}
		out = append(out, candidate)
	}
	return out
}

func deriveCandidateID(clusterID, memberID string) string {
	h := sha256.Sum256([]byte(clusterID + "|" + memberID))
	return "cand_" + hex.EncodeToString(h[:8])
}

// dedupeKey mirrors the InstinctCandidate.DedupeKey contract in
// pkg/schema/instinct.go: sha256(normalize(Rule + Scope.Level +
// Scope.WorktreeID + Scope.RepoName)). normalize lower-cases and
// trims whitespace so trivial diff (= trailing space, case) does
// not duplicate.
func dedupeKey(rule string, scope schema.Scope) string {
	norm := strings.ToLower(strings.TrimSpace(rule))
	h := sha256.New()
	h.Write([]byte(norm))
	h.Write([]byte{0x00})
	h.Write([]byte(string(scope.Level)))
	h.Write([]byte{0x00})
	h.Write([]byte(scope.WorktreeID))
	h.Write([]byte{0x00})
	h.Write([]byte(scope.RepoName))
	return hex.EncodeToString(h.Sum(nil))
}
