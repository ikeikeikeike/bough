package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikeikeikeike/bough/internal/config"
)

// TestRunBackfill_RelinksExistingWorktrees is the regression for #61: before
// this, linkWorktreeArtifacts was wired ONLY into `bough create`, so a
// worktree that predates the project-scope evolved-artifact move — or was
// itself registered by an earlier `bough backfill` run before this fix —
// never got its .claude/{skills,agents,commands} symlinks and silently
// loaded zero evolved artifacts. backfill must relink EVERY discovered
// worktree dir, not just newly-registered ones.
func TestRunBackfill_RelinksExistingWorktrees(t *testing.T) {
	mono := t.TempDir()
	wtDir := filepath.Join(mono, "worktrees", "F-existing")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-populate a project-scoped skill so the relink has something real
	// to point at (mirrors what `bough evolve` would have already deployed).
	if err := os.MkdirAll(filepath.Join(mono, ".claude", "skills", "s1"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Registry: config.RegistryConfig{Path: filepath.Join(mono, ".bough-ports.json")}}
	var stderr bytes.Buffer
	if err := runBackfill(&stderr, cfg, mono, mono); err != nil {
		t.Fatalf("runBackfill: %v", err)
	}

	link := filepath.Join(wtDir, ".claude", "skills")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("backfill did not create the worktree skills symlink: %v", err)
	}
	if want := filepath.Join(mono, ".claude", "skills"); got != want {
		t.Errorf("link = %q, want %q", got, want)
	}

	// Re-running backfill against the now-ALREADY-REGISTERED worktree must
	// still relink — the relink must not be gated on "newly added to the
	// registry" (that was the exact gap #61 reported: pre-existing /
	// already-backfilled worktrees never got relinked).
	if err := runBackfill(&stderr, cfg, mono, mono); err != nil {
		t.Fatalf("second runBackfill: %v", err)
	}
	if got2, err := os.Readlink(link); err != nil || got2 != got {
		t.Errorf("relink on second run: got=%q err=%v, want unchanged %q", got2, err, got)
	}
}

// TestRunBackfill_NoWorktreesDirIsANoop preserves the pre-existing
// behaviour: an absent worktrees/ dir is not an error.
func TestRunBackfill_NoWorktreesDirIsANoop(t *testing.T) {
	mono := t.TempDir()
	cfg := &config.Config{Registry: config.RegistryConfig{Path: filepath.Join(mono, ".bough-ports.json")}}
	var stderr bytes.Buffer
	if err := runBackfill(&stderr, cfg, mono, mono); err != nil {
		t.Fatalf("runBackfill on a monorepo with no worktrees dir should be a no-op, got: %v", err)
	}
}
