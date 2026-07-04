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
