package evolve

import (
	"context"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/judge"
	"github.com/ikeikeikeike/bough/pkg/schema"
	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

func mkBundle(id, content string) schema.TraceBundle {
	return schema.TraceBundle{
		ID:         id,
		Source:     schema.TraceSourceStdin,
		Scope:      schema.Scope{Level: schema.ScopeRepo, RepoName: "bough"},
		Content:    content,
		CapturedAt: time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
	}
}

func TestPipeline_RunEndToEnd(t *testing.T) {
	bundles := []schema.TraceBundle{
		mkBundle("a", "I/O goes through the data layer wrapper, never inline in usecase"),
		mkBundle("b", "I/O lives in data layer; usecase calls wrappers via interface"),
		mkBundle("c", "data layer wraps I/O; usecase keeps no transport client"),
		mkBundle("d", "TODO: implement later"), // should drop on Gate 2 anti-pattern
		mkBundle("e", "short"),                  // should drop on Gate 2 length
		mkBundle("f", ""),                       // should drop on Gate 1 (empty content)
	}
	h := judge.NewHeuristicJudgeClient()
	h.SetClock(func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) })

	p := Pipeline{
		Judge:         h,
		PromptVersion: "v3-2026-06-23",
		ModelID:       "claude-opus-4-7",
		Now:           func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) },
	}
	res, err := p.Run(context.Background(), bundles)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if res.InputCount != 6 {
		t.Errorf("InputCount = %d, want 6", res.InputCount)
	}
	if res.Dropped != 3 {
		t.Errorf("Dropped = %d, want 3 (TODO + short + empty)", res.Dropped)
	}
	if res.Clusters == 0 {
		t.Errorf("Clusters = 0, want >= 1 (the three I/O bundles should group)")
	}
	if len(res.Candidates) == 0 {
		t.Errorf("Candidates empty; heuristic judge should have promoted at least one cluster")
	}
	for _, c := range res.Candidates {
		if c.State != schema.InstinctStateCandidate {
			t.Errorf("Candidate state = %q, want candidate", c.State)
		}
		if c.DedupeKey == "" {
			t.Errorf("Candidate DedupeKey empty")
		}
	}
}

func TestPipeline_RejectsNilJudge(t *testing.T) {
	p := Pipeline{PromptVersion: "v3", ModelID: "m"}
	_, err := p.Run(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for nil Judge, got nil")
	}
}

func TestPipeline_RejectsEmptyPromptVersion(t *testing.T) {
	p := Pipeline{Judge: judge.NewHeuristicJudgeClient(), ModelID: "m"}
	_, err := p.Run(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for empty PromptVersion, got nil")
	}
}

func TestPipeline_JudgeErrorBecomesDoubt(t *testing.T) {
	stub := stubJudge{err: errStubFail}
	p := Pipeline{
		Judge:         stub,
		PromptVersion: "v3",
		ModelID:       "m",
		Now:           func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) },
	}
	bundles := []schema.TraceBundle{
		mkBundle("a", "I/O goes through the data layer wrapper, never inline in usecase"),
		mkBundle("b", "data layer wraps I/O; usecase keeps no transport client"),
		mkBundle("c", "I/O lives in data layer; usecase calls wrappers via interface"),
	}
	res, err := p.Run(context.Background(), bundles)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Verdicts[api.VerdictDoubt] == 0 {
		t.Errorf("expected at least one DOUBT verdict from judge failure")
	}
}

var errStubFail = stubError("forced judge failure")

type stubError string

func (s stubError) Error() string { return string(s) }

type stubJudge struct {
	err error
}

func (s stubJudge) Name() string { return "stub" }
func (s stubJudge) Judge(_ context.Context, _ api.JudgeRequest) (api.JudgeVerdict, error) {
	return api.JudgeVerdict{}, s.err
}
