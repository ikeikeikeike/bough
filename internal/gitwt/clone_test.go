package gitwt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsRemoteURL(t *testing.T) {
	remote := []string{
		"git@github.com:org/repo",
		"git@github.com:org/repo.git",
		"https://github.com/org/repo",
		"ssh://git@host/path",
		"file:///abs/path",
	}
	local := []string{
		"~/src/repo", "./repo", "../repo", "/abs/repo", "repo", "path/to/repo",
	}
	for _, s := range remote {
		if !isRemoteURL(s) {
			t.Errorf("isRemoteURL(%q) = false, want true (remote)", s)
		}
	}
	for _, s := range local {
		if isRemoteURL(s) {
			t.Errorf("isRemoteURL(%q) = true, want false (local)", s)
		}
	}
}

// TestClone_Local clones a real local git repo with --local and asserts
// the destination is a populated clone.
func TestClone_Local(t *testing.T) {
	src := t.TempDir()
	gitC := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", src}, args...)...)
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	gitC("init", "-q")
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitC("add", ".")
	gitC("commit", "-q", "-m", "init")

	dst := filepath.Join(t.TempDir(), "cloned")
	if err := NewRunner().Clone(context.Background(), src, dst, ""); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
		t.Errorf("cloned dst is not a git repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "f.txt")); err != nil {
		t.Errorf("cloned content missing: %v", err)
	}
}
