package judge

import (
	"context"
	"strings"
	"testing"
	"time"

	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

func TestHeuristicJudge_DecisionTree(t *testing.T) {
	cases := []struct {
		name        string
		ids         []string
		hashes      []string
		nearestPrev string
		wantVerdict api.VerdictKind
		wantReuse   bool
	}{
		{
			name:        "empty cluster is FAIL",
			ids:         nil,
			hashes:      nil,
			wantVerdict: api.VerdictFail,
		},
		{
			name:        "all-identical cluster is FAIL (duplicate)",
			ids:         []string{"a", "b", "c"},
			hashes:      []string{"h0", "h0", "h0"},
			wantVerdict: api.VerdictFail,
		},
		{
			name:        "singleton is DOUBT",
			ids:         []string{"a"},
			hashes:      []string{"h0"},
			wantVerdict: api.VerdictDoubt,
		},
		{
			name:        "pair with distinct hashes is DOUBT",
			ids:         []string{"a", "b"},
			hashes:      []string{"h0", "h1"},
			wantVerdict: api.VerdictDoubt,
		},
		{
			name:        "triplet+ with diversity is PASS",
			ids:         []string{"a", "b", "c"},
			hashes:      []string{"h0", "h1", "h2"},
			wantVerdict: api.VerdictPass,
		},
		{
			name:        "prior label triggers ReusePriorLabel",
			ids:         []string{"a", "b", "c"},
			hashes:      []string{"h0", "h1", "h2"},
			nearestPrev: "io-lives-in-data-layer",
			wantVerdict: api.VerdictPass,
			wantReuse:   true,
		},
	}
	h := NewHeuristicJudgeClient()
	h.SetClock(func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) })
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := api.JudgeRequest{
				PromptVersion:       "v3-2026-06-23",
				ModelID:             "claude-opus-4-7",
				ClusterMemberIDs:    tc.ids,
				ClusterMemberHashes: tc.hashes,
				NearestPriorLabel:   tc.nearestPrev,
				Temperature:         0,
				MaxOutputTokens:     1024,
			}
			got, err := h.Judge(context.Background(), req)
			if err != nil {
				t.Fatalf("Judge() error = %v", err)
			}
			if got.Verdict != tc.wantVerdict {
				t.Errorf("Verdict = %q, want %q", got.Verdict, tc.wantVerdict)
			}
			if got.ReusePriorLabel != tc.wantReuse {
				t.Errorf("ReusePriorLabel = %t, want %t", got.ReusePriorLabel, tc.wantReuse)
			}
			if got.TimestampUTC == "" {
				t.Errorf("TimestampUTC empty")
			}
			if got.RecommendedLabel == "" {
				t.Errorf("RecommendedLabel empty")
			}
			if tc.nearestPrev != "" && got.RecommendedLabel != tc.nearestPrev {
				t.Errorf("RecommendedLabel = %q, want prior %q", got.RecommendedLabel, tc.nearestPrev)
			}
			if tc.nearestPrev == "" && !strings.HasPrefix(got.RecommendedLabel, "heuristic_") {
				t.Errorf("RecommendedLabel = %q, want heuristic_ prefix", got.RecommendedLabel)
			}
		})
	}
}

func TestHeuristicJudge_Deterministic(t *testing.T) {
	// A deterministic backend must produce identical verdicts for
	// identical inputs across calls. This is the core cache
	// correctness invariant; if it ever fails, the SHA256 cache and
	// the ReplayJudgeClient both break in subtle ways.
	h := NewHeuristicJudgeClient()
	h.SetClock(func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) })
	req := api.JudgeRequest{
		PromptVersion:       "v3",
		ModelID:             "claude-opus-4-7",
		ClusterMemberIDs:    []string{"a", "b", "c"},
		ClusterMemberHashes: []string{"h0", "h1", "h2"},
	}
	v1, _ := h.Judge(context.Background(), req)
	v2, _ := h.Judge(context.Background(), req)
	if v1 != v2 {
		t.Errorf("non-deterministic: %+v != %+v", v1, v2)
	}
}
