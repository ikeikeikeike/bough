package cli

import (
	"bytes"
	"os"
	"testing"
)

// TestSpinner_InertOnNonTTY is the hook-safety contract: when stderr is
// not a terminal (the WorktreeCreate hook pipes it, CI captures it), the
// spinner must write nothing so the log stays plain and greppable.
func TestSpinner_InertOnNonTTY(t *testing.T) {
	var buf bytes.Buffer
	sp := startSpinner(&buf, "cloning something")
	sp.Stop()
	if buf.Len() != 0 {
		t.Errorf("spinner wrote %q to a non-TTY writer; want inert (empty)", buf.String())
	}
}

// TestIsInteractive_nonTerminalIsFalse covers both the non-*os.File case
// (a buffer in tests) and a real *os.File that is not a terminal (an
// os.Pipe, which mirrors the hook's piped stderr).
func TestIsInteractive_nonTerminalIsFalse(t *testing.T) {
	if isInteractive(&bytes.Buffer{}) {
		t.Errorf("bytes.Buffer reported interactive")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	if isInteractive(w) {
		t.Errorf("os.Pipe write end reported interactive")
	}
}

// TestSpinner_StopOnInertIsSafe guards the hook path: an inert spinner's
// Stop must never touch its nil channels (no panic).
func TestSpinner_StopOnInertIsSafe(t *testing.T) {
	sp := startSpinner(&bytes.Buffer{}, "x")
	sp.Stop()
}
