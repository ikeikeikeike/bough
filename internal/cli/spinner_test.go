package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/termio"
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

// TestIsInteractive_unwrapsSyncWriter: runCreate hands the spinner a
// termio.SyncWriter, so TTY detection must look through the wrapper at
// the underlying fd — a wrapped pipe stays non-interactive (as here),
// a wrapped terminal stays interactive.
func TestIsInteractive_unwrapsSyncWriter(t *testing.T) {
	if isInteractive(termio.NewSyncWriter(&bytes.Buffer{})) {
		t.Errorf("SyncWriter over a buffer reported interactive")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	if isInteractive(termio.NewSyncWriter(w)) {
		t.Errorf("SyncWriter over a pipe reported interactive")
	}
}

// safeBuf is a mutex-guarded bytes.Buffer: the spinner goroutine
// writes (through the SyncWriter) while the test goroutine reads, and
// bytes.Buffer alone would race under -race.
type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *safeBuf) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Len()
}

// TestSpinner_PaintsAsStatusThroughSyncWriter is the #67 coordination
// contract: through a termio.SyncWriter the spinner must register its
// frame as the writer's status line (so a concurrent plugin log line
// erases + repaints it) and Stop() must deregister it — after Stop, a
// foreign write is plain passthrough again.
func TestSpinner_PaintsAsStatusThroughSyncWriter(t *testing.T) {
	var buf safeBuf
	sw := termio.NewSyncWriter(&buf)
	s := &spinner{w: sw, tty: true, stop: make(chan struct{}), done: make(chan struct{})}
	go s.run("booting")

	// Wait for frame 0 (painted immediately by run()).
	deadline := time.After(2 * time.Second)
	for !strings.Contains(buf.String(), "booting") {
		select {
		case <-deadline:
			t.Fatal("spinner never painted through the SyncWriter")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// A concurrent log line must be framed by erase + repaint, proving
	// the frame was registered as the SyncWriter's status. The whole
	// erase + line + repaint sequence happens under the SyncWriter
	// mutex, so it lands contiguously despite the ticking spinner.
	if _, err := io.WriteString(sw, "[WARN] plugin: x\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(buf.String(), "\r\x1b[K[WARN] plugin: x\n") {
		t.Errorf("log line not erase-framed: %q", buf.String())
	}

	s.Stop()
	// After Stop the status must be deregistered: a foreign write is
	// appended verbatim with no erase prefix. (Stop waited for run()
	// to exit, so nothing writes concurrently past this point.)
	mark := buf.Len()
	if _, err := io.WriteString(sw, "after\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := buf.String()[mark:]; got != "after\n" {
		t.Errorf("write after Stop not passthrough: %q", got)
	}
}

// TestSpinner_StopOnInertIsSafe guards the hook path: an inert spinner's
// Stop must never touch its nil channels (no panic).
func TestSpinner_StopOnInertIsSafe(t *testing.T) {
	sp := startSpinner(&bytes.Buffer{}, "x")
	sp.Stop()
}

// TestSpinner_AnimatesAndStops exercises the TTY animation path the
// inert-only tests never reach: run() must paint frame 0 (carrying the
// message) immediately, and Stop() must observe the stop signal, unblock,
// and stay panic-free on a second call (the sync.Once guard).
func TestSpinner_AnimatesAndStops(t *testing.T) {
	pr, pw := io.Pipe()
	s := &spinner{w: termio.NewSyncWriter(pw), tty: true, stop: make(chan struct{}), done: make(chan struct{})}

	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := pr.Read(buf) // blocks until run() writes frame 0
		got <- append([]byte(nil), buf[:n]...)
		_, _ = io.Copy(io.Discard, pr) // drain later frames / clear-line so run()/Stop() never block
	}()
	go s.run("booting")

	select {
	case frame := <-got:
		if !bytes.Contains(frame, []byte("booting")) {
			t.Errorf("first spinner frame missing the message: %q", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("spinner never painted a frame")
	}

	// Stop() twice: it must unblock (run() observed stop) and the second
	// call must be a no-op, not a double-close panic.
	stopped := make(chan struct{})
	go func() { s.Stop(); s.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("spinner Stop() hung")
	}
	_ = pw.Close()
}
