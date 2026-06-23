package evolve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/judge"
	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

func TestCacheKey_Deterministic(t *testing.T) {
	req := api.JudgeRequest{
		PromptVersion:       "v3",
		ModelID:             "claude-opus-4-7",
		ClusterMemberIDs:    []string{"a", "b"},
		ClusterMemberHashes: []string{"h0", "h1"},
		NearestPriorLabel:   "io-lives-in-data-layer",
		NearestPriorDesc:    "I/O wraps via data layer",
		Temperature:         0,
		MaxOutputTokens:     1024,
	}
	k1 := CacheKey(req)
	k2 := CacheKey(req)
	if k1 != k2 {
		t.Errorf("non-deterministic: %s vs %s", k1, k2)
	}
	if len(k1) != 64 {
		t.Errorf("cache key length = %d, want 64 (= hex of sha256)", len(k1))
	}
}

func TestCacheKey_DifferentInputsDifferentKeys(t *testing.T) {
	base := api.JudgeRequest{
		PromptVersion:       "v3",
		ModelID:             "claude-opus-4-7",
		ClusterMemberIDs:    []string{"a"},
		ClusterMemberHashes: []string{"h0"},
	}
	other := base
	other.PromptVersion = "v4"
	if CacheKey(base) == CacheKey(other) {
		t.Errorf("PromptVersion change should change key")
	}
}

func TestValidateVerdict(t *testing.T) {
	cases := []struct {
		name    string
		v       api.JudgeVerdict
		wantErr bool
	}{
		{"valid PASS", api.JudgeVerdict{Verdict: api.VerdictPass, Confidence: 0.8, TimestampUTC: "2026-06-23T00:00:00Z"}, false},
		{"invalid verdict literal", api.JudgeVerdict{Verdict: "MAYBE", TimestampUTC: "x"}, true},
		{"confidence over 1", api.JudgeVerdict{Verdict: api.VerdictPass, Confidence: 1.5, TimestampUTC: "x"}, true},
		{"confidence below 0", api.JudgeVerdict{Verdict: api.VerdictFail, Confidence: -0.1, TimestampUTC: "x"}, true},
		{"empty timestamp", api.JudgeVerdict{Verdict: api.VerdictPass, Confidence: 0.5}, true},
		{"negative cost", api.JudgeVerdict{Verdict: api.VerdictPass, Confidence: 0.5, TimestampUTC: "x", CostEstimateUSD: -0.01}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateVerdict(tc.v)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateVerdict err = %v, wantErr = %t", err, tc.wantErr)
			}
		})
	}
}

func TestAuditDir_WriteThenRead(t *testing.T) {
	dir := t.TempDir()
	a := NewAuditDir(dir)
	rec := AuditRecord{
		CacheKey:  "abc123",
		JudgeName: "heuristic",
		ParsedAt:  "2026-06-23T00:00:00Z",
		Request:   api.JudgeRequest{PromptVersion: "v3", ModelID: "m"},
		Verdict:   api.JudgeVerdict{Verdict: api.VerdictPass, Confidence: 0.8, TimestampUTC: "x"},
	}
	if err := a.WriteRecord(rec); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	got, err := a.ReadRecord("abc123")
	if err != nil {
		t.Fatalf("ReadRecord: %v", err)
	}
	if got.CacheKey != "abc123" || got.Verdict.Verdict != api.VerdictPass {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestAuditDir_WriteRejectsEmptyKey(t *testing.T) {
	a := NewAuditDir(t.TempDir())
	if err := a.WriteRecord(AuditRecord{}); err == nil {
		t.Errorf("expected error for empty CacheKey")
	}
}

func TestCachedJudge_FirstCallPopulatesCache(t *testing.T) {
	dir := t.TempDir()
	audit := NewAuditDir(dir)
	h := judge.NewHeuristicJudgeClient()
	h.SetClock(func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) })
	c := NewCachedJudge(h, audit)
	c.SetClock(func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) })

	req := api.JudgeRequest{
		PromptVersion:       "v3",
		ModelID:             "claude-opus-4-7",
		ClusterMemberIDs:    []string{"a", "b", "c"},
		ClusterMemberHashes: []string{"h0", "h1", "h2"},
	}
	v1, err := c.Judge(context.Background(), req)
	if err != nil {
		t.Fatalf("first Judge: %v", err)
	}
	if v1.Verdict != api.VerdictPass {
		t.Errorf("verdict = %q, want PASS", v1.Verdict)
	}
	// Cache file present?
	key := CacheKey(req)
	if _, err := os.Stat(filepath.Join(dir, "judgements", key+".json")); err != nil {
		t.Errorf("cache file not written: %v", err)
	}
}

func TestCachedJudge_SecondCallHitsCache(t *testing.T) {
	dir := t.TempDir()
	audit := NewAuditDir(dir)
	stub := &countingJudge{inner: judge.NewHeuristicJudgeClient()}
	stub.inner.SetClock(func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) })
	c := NewCachedJudge(stub, audit)
	c.SetClock(func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) })

	req := api.JudgeRequest{
		PromptVersion:       "v3",
		ModelID:             "claude-opus-4-7",
		ClusterMemberIDs:    []string{"a", "b", "c"},
		ClusterMemberHashes: []string{"h0", "h1", "h2"},
	}
	if _, err := c.Judge(context.Background(), req); err != nil {
		t.Fatalf("first Judge: %v", err)
	}
	if _, err := c.Judge(context.Background(), req); err != nil {
		t.Fatalf("second Judge: %v", err)
	}
	if stub.calls != 1 {
		t.Errorf("inner.Judge calls = %d, want 1 (= second call should hit cache)", stub.calls)
	}
}

type countingJudge struct {
	inner *judge.HeuristicJudgeClient
	calls int
}

func (c *countingJudge) Name() string { return c.inner.Name() }
func (c *countingJudge) Judge(ctx context.Context, req api.JudgeRequest) (api.JudgeVerdict, error) {
	c.calls++
	return c.inner.Judge(ctx, req)
}
