package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestProcessGroupKillReapsGrandchild verifies the mechanism v0.9.19's L4 fix
// relies on: a child started with Setpgid leads its own process group, so
// syscall.Kill(-childpid, SIGKILL) reaps a GRANDCHILD it spawned. That is how
// runObserverOnceQuiet's Cancel kills an orphaned `claude --print` on daemon
// shutdown instead of leaving it parented to init.
func TestProcessGroupKillReapsGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	// sh (child) spawns `sleep 60` (grandchild), records its pid, then waits.
	c := exec.Command("sh", "-c", "sleep 60 & echo $! > "+pidFile+"; wait")
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }() // belt + suspenders

	// Wait for the grandchild pid to be recorded.
	var gpid int
	for i := 0; i < 200; i++ {
		if b, err := os.ReadFile(pidFile); err == nil {
			if p, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && p > 0 {
				gpid = p
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if gpid == 0 {
		t.Fatal("grandchild pid never recorded")
	}

	// Kill the whole process group — the fix's Cancel behavior.
	if err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("group kill: %v", err)
	}
	_ = c.Wait()

	// The grandchild must be gone (signal 0 → error once reaped).
	dead := false
	for i := 0; i < 200; i++ {
		if err := syscall.Kill(gpid, 0); err != nil {
			dead = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !dead {
		t.Errorf("grandchild pid %d still alive after the process-group kill", gpid)
	}
}
