package evolve

import (
	"strings"
	"unicode"

	"github.com/ikeikeikeike/bough/pkg/schema"
)

// MinContentLen is the minimum byte-length a TraceBundle.Content
// must reach to clear Gate 2. v3 canonical = 24 bytes; ECC Python
// uses the same constant so a single-word observation never makes
// it past the heuristic filter.
const MinContentLen = 24

// MinTokenCount is the minimum count of distinct alpha tokens the
// content must contain. Stops "yes yes yes yes yes" style padding
// from passing the length check via repetition.
const MinTokenCount = 4

// HeuristicVerdict carries the Gate 2 outcome so the audit log can
// record *why* a bundle was dropped without re-running the gate.
type HeuristicVerdict struct {
	Pass   bool
	Score  float64 // 0.0 - 1.0
	Reason string
}

// AntiPatterns are substrings that disqualify an observation from
// becoming an instinct. Mirrors the ECC Python v3 anti-pattern set:
// noise / acknowledgement / meta tokens that the LLM judge would
// also reject downstream, so dropping them early saves cycles.
var AntiPatterns = []string{
	"i don't know",
	"i'm not sure",
	"todo:",
	"fixme:",
	"xxx",
	"placeholder",
	"sample text",
	"lorem ipsum",
}

// Gate2Heuristic runs the deterministic mechanical filter. Returns
// HeuristicVerdict with Pass=true when the observation looks like
// a viable instinct candidate; Pass=false otherwise with Reason
// pointing at the failing check.
//
// The decision is intentionally pure (no time, no I/O) so the
// SHA256 cache and golden corpus diff stay byte-stable.
func Gate2Heuristic(tb schema.TraceBundle) HeuristicVerdict {
	content := strings.TrimSpace(tb.Content)
	if len(content) < MinContentLen {
		return HeuristicVerdict{
			Pass:   false,
			Score:  float64(len(content)) / float64(MinContentLen),
			Reason: "content too short",
		}
	}
	tokens := tokenize(content)
	if len(tokens) < MinTokenCount {
		return HeuristicVerdict{
			Pass:   false,
			Score:  float64(len(tokens)) / float64(MinTokenCount),
			Reason: "too few distinct tokens",
		}
	}
	lower := strings.ToLower(content)
	for _, ap := range AntiPatterns {
		if strings.Contains(lower, ap) {
			return HeuristicVerdict{
				Pass:   false,
				Score:  0.0,
				Reason: "matched anti-pattern: " + ap,
			}
		}
	}
	score := scoreContent(content, tokens)
	return HeuristicVerdict{
		Pass:   true,
		Score:  score,
		Reason: "passed length + token + anti-pattern checks",
	}
}

// tokenize splits content on whitespace + punctuation and returns
// the lower-cased distinct word set. Tokens shorter than 2 chars
// are dropped — they're usually punctuation noise.
func tokenize(s string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	cur := strings.Builder{}
	flush := func() {
		w := cur.String()
		cur.Reset()
		if len(w) < 2 {
			return
		}
		lw := strings.ToLower(w)
		if _, ok := seen[lw]; ok {
			return
		}
		seen[lw] = struct{}{}
		out = append(out, lw)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// scoreContent blends length and token diversity into a 0-1 score.
// Longer content with more distinct tokens scores higher; the score
// feeds into Gate 3 cluster confidence and the audit record.
func scoreContent(content string, tokens []string) float64 {
	lenScore := float64(len(content)) / 200.0
	if lenScore > 1.0 {
		lenScore = 1.0
	}
	tokScore := float64(len(tokens)) / 24.0
	if tokScore > 1.0 {
		tokScore = 1.0
	}
	return 0.4*lenScore + 0.6*tokScore
}
