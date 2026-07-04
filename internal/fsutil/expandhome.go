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
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
		}
	}
	return p
}
