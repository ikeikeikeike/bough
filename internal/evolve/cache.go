package evolve

import (
	"crypto/sha256"
	"encoding/hex"

	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// CacheKey returns the canonical sha256 fingerprint of a JudgeRequest.
// The same fingerprint MUST always produce the same JudgeVerdict;
// callers rely on this to dedupe LLM calls and replay golden corpus
// fixtures.
//
// Layout (round 5 reviewer non-negotiable):
//
//	sha256(prompt_version | 0x00 |
//	       model_id        | 0x00 |
//	       cluster_member_ids   | 0x1F-joined | 0x00 |
//	       cluster_member_hashes | 0x1F-joined | 0x00 |
//	       nearest_prior_label  | 0x00 |
//	       nearest_prior_description)
//
// 0x00 separates top-level fields; 0x1F separates list items so a
// member id containing "|" never collides with the join character.
//
// internal/judge/replay.go reimplements this function verbatim to
// avoid an import cycle; the golden corpus test cross-checks both
// implementations produce identical keys for the same input.
func CacheKey(req api.JudgeRequest) string {
	h := sha256.New()
	h.Write([]byte(req.PromptVersion))
	h.Write([]byte{0x00})
	h.Write([]byte(req.ModelID))
	h.Write([]byte{0x00})
	for _, id := range req.ClusterMemberIDs {
		h.Write([]byte(id))
		h.Write([]byte{0x1F})
	}
	h.Write([]byte{0x00})
	for _, hash := range req.ClusterMemberHashes {
		h.Write([]byte(hash))
		h.Write([]byte{0x1F})
	}
	h.Write([]byte{0x00})
	h.Write([]byte(req.NearestPriorLabel))
	h.Write([]byte{0x00})
	h.Write([]byte(req.NearestPriorDesc))
	return hex.EncodeToString(h.Sum(nil))
}

// ValidateVerdict checks a JudgeVerdict satisfies the JSON schema
// the evolve pipeline + audit layer assume:
//
//	verdict          ∈ {PASS, DOUBT, FAIL}
//	confidence       ∈ [0.0, 1.0]
//	timestamp_utc    non-empty
//	cost_estimate_usd ≥ 0
//
// Reason and RecommendedLabel are advisory; the pipeline does not
// enforce shape on them.
//
// A live LLM that returns an invalid verdict is treated as a
// judge failure by the pipeline (= falls through to DOUBT), but
// the audit log records the original raw verdict so the operator
// can diagnose the drift.
func ValidateVerdict(v api.JudgeVerdict) error {
	switch v.Verdict {
	case api.VerdictPass, api.VerdictDoubt, api.VerdictFail:
	default:
		return verdictError{field: "verdict", got: string(v.Verdict), want: "PASS / DOUBT / FAIL"}
	}
	if v.Confidence < 0 || v.Confidence > 1 {
		return verdictError{field: "confidence", got: floatString(v.Confidence), want: "[0.0, 1.0]"}
	}
	if v.TimestampUTC == "" {
		return verdictError{field: "timestamp_utc", got: "", want: "non-empty RFC3339"}
	}
	if v.CostEstimateUSD < 0 {
		return verdictError{field: "cost_estimate_usd", got: floatString(v.CostEstimateUSD), want: "≥ 0"}
	}
	return nil
}

type verdictError struct {
	field string
	got   string
	want  string
}

func (e verdictError) Error() string {
	return "invalid JudgeVerdict." + e.field + " = " + e.got + " (want " + e.want + ")"
}

func floatString(f float64) string {
	// Avoid pulling strconv into this hot path; the audit log
	// formats the full struct elsewhere. This is only used in
	// error messages.
	if f == 0 {
		return "0"
	}
	buf := make([]byte, 0, 16)
	if f < 0 {
		buf = append(buf, '-')
		f = -f
	}
	intPart := int64(f)
	frac := f - float64(intPart)
	buf = appendInt(buf, intPart)
	if frac > 0 {
		buf = append(buf, '.')
		frac10 := int64(frac * 1e6)
		buf = appendInt(buf, frac10)
	}
	return string(buf)
}

func appendInt(buf []byte, n int64) []byte {
	if n == 0 {
		return append(buf, '0')
	}
	var rev [20]byte
	i := 0
	for n > 0 {
		rev[i] = byte('0' + n%10)
		n /= 10
		i++
	}
	for j := i - 1; j >= 0; j-- {
		buf = append(buf, rev[j])
	}
	return buf
}
