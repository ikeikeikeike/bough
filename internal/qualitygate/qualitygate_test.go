package qualitygate

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestGate_Match(t *testing.T) {
	cases := []struct {
		name string
		g    Gate
		mc   MatchContext
		want bool
	}{
		{"empty matcher matches all", Gate{}, MatchContext{Event: "PostToolUse"}, true},
		{"event-only mismatch", Gate{OnEvent: "PostToolUse"}, MatchContext{Event: "PreToolUse"}, false},
		{"event-only match", Gate{OnEvent: "PostToolUse"}, MatchContext{Event: "PostToolUse"}, true},
		{"tool gate matches", Gate{OnTool: "Edit"}, MatchContext{Tool: "Edit"}, true},
		{"tool gate mismatch", Gate{OnTool: "Edit"}, MatchContext{Tool: "Bash"}, false},
		{"regex on file path matches",
			Gate{OnMatch: `.*\.go$`},
			MatchContext{FilePath: "internal/foo.go"}, true},
		{"regex on file path mismatch",
			Gate{OnMatch: `.*\.go$`},
			MatchContext{FilePath: "internal/foo.ts"}, false},
		{"all matchers must AND",
			Gate{OnEvent: "PostToolUse", OnTool: "Edit", OnMatch: `\.go$`},
			MatchContext{Event: "PostToolUse", Tool: "Edit", FilePath: "x.go"}, true},
		{"all matchers must AND (one fails)",
			Gate{OnEvent: "PostToolUse", OnTool: "Edit", OnMatch: `\.go$`},
			MatchContext{Event: "PostToolUse", Tool: "Bash", FilePath: "x.go"}, false},
		{"invalid regex never matches",
			Gate{OnMatch: "["},
			MatchContext{FilePath: "x.go"}, false},
		{"regex falls back to command when filepath empty",
			Gate{OnMatch: "go test"},
			MatchContext{Command: "go test ./..."}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.g.Match(tc.mc); got != tc.want {
				t.Errorf("Match() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestGate_Run_PassExit0(t *testing.T) {
	g := Gate{Name: "true", Command: "true", TimeoutSeconds: 5}
	r := g.Run(context.Background())
	if r.Err != nil {
		t.Errorf("expected nil Err for `true`, got %v", r.Err)
	}
	if r.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", r.ExitCode)
	}
}

func TestGate_Run_FailExitNonZero(t *testing.T) {
	g := Gate{Name: "false", Command: "false", TimeoutSeconds: 5}
	r := g.Run(context.Background())
	if r.Err == nil {
		t.Errorf("expected non-nil Err for `false`")
	}
	if r.ExitCode == 0 {
		t.Errorf("expected non-zero ExitCode, got 0")
	}
}

func TestGate_Run_StdoutCaptured(t *testing.T) {
	g := Gate{Name: "echo", Command: "echo hello", TimeoutSeconds: 5}
	r := g.Run(context.Background())
	if !strings.Contains(r.Stdout, "hello") {
		t.Errorf("Stdout = %q, want substring 'hello'", r.Stdout)
	}
}

func TestRunMatching_SkipsNonMatching(t *testing.T) {
	var stderr bytes.Buffer
	gates := []Gate{
		{Name: "matches", Command: "true", OnEvent: "PostToolUse", TimeoutSeconds: 5},
		{Name: "skipped", Command: "false", OnEvent: "Stop", TimeoutSeconds: 5},
	}
	results := RunMatching(context.Background(), gates, MatchContext{Event: "PostToolUse"}, &stderr)
	if len(results) != 1 {
		t.Errorf("RunMatching returned %d results, want 1", len(results))
	}
	if results[0].Gate != "matches" {
		t.Errorf("RunMatching ran %q, want 'matches'", results[0].Gate)
	}
	if !strings.Contains(stderr.String(), "matches") {
		t.Errorf("stderr missing pass summary: %s", stderr.String())
	}
}
