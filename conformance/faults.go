package conformance

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	engineapi "github.com/ikeikeikeike/bough/plugins/engine/api"

	"github.com/ikeikeikeike/bough/internal/pluginhost"
)

// runFaults exercises three deliberately-broken inputs and asserts
// that Up surfaces a real error for each. A plugin that swallows
// these failures silently is the worst failure mode: bough's
// allocator sees Up succeed, hands the host an unusable URL, and the
// downstream service crashes far from the actual root cause.
//
// Each fault is opt-out via the Skip* knobs in Config because some
// plugins genuinely cannot simulate them (e.g. a cluster-side
// provisioner that has no concept of a local socket cannot have its
// port preempted by `net.Listen`).
//
// Fault_ImagePullFailure and Fault_DatadirPermission each force the
// backend that makes their fault real: image-pull forces docker (only
// the docker path pulls an image); datadir-permission forces the
// host-process backend (cfg.DatadirFaultBackend), because only that
// path prepares Datadir with a synchronous os.MkdirAll inside Up and
// so surfaces an un-writable parent as an Up error — the docker path
// merely bind-mounts and the container's write fails asynchronously,
// long after Up has already returned nil.
func runFaults(t *testing.T, cfg Config) {
	t.Helper()

	t.Run("Fault_PortConflict", func(t *testing.T) {
		if cfg.SkipPortConflict {
			t.Skip("SkipPortConflict")
		}
		runFaultPortConflict(t, cfg)
	})

	t.Run("Fault_DatadirPermission", func(t *testing.T) {
		if cfg.SkipDatadirPermission {
			t.Skip("SkipDatadirPermission")
		}
		runFaultDatadirPermission(t, cfg)
	})

	t.Run("Fault_ImagePullFailure", func(t *testing.T) {
		if cfg.SkipImagePullFailure {
			t.Skip("SkipImagePullFailure")
		}
		runFaultImagePullFailure(t, cfg)
	})
}

func runFaultPortConflict(t *testing.T, cfg Config) {
	prov, cleanup, port := spawnFreshAndPickPort(t, cfg)
	defer cleanup()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("could not bind sidecar listener on :%d: %v "+
			"(test cannot prove the plugin rejects port conflict)", port, err)
	}
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), cfg.UpTimeout)
	defer cancel()
	datadir := t.TempDir()
	upErr := prov.Up(ctx, &engineapi.UpReq{
		Ports:        []engineapi.PortSpec{{Role: cfg.MainPortRole, Port: port}},
		Datadir:      datadir,
		WorktreeRoot: t.TempDir(),
		SocketDir:    t.TempDir(),
		Extras:       mergeExtras(cfg),
	})
	if upErr == nil {
		// The plugin claimed success on a port we already hold.
		// Best-effort teardown to avoid leaving a real container
		// running, then fail the contract check.
		_ = prov.Down(ctx, &engineapi.DownReq{Ports: []int{port}, GracefulTimeoutSec: 5})
		_ = prov.Cleanup(ctx, datadir, []int{port})
		t.Errorf("Up on a port already held by a sidecar listener must fail; got nil error")
		return
	}
	t.Logf("Up surfaced port-conflict as: %v", upErr)
}

func runFaultDatadirPermission(t *testing.T, cfg Config) {
	if os.Getuid() == 0 {
		t.Skip("running as root — chmod 0o000 does not block writes; cannot test this path")
	}

	prov, cleanup, port := spawnFreshAndPickPort(t, cfg)
	defer cleanup()

	parent := t.TempDir()
	faultDir := filepath.Join(parent, "perm-fault")
	if err := os.MkdirAll(faultDir, 0o755); err != nil {
		t.Fatalf("mkdir fault dir: %v", err)
	}
	// Restore on cleanup so t.TempDir's RemoveAll can succeed.
	t.Cleanup(func() { _ = os.Chmod(faultDir, 0o700) })
	if err := os.Chmod(faultDir, 0o000); err != nil {
		t.Skipf("chmod fault dir 0o000: %v (cannot test this path)", err)
	}

	// Force the host-process backend (see runFaults' doc): only that
	// path mkdirs Datadir synchronously inside Up, so a 0o000 parent is
	// a deterministic, cross-platform Up error. The mkdir precedes the
	// backend-binary exec, so this fires even when the backend binary
	// itself is absent — isBackendBinaryMissing below distinguishes the
	// rare fall-through.
	extras := mergeExtras(cfg)
	extras["backend"] = cfg.DatadirFaultBackend

	datadir := faultDatadir(faultDir)
	ctx, cancel := context.WithTimeout(t.Context(), cfg.UpTimeout)
	defer cancel()
	upErr := prov.Up(ctx, &engineapi.UpReq{
		Ports:        []engineapi.PortSpec{{Role: cfg.MainPortRole, Port: port}},
		Datadir:      datadir,
		WorktreeRoot: t.TempDir(),
		SocketDir:    t.TempDir(),
		Extras:       extras,
	})
	if upErr == nil {
		_ = prov.Down(ctx, &engineapi.DownReq{Ports: []int{port}, GracefulTimeoutSec: 5})
		_ = prov.Cleanup(ctx, datadir, []int{port})
		t.Errorf("Up with an un-writable datadir parent must fail; got nil error")
		return
	}
	if isBackendBinaryMissing(upErr) {
		t.Skipf("Fault_DatadirPermission inconclusive: the datadir mkdir should have "+
			"failed first, but Up reached the %q backend launch and it is not installed (%v)",
			extras["backend"], upErr)
	}
	t.Logf("Up surfaced datadir-permission as: %v", upErr)
}

// faultDatadir returns the datadir path Fault_DatadirPermission drives
// Up with: two levels below the 0o000 faultDir. The nesting defeats
// both datadir-prep shapes plugins use:
//
//   - mysql / redis / elasticsearch mkdir Datadir itself, so any path
//     under the un-writable faultDir fails.
//   - postgres mkdirs only filepath.Dir(Datadir) — it must not
//     pre-create $PGDATA or initdb refuses to run. A single level
//     (faultDir/data) makes filepath.Dir == faultDir, which already
//     exists, so the mkdir is a no-op and Up wrongly succeeds. Two
//     levels put filepath.Dir at faultDir/data, which cannot be created
//     under the 0o000 faultDir — so postgres fails at its mkdir too.
func faultDatadir(faultDir string) string {
	return filepath.Join(faultDir, "data", "db")
}

// isBackendBinaryMissing reports whether err is the "plugin tried to
// exec its host-process backend binary but it is not on PATH" shape.
// Matched textually because the error crosses the go-plugin gRPC
// boundary as a string (the api server's errString → the client's
// errors.New), which drops the exec.ErrNotFound sentinel; the phrase
// "executable file not found" is identical on linux and darwin.
//
// Fault_DatadirPermission skips (rather than passes) on this shape so
// an inconclusive run — where the synchronous datadir mkdir did not
// fire and Up fell through to a backend launch the host cannot perform
// — never reads as a green contract check.
func isBackendBinaryMissing(err error) bool {
	return err != nil && strings.Contains(err.Error(), "executable file not found")
}

func runFaultImagePullFailure(t *testing.T, cfg Config) {
	prov, cleanup, port := spawnFreshAndPickPort(t, cfg)
	defer cleanup()

	// Start from mergeExtras so `backend=docker` is forced — without it
	// the plugin (with nix on PATH from a devShell) would take the
	// services-flake path and never touch the bogus image, leaving
	// Up returning nil and the contract-check vacuous.
	extras := mergeExtras(cfg)
	// Force the plugin onto an image ref that cannot resolve.
	// We use a registry-prefix that the docker registry will reject
	// as "manifest unknown" rather than a random string that might
	// hit a 404 at the auth layer instead.
	extras["docker.image"] = "ghcr.io/ikeikeikeike/bough-conformance-does-not-exist:nope"

	datadir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), cfg.UpTimeout)
	defer cancel()
	upErr := prov.Up(ctx, &engineapi.UpReq{
		Ports:        []engineapi.PortSpec{{Role: cfg.MainPortRole, Port: port}},
		Datadir:      datadir,
		WorktreeRoot: t.TempDir(),
		SocketDir:    t.TempDir(),
		Extras:       extras,
	})
	if upErr == nil {
		_ = prov.Down(ctx, &engineapi.DownReq{Ports: []int{port}, GracefulTimeoutSec: 5})
		_ = prov.Cleanup(ctx, datadir, []int{port})
		t.Errorf("Up with a non-existent image must fail; got nil error")
		return
	}
	// We intentionally do not pin error text — different registries
	// phrase "manifest unknown" / "pull failed" / "denied" differently.
	// The contract bound here is just "must surface a non-nil error".
	t.Logf("Up surfaced image-pull-failure as: %v", upErr)
}

// spawnFreshAndPickPort starts a brand-new plugin subprocess (each
// fault gets its own process so a panic in one cannot poison the
// next) and returns the configured main role's range Low as the port
// to drive Up with. Caller MUST defer the returned cleanup func.
//
// Faults always target a single role (port-conflict can only collide
// on one port at a time, datadir-perm has nothing to do with port
// count, image-pull is plugin-image-scoped) — so for multi-port
// plugins we still bind just the main role for these tests, which is
// what Config.MainPortRole identifies.
func spawnFreshAndPickPort(t *testing.T, cfg Config) (engineapi.EngineProvider, func(), int) {
	t.Helper()
	prov, kill, err := pluginhost.DiscoverFromBinary(cfg.PluginBinary)
	if err != nil {
		t.Fatalf("spawn plugin %q: %v", cfg.PluginBinary, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ranges, err := prov.PortRangeDefault(ctx)
	if err != nil {
		kill()
		t.Fatalf("PortRangeDefault: %v", err)
	}
	mainRange, ok := ranges[cfg.MainPortRole]
	if !ok {
		kill()
		t.Fatalf("PortRangeDefault did not declare the configured main role %q; "+
			"set conformance.Config.MainPortRole to one of the plugin's declared roles", cfg.MainPortRole)
	}
	// Route through the same probe-and-bind picker allocateRoles uses
	// (lifecycle.go) instead of the raw, unscanned Low — otherwise a
	// stray process already bound to Low (the exact class of CI flake
	// pickFreePort exists to dodge) makes Up() fail on a port
	// collision, and Fault_DatadirPermission / Fault_ImagePullFailure
	// both only assert upErr != nil, so they'd false-pass without ever
	// having exercised the fault they exist to guard.
	return prov, kill, pickFreePort(mainRange.Low, mainRange.High, nil)
}
