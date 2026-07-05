package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("UserHomeDir unavailable: %v", err)
	}
	cases := map[string]string{
		"~":         home,
		"~/":        home,
		"~/x":       filepath.Join(home, "x"),
		"~/a/b":     filepath.Join(home, "a", "b"),
		"~user/x":   "~user/x", // another user's home is left untouched
		"/abs/path": "/abs/path",
		"rel/path":  "rel/path",
		"":          "",
		"foo~bar":   "foo~bar", // a tilde that is not leading
	}
	for in, want := range cases {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExpandHomeStrict(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("UserHomeDir unavailable: %v", err)
	}
	cases := map[string]string{
		"~":         home,
		"~/":        home,
		"~/x":       filepath.Join(home, "x"),
		"~user/x":   "~user/x", // another user's home is left untouched
		"/abs/path": "/abs/path",
		"":          "",
	}
	for in, want := range cases {
		got, err := ExpandHomeStrict(in)
		if err != nil {
			t.Errorf("ExpandHomeStrict(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ExpandHomeStrict(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExpandHomeStrict_UserHomeDirFailure is the regression guard for
// the ecc_import.go call site: on a genuinely unresolvable "~" (e.g.
// $HOME unset in a minimal container), the strict variant must surface
// the real cause instead of silently continuing with a literal,
// un-expanded "~/..." path that later fails with an unrelated,
// confusing error (e.g. os.Stat "no such file or directory").
func TestExpandHomeStrict_UserHomeDirFailure(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := ExpandHomeStrict("~/foo"); err == nil {
		t.Error("ExpandHomeStrict with $HOME unset = nil error, want an error")
	}
	// A path with no leading "~" never touches UserHomeDir, so it must
	// not error even with $HOME unset.
	if got, err := ExpandHomeStrict("/abs/path"); err != nil || got != "/abs/path" {
		t.Errorf("ExpandHomeStrict(%q) with $HOME unset = (%q, %v), want (%q, nil)", "/abs/path", got, err, "/abs/path")
	}
}

// TestExpandHome_UserHomeDirFailureFallsBackUnchanged pins ExpandHome's
// own contract (unlike ExpandHomeStrict, it never errors) now that it
// is implemented in terms of ExpandHomeStrict.
func TestExpandHome_UserHomeDirFailureFallsBackUnchanged(t *testing.T) {
	t.Setenv("HOME", "")
	if got := ExpandHome("~/foo"); got != "~/foo" {
		t.Errorf("ExpandHome with $HOME unset = %q, want unchanged %q", got, "~/foo")
	}
}
