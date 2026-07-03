package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/config"
	"github.com/ikeikeikeike/bough/internal/termio"
)

// TestRunPostCreateHooks_BackgroundedChildDoesNotHang is the
// regression tripwire for the exec-writer contract: hook children must
// inherit the raw fd (termio.ExecWriter), not the SyncWriter. Handing
// os/exec a non-*os.File writer makes it substitute a pipe + copy
// goroutine, and Cmd.Wait then blocks until pipe EOF — which a
// backgrounded grandchild that inherited the fd holds open for its
// whole lifetime. Here the hook backgrounds `sleep 10`; create must
// return as soon as bash itself exits, not after the sleep.
func TestRunPostCreateHooks_BackgroundedChildDoesNotHang(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	go func() { _, _ = io.Copy(io.Discard, r) }() // drain so a full pipe can never block the hook

	// SyncWriter over a real *os.File — the production shape after
	// runCreate's termio.Wrap (stderr fd underneath).
	stderr := termio.NewSyncWriter(w)

	worktreeRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktreeRoot, "demo"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := &config.Config{Repositories: []config.Repository{{
		Name:       "demo",
		PostCreate: []string{"sleep 10 & echo hook-done"},
	}}}

	start := time.Now()
	failed := runPostCreateHooks(context.Background(), stderr, cfg, worktreeRoot, nil)
	elapsed := time.Since(start)

	if len(failed) != 0 {
		t.Fatalf("post_create failed: %v", failed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("post_create blocked %v on a backgrounded child (exec got a piped writer instead of the raw fd)", elapsed)
	}
}
