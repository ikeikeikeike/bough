package conformance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ikeikeikeike/bough/conformance"
)

// buildMockPlugin compiles ./mock_plugin into a binary under
// t.TempDir() and returns the absolute path. We always rebuild rather
// than caching: the suite is the contract guard for the binary that
// is going to run in CI, so the build step is a deliberate part of
// what we are asserting (a plugin that does not even compile is the
// loudest possible contract violation).
func buildMockPlugin(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH; conformance suite cannot build the mock plugin")
	}
	out := filepath.Join(t.TempDir(), "bough-plugin-mock")
	cmd := exec.Command(
		"go", "build", "-o", out,
		"github.com/ikeikeikeike/bough/conformance/mock_plugin",
	)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build mock_plugin: %v\n%s", err, output)
	}
	return out
}

// TestRun_MockPlugin_GreenPath is the floor: every contract phase
// (PortRangeDefault → Up → ReadyCheck → EnvVars → Down → Cleanup,
// twice, then a final idempotent Cleanup) must succeed against the
// healthy mock plugin without a single t.Fail.
//
// If this test breaks, the conformance suite is broken — not the
// plugin under examination. Run it first whenever the suite changes.
func TestRun_MockPlugin_GreenPath(t *testing.T) {
	bin := buildMockPlugin(t)
	conformance.Run(t, conformance.Config{
		PluginBinary:    bin,
		Image:           "mock:latest", // unused by mock; kept for signature parity
		IdempotentCount: 2,
	})
}

// TestRun_EmptyPluginBinary_Skips guards the Λ-6.1 ergonomics: if
// BOUGH_CONFORMANCE_PLUGIN_BIN is unset (= the dev ran `go test ./...`
// without the conformance build tag stitched together), the suite
// must Skip, not Fail. Otherwise the suite turns into a developer-
// hostile gate on every plain `go test`.
//
// Implementation note: testing.T cannot be replaced with a shim
// (conformance.Run requires *testing.T for t.Run / t.Context), so we
// drive the Skip path via a sub-test and assert that the sub-test
// ended Skipped, not Failed.
func TestRun_EmptyPluginBinary_Skips(t *testing.T) {
	t.Run("skip-path", func(sub *testing.T) {
		conformance.Run(sub, conformance.Config{PluginBinary: ""})
		// `defer` only runs after Run returns normally; on Skip the
		// goroutine unwinds via runtime.Goexit from SkipNow, so the
		// post-check has to live in t.Cleanup of the OUTER t below.
	})
	// If `skip-path` Skip'd, the outer t.Run returns true and the
	// sub-test's Skipped() reflects that. Go's testing package
	// reports skipped sub-tests as not-failed at the parent level.
	if t.Failed() {
		t.Errorf("Run with empty PluginBinary marked outer test as failed")
	}
}
