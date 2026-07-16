package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ikeikeikeike/bough/internal/homunculus"
)

// TestResolveIdentityRoot_AgreesWithEvolveUnderRelocatingOverride is the
// regression for #60: `bough evolve` deploys project-scoped artifacts into
// homunculus.DetectIdentity(resolveMonorepoRoot(cwd)).Root — the .bough.yaml
// MARKER dir — while `bough create` / `bough backfill` used to anchor their
// worktree symlink source on loadConfigAndRoot's root, which APPLIES a
// relocating `monorepo_root:` override on top of that same marker dir. Under
// such a config the two commands silently disagreed on where project skills
// live, and the worktree session loaded zero evolved artifacts even though
// `bough evolve` reported having written them.
//
// resolveIdentityRoot must stay anchored on the marker dir — same as evolve
// — independent of loadConfigAndRoot's override.
func TestResolveIdentityRoot_AgreesWithEvolveUnderRelocatingOverride(t *testing.T) {
	markerDir := t.TempDir()
	relocated := t.TempDir()
	yaml := "schema_version: 2\nmonorepo_root: \"" + relocated + "\"\nrepositories:\n" +
		"  - name: 'x'\n    branch_strategy: develop\nregistry:\n  path: \".bough-ports.json\"\n"
	if err := os.WriteFile(filepath.Join(markerDir, ".bough.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	// sanity: loadConfigAndRoot really does apply the override (this is the
	// existing, intentional behaviour for repo materialization — it must
	// NOT change).
	relocatedRoot, _, err := loadConfigAndRoot(&cobra.Command{}, markerDir)
	if err != nil {
		t.Fatalf("loadConfigAndRoot: %v", err)
	}
	if relocatedRoot != relocated {
		t.Fatalf("sanity check failed: loadConfigAndRoot should resolve to the override %q, got %q", relocated, relocatedRoot)
	}

	// the identity root must stay on the marker dir, NOT the override.
	identityRoot, err := resolveIdentityRoot(markerDir)
	if err != nil {
		t.Fatalf("resolveIdentityRoot: %v", err)
	}
	if identityRoot != markerDir {
		t.Errorf("resolveIdentityRoot must anchor on the .bough.yaml marker dir: got %q, want %q", identityRoot, markerDir)
	}

	// and it must agree exactly with what `bough evolve` resolves via
	// homunculus.DetectIdentity(resolveMonorepoRoot(cwd)) — the precise
	// mismatch #60 flagged between the two commands.
	ident, err := homunculus.DetectIdentity(resolveMonorepoRoot(markerDir))
	if err != nil {
		t.Fatalf("DetectIdentity: %v", err)
	}
	if identityRoot != ident.Root {
		t.Errorf("resolveIdentityRoot (%q) must equal evolve's ident.Root (%q) so deploy and link agree", identityRoot, ident.Root)
	}
}

// TestResolveIdentityRoot_EmptyCwdHintUsesGetwd covers the same
// empty-cwdHint→os.Getwd() fallback loadConfigAndRoot already has, since
// resolveIdentityRoot duplicates that same small resolution step for the
// un-relocated root.
func TestResolveIdentityRoot_EmptyCwdHintUsesGetwd(t *testing.T) {
	tmp := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(prev) }()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	// Read back via os.Getwd() (not the raw tmp string) so any OS-level
	// symlink normalization (e.g. macOS /var -> /private/var) is on both
	// sides of the comparison.
	wantCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolveIdentityRoot("")
	if err != nil {
		t.Fatalf("resolveIdentityRoot(\"\"): %v", err)
	}
	// no .bough.yaml anywhere up the chain from a bare tempdir → falls back
	// to cwd itself (mirrors resolveMonorepoRoot's own fallback contract).
	if got != wantCwd {
		t.Errorf("got %q, want cwd %q", got, wantCwd)
	}
}
