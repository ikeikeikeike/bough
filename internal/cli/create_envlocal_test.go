package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/ikeikeikeike/bough/internal/config"
)

// TestRenderEnvLocals_BestEffort is the v0.9.19 regression: a repo whose
// env_local template fails to render must NOT abort create — it used to return
// an error that bypassed the worktree-path stdout emit + failure summary. It is
// now best-effort like runPostCreateHooks and returns the failed repo so the
// caller can fold it into the summary while still emitting the worktree path.
func TestRenderEnvLocals_BestEffort(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		Repositories: []config.Repository{
			// {{ .NoSuchField }} on the struct Context fails Execute.
			{Name: "demo", EnvLocal: map[string]string{"BAD": "{{ .NoSuchField }}"}},
		},
	}
	failed := renderEnvLocals(io.Discard, cfg, root, "F-test", nil, map[string]int{}, map[string]bool{})
	if len(failed) != 1 {
		t.Fatalf("renderEnvLocals returned %d failures, want 1 (must not abort): %v", len(failed), failed)
	}
	if failed[0].Repo != "demo" {
		t.Errorf("failure entry has repo %q, want %q", failed[0].Repo, "demo")
	}
	if !strings.Contains(failed[0].Detail, "render") {
		t.Errorf("failure entry Detail missing 'render': %q", failed[0].Detail)
	}
}

// TestRenderEnvLocals_SkipAndEmpty: a skipped repo and a repo with no env_local
// produce no failures (and no panic).
func TestRenderEnvLocals_SkipAndEmpty(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		Repositories: []config.Repository{
			{Name: "skipped", EnvLocal: map[string]string{"BAD": "{{ .NoSuchField }}"}},
			{Name: "noenv"},
		},
	}
	failed := renderEnvLocals(io.Discard, cfg, root, "F-test", nil, map[string]int{}, map[string]bool{"skipped": true})
	if len(failed) != 0 {
		t.Errorf("want 0 failures (one skipped, one without env_local), got %v", failed)
	}
}
