package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureSymlink covers the shared idempotent-symlink helper used by the
// v0.9.20 project-scoped skill deploy + the worktree skills link.
func TestEnsureSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "sub", "link") // parent created by ensureSymlink

	if err := ensureSymlink(target, link); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, _ := os.Readlink(link); got != target {
		t.Errorf("link target = %q, want %q", got, target)
	}
	// idempotent — re-run on an already-correct link is a no-op, no error
	if err := ensureSymlink(target, link); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
	// repoint a stale symlink
	target2 := filepath.Join(tmp, "target2")
	_ = os.MkdirAll(target2, 0o755)
	if err := ensureSymlink(target2, link); err != nil {
		t.Fatalf("repoint: %v", err)
	}
	if got, _ := os.Readlink(link); got != target2 {
		t.Errorf("repointed = %q, want %q", got, target2)
	}
	// refuse to clobber a real (non-symlink) dir
	realDir := filepath.Join(tmp, "real")
	_ = os.MkdirAll(realDir, 0o755)
	if err := ensureSymlink(target, realDir); err == nil {
		t.Errorf("ensureSymlink must refuse to clobber a real dir")
	}
}

// TestPruneStaleGlobalSkillLinks is the v0.9.20 migration safety test: only
// symlinks pointing INTO this project's evolved tree are removed; hand-authored
// real dirs and unrelated symlinks (e.g. playwright → nvm) are left untouched.
// globalDir is a temp dir — the real ~/.claude/skills is never touched.
func TestPruneStaleGlobalSkillLinks(t *testing.T) {
	tmp := t.TempDir()
	evolved := filepath.Join(tmp, "homunculus", "evolved", "skills")
	_ = os.MkdirAll(filepath.Join(evolved, "myskill"), 0o755)
	other := filepath.Join(tmp, "other")
	_ = os.MkdirAll(other, 0o755)
	global := filepath.Join(tmp, "global")
	_ = os.MkdirAll(global, 0o755)

	// (a) stale link pointing into evolved → must be pruned
	if err := os.Symlink(filepath.Join(evolved, "myskill"), filepath.Join(global, "myskill")); err != nil {
		t.Fatal(err)
	}
	// (b) unrelated symlink (playwright-style) → must be left
	if err := os.Symlink(other, filepath.Join(global, "playwright")); err != nil {
		t.Fatal(err)
	}
	// (c) hand-authored real dir → must be left
	_ = os.MkdirAll(filepath.Join(global, "handauthored"), 0o755)

	pruneStaleGlobalSkillLinks(io.Discard, global, evolved)

	if _, err := os.Lstat(filepath.Join(global, "myskill")); !os.IsNotExist(err) {
		t.Errorf("stale link into the evolved tree was not pruned")
	}
	if _, err := os.Lstat(filepath.Join(global, "playwright")); err != nil {
		t.Errorf("unrelated symlink was wrongly pruned: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(global, "handauthored")); err != nil {
		t.Errorf("hand-authored real dir was wrongly removed: %v", err)
	}
}

// TestDeploySkills_SelfCancelGuard is the v0.9.20 self-review fix: when the
// project dir and the global dir are the same (the monorepo root resolves to
// $HOME), the prune must be SKIPPED so it does not delete the project links just
// created (otherwise skills would silently never deploy).
func TestDeploySkills_SelfCancelGuard(t *testing.T) {
	tmp := t.TempDir()
	evolved := filepath.Join(tmp, "homunculus", "evolved", "skills")
	_ = os.MkdirAll(filepath.Join(evolved, "myskill"), 0o755)
	_ = os.WriteFile(filepath.Join(evolved, "myskill", "SKILL.md"), []byte("# myskill\n"), 0o644)

	dir := filepath.Join(tmp, "claude", "skills") // projectDir == globalDir
	deploySkills(io.Discard, io.Discard, evolved, dir, dir)

	link := filepath.Join(dir, "myskill")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("project skill link was wrongly removed by a self-cancelling prune: %v", err)
	}
	if want := filepath.Join(evolved, "myskill"); got != want {
		t.Errorf("link = %q, want %q", got, want)
	}
}

// TestDeploySkills_LinksAndPrunes covers the normal path: link the evolved skill
// into projectDir and prune a stale link from a distinct globalDir.
func TestDeploySkills_LinksAndPrunes(t *testing.T) {
	tmp := t.TempDir()
	evolved := filepath.Join(tmp, "evolved", "skills")
	_ = os.MkdirAll(filepath.Join(evolved, "s1"), 0o755)
	_ = os.WriteFile(filepath.Join(evolved, "s1", "SKILL.md"), []byte("# s1\n"), 0o644)
	projectDir := filepath.Join(tmp, "proj", ".claude", "skills")
	globalDir := filepath.Join(tmp, "global")
	_ = os.MkdirAll(globalDir, 0o755)
	if err := os.Symlink(filepath.Join(evolved, "s1"), filepath.Join(globalDir, "s1")); err != nil {
		t.Fatal(err)
	}

	deploySkills(io.Discard, io.Discard, evolved, projectDir, globalDir)

	if got, err := os.Readlink(filepath.Join(projectDir, "s1")); err != nil || got != filepath.Join(evolved, "s1") {
		t.Errorf("project link not created correctly: got=%q err=%v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(globalDir, "s1")); !os.IsNotExist(err) {
		t.Errorf("stale global link not pruned")
	}
}

// TestLinkWorktreeSkills verifies the worktree gets an absolute symlink to the
// monorepo's project-scoped skills, and a pre-existing real dir is not clobbered.
func TestLinkWorktreeSkills(t *testing.T) {
	root := t.TempDir()
	wt := filepath.Join(t.TempDir(), "wt")
	_ = os.MkdirAll(wt, 0o755)

	linkWorktreeSkills(io.Discard, root, wt)

	link := filepath.Join(wt, ".claude", "skills")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("worktree skills symlink not created: %v", err)
	}
	want := filepath.Join(root, ".claude", "skills")
	if got != want {
		t.Errorf("link = %q, want %q", got, want)
	}
	if !isDir(want) {
		t.Errorf("monorepo .claude/skills was not created at %q", want)
	}

	// real-dir guard: a pre-existing real <wt>/.claude/skills must survive
	wt2 := filepath.Join(t.TempDir(), "wt2")
	realSkills := filepath.Join(wt2, ".claude", "skills")
	_ = os.MkdirAll(realSkills, 0o755)
	linkWorktreeSkills(io.Discard, root, wt2)
	if fi, _ := os.Lstat(realSkills); fi != nil && fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("a real worktree .claude/skills was clobbered into a symlink")
	}
}
