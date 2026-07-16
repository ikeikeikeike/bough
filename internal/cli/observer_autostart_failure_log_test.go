package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ikeikeikeike/bough/internal/homunculus"
)

// TestDispatchObserverAutostart_LogsEarlyFailure is the regression test
// for a swallowed-error gap this retrospective review found:
// dispatchObserverAutostart's doc comment promises a spawn failure "stays
// diagnosable instead of vanishing without a trace", but the original
// code only appended to the log when startObserverDaemon returned a
// non-empty logPath — and startObserverDaemon returns "" for exactly its
// two earliest failure modes (resolveObserverProject / EnsureProjectDirs
// erroring), before a project-scoped log path can even be computed. On
// autostart, which runs on every UserPromptSubmit, that meant the most
// likely first-run failures (e.g. a permission problem materializing the
// homunculus project dir) left zero trace anywhere — not even the one
// place the feature's own docs point operators to.
//
// This reproduces an EnsureProjectDirs failure (a regular file occupying
// the path a project subdirectory needs to be a directory) and asserts
// the error lands in a project-independent fallback log instead of
// disappearing.
func TestDispatchObserverAutostart_LogsEarlyFailure(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("BOUGH_HOMUNCULUS_DIR", homeDir)

	root := t.TempDir()
	boughYAML := `schema_version: 1
monorepo_root: "."
repositories:
  - {name: a, branch_strategy: develop}
registry: {path: .worktree-ports.json}
instinct:
  observer:
    autostart: true
`
	if err := os.WriteFile(filepath.Join(root, ".bough.yaml"), []byte(boughYAML), 0o644); err != nil {
		t.Fatalf("write .bough.yaml: %v", err)
	}

	prev, _ := os.Getwd()
	defer func() { _ = os.Chdir(prev) }()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	// Resolve identity from the REAL (symlink-resolved) cwd, the same
	// value os.Getwd() inside resolveObserverConfig will return — on
	// macOS a t.TempDir() path is a /tmp symlink to /private/tmp, and
	// hashing the wrong spelling would seed the blocker file under a
	// different project_id than dispatchObserverAutostart resolves to.
	realCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	realRoot := resolveMonorepoRoot(realCwd)

	ident, layout, err := resolveObserverProject(realRoot)
	if err != nil {
		t.Fatalf("resolveObserverProject: %v", err)
	}
	// Block EnsureProjectDirs: put a plain file where a project
	// subdirectory needs to exist, so os.MkdirAll fails with ENOTDIR.
	if err := os.MkdirAll(filepath.Dir(layout.ProjectDir(ident.ID)), 0o755); err != nil {
		t.Fatalf("seed projects dir: %v", err)
	}
	if err := os.WriteFile(layout.ProjectDir(ident.ID), []byte("blocker"), 0o644); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}

	// Sanity: this setup really does make startObserverDaemon fail with
	// an empty logPath — otherwise this test would not be exercising the
	// gap it claims to.
	if _, _, logPath, err := startObserverDaemon(realRoot, 600); err == nil || logPath != "" {
		t.Fatalf("test setup bug: want (err != nil, logPath == \"\"), got (err=%v, logPath=%q)", err, logPath)
	}

	dispatchObserverAutostart(&cobra.Command{})

	fallback := filepath.Join(homunculus.NewLayout().Root, "observer-autostart-errors.log")
	raw, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatalf("fallback log was never written: %v", err)
	}
	if !strings.Contains(string(raw), "autostart:") {
		t.Errorf("fallback log = %q, want it to mention the autostart failure", raw)
	}
}
