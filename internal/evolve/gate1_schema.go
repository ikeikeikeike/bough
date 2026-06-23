// Package evolve ports the upstream ECC `/evolve-skill-manual-v3`
// canonical 4-gate pipeline into Go. The four gates run in sequence:
//
//	Gate 1 — schema validation       (drops malformed TraceBundles)
//	Gate 2 — heuristic filter        (drops low-quality / anti-pattern)
//	Gate 3 — clustering              (groups similar observations)
//	-- LLM judge -- (= GATE 5 in ECC vocabulary, between 3 and 4)
//	Gate 4 — candidate stamp         (writes state=candidate Instincts)
//
// The boundary lines and thresholds mirror ECC Python v3 so the
// v0.7.1 golden corpus diff can pin a behavioural compatibility
// baseline. Each gate lives in its own file so a future plugin slot
// can swap individual gates without touching the orchestrator.
//
// See ~/.claude/plans/bough-v071-sprint-detail.md §2.4 for the
// design rationale and the per-gate Python source references.
package evolve

import (
	"fmt"
	"strings"
	"time"

	"github.com/ikeikeikeike/bough/pkg/schema"
)

// Gate1Schema validates that a TraceBundle carries the minimum
// fields the rest of the pipeline depends on. The downstream gates
// dereference Scope.Level, Content, and CapturedAt without nil
// checks, so a malformed bundle here would crash Gate2.
//
// Returns nil when the bundle passes; otherwise an error describing
// the first missing field. The pipeline drops failing bundles into
// an audit jsonl rather than aborting the whole batch.
func Gate1Schema(tb schema.TraceBundle) error {
	if tb.ID == "" {
		return fmt.Errorf("gate1_schema: TraceBundle.ID empty")
	}
	if tb.Source == "" {
		return fmt.Errorf("gate1_schema: TraceBundle.Source empty (id=%s)", tb.ID)
	}
	if tb.Scope.Level == "" {
		return fmt.Errorf("gate1_schema: TraceBundle.Scope.Level empty (id=%s)", tb.ID)
	}
	if strings.TrimSpace(tb.Content) == "" {
		return fmt.Errorf("gate1_schema: TraceBundle.Content empty (id=%s)", tb.ID)
	}
	if tb.CapturedAt.IsZero() {
		return fmt.Errorf("gate1_schema: TraceBundle.CapturedAt zero (id=%s)", tb.ID)
	}
	if tb.CapturedAt.After(time.Now().Add(24 * time.Hour)) {
		return fmt.Errorf("gate1_schema: TraceBundle.CapturedAt in the future (id=%s, ts=%s)", tb.ID, tb.CapturedAt)
	}
	return nil
}
