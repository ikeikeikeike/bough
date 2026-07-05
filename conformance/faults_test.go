//go:build darwin || linux

package conformance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestFaultDatadir_NestingDefeatsBothMkdirShapes is the regression
// guard for issue #73's core subtlety: Fault_DatadirPermission drives
// Up with a datadir two levels below a 0o000 parent so BOTH
// datadir-prep shapes plugins use fail at their synchronous mkdir —
//
//   - mysql / redis / elasticsearch: os.MkdirAll(Datadir)
//   - postgres:                      os.MkdirAll(filepath.Dir(Datadir))
//
// The pre-fix one-level path (faultDir/data) made filepath.Dir ==
// faultDir, which already exists, so the postgres shape was a no-op and
// Up wrongly succeeded (a false-red once SkipDatadirPermission was
// removed). Both os.MkdirAll calls below must return a permission error.
func TestFaultDatadir_NestingDefeatsBothMkdirShapes(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — 0o000 does not block writes")
	}
	parent := t.TempDir()
	faultDir := filepath.Join(parent, "perm-fault")
	if err := os.MkdirAll(faultDir, 0o755); err != nil {
		t.Fatalf("mkdir fault dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(faultDir, 0o700) })
	if err := os.Chmod(faultDir, 0o000); err != nil {
		t.Skipf("chmod 0o000: %v (cannot test this path)", err)
	}

	datadir := faultDatadir(faultDir)

	// mysql / redis / elasticsearch shape: mkdir the datadir itself.
	if err := os.MkdirAll(datadir, 0o755); !errors.Is(err, os.ErrPermission) {
		t.Errorf("MkdirAll(datadir) err = %v, want a permission error (mysql/redis/es shape)", err)
	}
	// postgres shape: mkdir only the parent of the datadir.
	if err := os.MkdirAll(filepath.Dir(datadir), 0o755); !errors.Is(err, os.ErrPermission) {
		t.Errorf("MkdirAll(filepath.Dir(datadir)) err = %v, want a permission error (postgres shape)", err)
	}
}

func TestDatadirFaultBackend_DefaultsToNix(t *testing.T) {
	if got := applyDefaults(Config{}).DatadirFaultBackend; got != "nix" {
		t.Errorf("default DatadirFaultBackend = %q, want %q", got, "nix")
	}
	// An explicit value must survive applyDefaults so an external plugin
	// whose host-process backend is not keyed on "nix" can override it.
	if got := applyDefaults(Config{DatadirFaultBackend: "podman"}).DatadirFaultBackend; got != "podman" {
		t.Errorf("explicit DatadirFaultBackend = %q, want it preserved as %q", got, "podman")
	}
}

// TestIsBackendBinaryMissing pins the textual match that turns an
// inconclusive datadir fault (Up fell through to a backend launch on a
// host without that backend installed) into a Skip rather than a
// spurious pass. The "executable file not found" phrase is the
// exec.ErrNotFound wording, identical on linux and darwin, and survives
// the go-plugin gRPC string round-trip.
func TestIsBackendBinaryMissing(t *testing.T) {
	missing := errors.New(`mysql: nix run: exec: "nix": executable file not found in $PATH`)
	if !isBackendBinaryMissing(missing) {
		t.Errorf("isBackendBinaryMissing(%v) = false, want true", missing)
	}
	// The real fault (the mkdir failing first) must NOT read as missing.
	perm := errors.New("mysql: mkdir datadir: permission denied")
	if isBackendBinaryMissing(perm) {
		t.Errorf("isBackendBinaryMissing(%v) = true, want false (real fault, not a missing binary)", perm)
	}
	if isBackendBinaryMissing(nil) {
		t.Errorf("isBackendBinaryMissing(nil) = true, want false")
	}
}
