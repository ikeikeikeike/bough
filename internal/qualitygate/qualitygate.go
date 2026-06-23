// Package qualitygate runs operator-supplied lint / typecheck /
// smoke commands sequentially when matching Claude Code hook
// events fire. The gates are declared in `.bough.yaml`'s
// `quality_gates:` section and wired into `bough hook handle`'s
// PostToolUse path.
//
// Gates are intentionally external commands, not bough-aware
// plugins: the operator owns their stack's lint / typecheck /
// build invocations, bough only sequences them and surfaces the
// pass/fail result via stderr so Claude Code's next turn sees it.
//
// v0.7.1 ships the runner skeleton + config schema; v0.7.2
// dogfooding wires the runner into the hook handle dispatch.
package qualitygate

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Gate declares one operator-defined sequence step. Command runs
// via `sh -c` so the operator can chain `nix develop -c make
// test-short` etc. without parsing.
//
// Matchers:
//
//	OnEvent  required: matched against the Claude Code hook event
//	         (PreToolUse / PostToolUse / SessionEnd / ...)
//	OnTool   optional: matches tool name (Edit / Bash / Read / ...)
//	OnMatch  optional regex matched against the tool's file path
//	         or command. Default = match all.
//	OnRepo   optional: matches the repository name from .bough.yaml
//
// All matchers AND together. Empty matcher = wildcard.
type Gate struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
	OnEvent string `yaml:"on_event"`
	OnTool  string `yaml:"on_tool"`
	OnMatch string `yaml:"on_match"`
	OnRepo  string `yaml:"on_repo"`

	// TimeoutSeconds caps the per-gate execution. Default = 60s
	// so a single hanging gate cannot block a hook indefinitely.
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

// MatchContext is the runtime context passed to Match. Event +
// Tool come from the Claude Code hook payload; FilePath / Command
// are extracted from tool_input depending on the tool.
type MatchContext struct {
	Event    string
	Tool     string
	FilePath string
	Command  string
	Repo     string
}

// Match returns true when every non-empty matcher on g matches
// the context. An empty matcher is a wildcard.
func (g Gate) Match(ctx MatchContext) bool {
	if g.OnEvent != "" && !strings.EqualFold(g.OnEvent, ctx.Event) {
		return false
	}
	if g.OnTool != "" && !strings.EqualFold(g.OnTool, ctx.Tool) {
		return false
	}
	if g.OnRepo != "" && !strings.EqualFold(g.OnRepo, ctx.Repo) {
		return false
	}
	if g.OnMatch != "" {
		re, err := regexp.Compile(g.OnMatch)
		if err != nil {
			return false
		}
		target := ctx.FilePath
		if target == "" {
			target = ctx.Command
		}
		if !re.MatchString(target) {
			return false
		}
	}
	return true
}

// GateResult records one gate execution outcome. ExitCode is
// shell exit code; Stdout / Stderr are captured raw output.
type GateResult struct {
	Gate     string
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
	Err      error
}

// Run executes a single gate. The cancel-aware context allows
// `bough hook handle` to enforce its own session-level timeout.
//
// On exit code 0 the gate passes; any non-zero code (or a timeout
// expiry) returns a non-nil GateResult.Err so the caller can
// surface a one-line summary via stderr.
func (g Gate) Run(ctx context.Context) GateResult {
	timeout := time.Duration(g.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(runCtx, "sh", "-c", g.Command)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := GateResult{
		Gate:     g.Name,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
		Err:      err,
	}
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
	} else if err != nil {
		res.ExitCode = -1
	}
	return res
}

// RunMatching runs every gate whose Match() returns true against
// the same context. Results are returned in registration order so
// the operator can read them top-to-bottom.
//
// stderr is the destination for the one-line per-gate summary;
// passing io.Discard suppresses it (used in tests).
func RunMatching(ctx context.Context, gates []Gate, mc MatchContext, stderr io.Writer) []GateResult {
	results := []GateResult{}
	for _, g := range gates {
		if !g.Match(mc) {
			continue
		}
		r := g.Run(ctx)
		results = append(results, r)
		if stderr != nil {
			status := "pass"
			if r.Err != nil {
				status = fmt.Sprintf("FAIL exit=%d", r.ExitCode)
			}
			fmt.Fprintf(stderr, "bough quality-gate %s: %s (%.1fs)\n", g.Name, status, r.Duration.Seconds())
		}
	}
	return results
}
