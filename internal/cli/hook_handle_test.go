package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveMonorepoRoot covers the v0.9.10 ECC-model routing: every
// sub-repo / worktree session must pool into the one monorepo project
// (the dir holding .bough.yaml), so observations land in a single
// homunculus project instead of fragmenting a .bough/ into every cwd.
func TestResolveMonorepoRoot(t *testing.T) {
	// a worktree path resolves to the monorepo parent (before /.worktrees/)
	if got := resolveMonorepoRoot("/x/mono/.worktrees/F-feat/auba-api"); got != "/x/mono" {
		t.Errorf("worktree: got %q want /x/mono", got)
	}

	// walk up to the nearest ancestor holding the .bough.yaml marker
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".bough.yaml"), []byte("schema_version: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "auba-api", "server")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveMonorepoRoot(sub); got != root {
		t.Errorf("marker walk-up: got %q want %q", got, root)
	}

	// no worktree, no marker → fall back to the original cwd
	bare := t.TempDir()
	if got := resolveMonorepoRoot(bare); got != bare {
		t.Errorf("fallback: got %q want %q", got, bare)
	}
}
