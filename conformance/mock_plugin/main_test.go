package main

import (
	"context"
	"os"
	"testing"

	api "github.com/ikeikeikeike/bough/plugins/engine/api"
)

// TestMockProvider_Up_RollbackOnlyNewlyBound is the regression guard
// for the #17-review finding: Up's sentinel-write-failure rollback
// used to iterate every port in req.Ports (including roles reused
// from a prior successful Up — up-or-reuse's whole point), not just
// the ones this call actually bound, and tore down a healthy,
// unrelated listener as a side effect of an unrelated write error.
func TestMockProvider_Up_RollbackOnlyNewlyBound(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — chmod 0o000 does not block writes; cannot force the sentinel-write failure")
	}
	p := newMockProvider()
	datadir := t.TempDir()

	// First Up: bind role "a" successfully (a real prior successful Up,
	// the precondition up-or-reuse exists for).
	if err := p.Up(context.Background(), &api.UpReq{
		Ports:   []api.PortSpec{{Role: "a", Port: singlePortLow}},
		Datadir: datadir,
	}); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	if _, ok := p.listeners[singlePortLow]; !ok {
		t.Fatalf("role a listener not bound after first Up")
	}

	// Force the second call's sentinel write to fail by making datadir
	// unwritable, without touching the already-bound listener. Restore
	// permissions on cleanup so t.TempDir's own RemoveAll can succeed.
	t.Cleanup(func() { _ = os.Chmod(datadir, 0o755) })
	if err := os.Chmod(datadir, 0o000); err != nil {
		t.Skipf("chmod datadir 0o000: %v (cannot force this path)", err)
	}

	// Second Up: reuse role "a" (up-or-reuse, no-op — the exact
	// precondition the rollback bug ignored) plus newly bind role "b".
	err := p.Up(context.Background(), &api.UpReq{
		Ports: []api.PortSpec{
			{Role: "a", Port: singlePortLow},
			{Role: "b", Port: singlePortLow + 1},
		},
		Datadir: datadir,
	})
	if err == nil {
		t.Fatal("second Up: want error from the unwritable-datadir sentinel write, got nil")
	}

	if _, ok := p.listeners[singlePortLow]; !ok {
		t.Error("role a listener (reused, healthy) was rolled back despite being unrelated to this failure — regression")
	}
	if _, ok := p.listeners[singlePortLow+1]; ok {
		t.Error("role b listener (newly bound this call) was NOT rolled back")
	}
}
