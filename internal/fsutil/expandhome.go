// Package fsutil holds the small filesystem helpers that were
// duplicated across bough's internal runtime packages (internal/cli,
// internal/registry, internal/gitwt): resolving a leading ~ and copying
// a file atomically.
package fsutil

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome resolves a leading ~ or ~/ to the user's home directory. A
// bare "~" and "~/x" expand; "~user" is left untouched (the stdlib has
// no portable way to resolve another user's home); on a UserHomeDir
// error the input is returned unchanged. This is the gitwt-derived
// behavior, adopted as the single canonical implementation.
func ExpandHome(p string) string {
	expanded, err := ExpandHomeStrict(p)
	if err != nil {
		return p
	}
	return expanded
}

// ExpandHomeStrict behaves like ExpandHome but surfaces a UserHomeDir
// failure instead of silently returning p unexpanded — for callers
// where an unresolved "~" must abort with the real cause rather than
// continue with a literal "~/..." path that later fails with an
// unrelated, confusing error (e.g. os.Stat "no such file or directory").
func ExpandHomeStrict(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(p[1:], "/")), nil
}
