package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyFile_ContentParentDirAndMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o640); err != nil {
		t.Fatalf("write src: %v", err)
	}
	// dst under a not-yet-existing subdir → CopyFile must create it.
	dst := filepath.Join(dir, "sub", "nested", "dst.txt")
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "hello" {
		t.Errorf("dst content = %q, want %q", got, "hello")
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("dst mode = %o, want 0o640 (src mode preserved)", info.Mode().Perm())
	}
}

func TestCopyFile_OverwritesAndLeavesNoTmp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old-and-longer"), 0o644); err != nil {
		t.Fatalf("write dst: %v", err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "new" {
		t.Errorf("dst = %q, want %q (overwrite)", got, "new")
	}
	// Atomicity: no *.tmp sibling is left behind on success.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestCopyFile_MissingSrc(t *testing.T) {
	dir := t.TempDir()
	if err := CopyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "dst")); err == nil {
		t.Error("CopyFile with a missing src = nil, want an error")
	}
}

// TestCopyFile_CleansUpTmpOnRenameFailure guards the final os.Rename
// step: unlike every earlier failure branch in CopyFile (which all
// remove the temp file before returning), the final rename used to
// return the raw error with no cleanup, orphaning the fully-written
// temp file on disk. Renaming a regular file onto an existing
// non-empty directory reliably fails (EISDIR on both Linux and Darwin)
// without needing any platform-specific fault injection.
func TestCopyFile_CleansUpTmpOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "occupied"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}

	if err := CopyFile(src, dst); err == nil {
		t.Fatal("CopyFile onto an existing non-empty directory = nil, want an error")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover temp file after rename failure: %s", e.Name())
		}
	}
}
