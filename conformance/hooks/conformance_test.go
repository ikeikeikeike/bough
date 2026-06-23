// Package hooks is the v0.7.0 Bootstrap safety floor end-to-end
// test. It builds the actual bough binary, then drives it through
// the install → handle → bootstrap → doctor → uninstall sequence
// against a tmpdir-rooted .claude/settings.json + .bough/. The
// unit tests in internal/hooks pin the per-method behaviour; this
// suite proves the chain works as a real CLI user would invoke it.
//
// Round 5 review insistence: hook auto-wire without a real-binary
// integration check is exactly how regressions ship. The
// subprocess approach (versus an in-process call) is the same
// pattern conformance/mcp/subprocess_test.go uses for the MCP
// stdio production path.
package hooks_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHooks_EndToEnd_InstallHandleBootstrapDoctorUninstall walks
// the canonical v0.7.0 user flow: install hooks, capture an
// observation via `bough hook handle`, generate a bootstrap dry-
// run, render the doctor report, then uninstall. Each step's
// stdout / artefacts are inspected so a future patch breaking any
// piece of the chain fails this test.
func TestHooks_EndToEnd_InstallHandleBootstrapDoctorUninstall(t *testing.T) {
	bin := buildBoughBinary(t)
	workdir := t.TempDir()

	run := func(t *testing.T, label string, stdin string, args ...string) (string, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = workdir
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s failed: %v\nstdout: %s\nstderr: %s", label, err, stdout.String(), stderr.String())
		}
		return stdout.String(), stderr.String()
	}

	// install
	stdout, _ := run(t, "hook install", "", "hook", "install")
	_ = stdout
	settingsPath := filepath.Join(workdir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings.json not created at %s: %v", settingsPath, err)
	}

	// list shows the eight events
	stdout, _ = run(t, "hook list", "", "hook", "list")
	wantedEvents := []string{
		"PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop",
		"SessionEnd", "PreCompact", "WorktreeCreate", "WorktreeRemove",
	}
	for _, event := range wantedEvents {
		if !strings.Contains(stdout, event) {
			t.Errorf("hook list missing %s: %s", event, stdout)
		}
	}

	// hook handle captures two observations
	run(t, "hook handle PreToolUse",
		`{"hook_event_name":"PreToolUse","tool_name":"Edit"}`,
		"hook", "handle", "--event", "PreToolUse",
	)
	run(t, "hook handle SessionEnd",
		`{"hook_event_name":"SessionEnd","reason":"logout"}`,
		"hook", "handle", "--event", "SessionEnd",
	)
	obsPath := filepath.Join(workdir, ".bough", "observations.jsonl")
	obsData, err := os.ReadFile(obsPath)
	if err != nil {
		t.Fatalf("observations.jsonl missing: %v", err)
	}
	if got := bytes.Count(obsData, []byte("\n")); got != 2 {
		t.Errorf("observations.jsonl line count: got %d want 2", got)
	}

	// v0.9 surface: observer run-once --dry-run renders the
	// prompt without spawning `claude --print`. The legacy
	// v0.7-v0.8 `bootstrap --dry-run` writes-Markdown surface is
	// gone (= chore(v0.9): reset). The dry-run is enough to
	// verify the install → handle pipeline lands observations
	// where the observer expects them.
	stdout, _ = run(t, "observer run-once --dry-run", "", "observer", "run-once", "--dry-run")
	if !strings.Contains(stdout, "rendered prompt") && !strings.Contains(stdout, "nothing to extract") {
		t.Errorf("observer run-once output missing expected marker: %s", stdout)
	}

	// doctor reports the wired state + observer line count
	stdout, _ = run(t, "doctor", "", "doctor")
	if !strings.Contains(stdout, "bough installed") {
		t.Errorf("doctor missing 'bough installed' marker: %s", stdout)
	}
	if !strings.Contains(stdout, "observations.jsonl") {
		t.Errorf("doctor missing observations.jsonl reference: %s", stdout)
	}

	// uninstall + list back to empty
	run(t, "hook uninstall", "", "hook", "uninstall")
	stdout, _ = run(t, "hook list after uninstall", "", "hook", "list")
	if !strings.Contains(stdout, "(no hooks wired") {
		t.Errorf("post-uninstall list should report empty: %s", stdout)
	}

	// settings.json should round-trip to `{}` (= no hand-edited
	// content was seeded, so uninstall drops the hooks key entirely).
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.json after uninstall: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	if _, ok := parsed["hooks"]; ok {
		t.Errorf("hooks key should be removed after uninstall: %s", data)
	}
}

// buildBoughBinary compiles the actual cmd/bough binary into a
// throwaway directory so the integration test exercises the real
// CLI surface end-to-end. The build cost is paid once per test
// run and amortised across the per-step assertions.
func buildBoughBinary(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bough-hooks-conf-*")
	if err != nil {
		t.Fatalf("mktempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	bin := filepath.Join(dir, "bough")
	repoRoot := findRepoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/bough")
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build cmd/bough: %v\n%s", err, out)
	}
	return bin
}

// findRepoRoot resolves the bough module root via `go env GOMOD`
// so the test still works when invoked from any nested package.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" {
		t.Fatalf("go env GOMOD empty — not in a module")
	}
	return filepath.Dir(mod)
}
