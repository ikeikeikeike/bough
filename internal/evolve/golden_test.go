package evolve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/judge"
	"github.com/ikeikeikeike/bough/pkg/schema"
)

// goldenCase is the on-disk shape of testdata/golden/inputs/*.json.
// Bundles is a list of TraceBundle skeletons (ID + Content +
// Scope.Level + Source); the test fills in CapturedAt from the
// fixed clock so the run stays byte-stable.
type goldenCase struct {
	Name    string             `json:"name"`
	Bundles []goldenBundleSeed `json:"bundles"`
}

type goldenBundleSeed struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	ScopeLevel string `json:"scope_level"`
	RepoName   string `json:"repo_name"`
	Content    string `json:"content"`
}

// goldenSnapshot is the trimmed Pipeline.Run result the test pins.
// Order is enforced by sorting Candidate IDs so map iteration order
// never causes flakes.
type goldenSnapshot struct {
	InputCount     int            `json:"input_count"`
	Dropped        int            `json:"dropped"`
	Clusters       int            `json:"clusters"`
	VerdictCounts  map[string]int `json:"verdict_counts"`
	CandidateCount int            `json:"candidate_count"`
	CandidateIDs   []string       `json:"candidate_ids"`
}

func TestGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/golden/inputs/*.json")
	if err != nil {
		t.Fatalf("glob inputs: %v", err)
	}
	if len(inputs) == 0 {
		t.Skip("no golden inputs present")
	}
	for _, path := range inputs {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var gc goldenCase
			if err := json.Unmarshal(raw, &gc); err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			bundles := []schema.TraceBundle{}
			for _, b := range gc.Bundles {
				bundles = append(bundles, schema.TraceBundle{
					ID:         b.ID,
					Source:     schema.TraceSource(b.Source),
					Scope:      schema.Scope{Level: schema.ScopeLevel(b.ScopeLevel), RepoName: b.RepoName},
					Content:    b.Content,
					CapturedAt: time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
				})
			}
			h := judge.NewHeuristicJudgeClient()
			h.SetClock(func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) })
			p := Pipeline{
				Judge:         h,
				PromptVersion: "golden-v3",
				ModelID:       "claude-opus-4-7",
				Now:           func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) },
			}
			res, err := p.Run(context.Background(), bundles)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			snap := goldenSnapshot{
				InputCount:     res.InputCount,
				Dropped:        res.Dropped,
				Clusters:       res.Clusters,
				CandidateCount: len(res.Candidates),
				VerdictCounts:  map[string]int{},
			}
			for k, v := range res.Verdicts {
				snap.VerdictCounts[string(k)] = v
			}
			for _, c := range res.Candidates {
				snap.CandidateIDs = append(snap.CandidateIDs, c.ID)
			}
			sort.Strings(snap.CandidateIDs)

			expectedPath := filepath.Join("testdata/golden/expected", filepath.Base(path))
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				buf, _ := json.MarshalIndent(snap, "", "  ")
				if err := os.WriteFile(expectedPath, buf, 0o644); err != nil {
					t.Fatalf("write expected %s: %v", expectedPath, err)
				}
				return
			}
			expRaw, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("read expected %s: %v (run UPDATE_GOLDEN=1 to refresh)", expectedPath, err)
			}
			var expSnap goldenSnapshot
			if err := json.Unmarshal(expRaw, &expSnap); err != nil {
				t.Fatalf("parse expected %s: %v", expectedPath, err)
			}
			gotBuf, _ := json.MarshalIndent(snap, "", "  ")
			wantBuf, _ := json.MarshalIndent(expSnap, "", "  ")
			if !strings.EqualFold(string(gotBuf), string(wantBuf)) {
				t.Errorf("golden diff for %s:\nGOT:\n%s\nWANT:\n%s", filepath.Base(path), gotBuf, wantBuf)
			}
		})
	}
}
