package config

import (
	"os"
	"testing"
)

// writeFile is a tiny shim around os.WriteFile that t.Fatals on error and
// keeps the test bodies focused on the YAML payload rather than the
// scaffolding around it.
func writeFile(t *testing.T, path, contents string) error {
	t.Helper()
	return os.WriteFile(path, []byte(contents), 0o644)
}
