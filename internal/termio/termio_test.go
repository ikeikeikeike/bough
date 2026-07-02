package termio

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestSyncWriter_ConcurrentLinesStayWhole is the #67 regression proxy:
// two goroutines hammering whole lines through one SyncWriter (the
// spinner goroutine and hclog's plugin-log goroutine in production)
// must never interleave mid-line. Every output line has to be exactly
// one of the written lines.
func TestSyncWriter_ConcurrentLinesStayWhole(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSyncWriter(&buf)

	const perWriter = 200
	var wg sync.WaitGroup
	for _, tag := range []string{"alpha", "beta"} {
		wg.Add(1)
		go func(tag string) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				fmt.Fprintf(sw, "%s line %d\n", tag, i)
			}
		}(tag)
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2*perWriter {
		t.Fatalf("got %d lines, want %d", len(lines), 2*perWriter)
	}
	for _, l := range lines {
		var tag string
		var n int
		if _, err := fmt.Sscanf(l, "%s line %d", &tag, &n); err != nil || (tag != "alpha" && tag != "beta") {
			t.Fatalf("interleaved/corrupt line: %q", l)
		}
	}
}

// TestSyncWriter_StatusErasedAndRepaintedAroundWrite is the visible
// symptom from issue #67: with a spinner frame on screen, a plugin log
// line must get its own clean row — erase the frame, write the line,
// repaint the frame — instead of gluing onto the frame and being
// overwritten by the next redraw.
func TestSyncWriter_StatusErasedAndRepaintedAroundWrite(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSyncWriter(&buf)

	sw.SetStatus("⠋ mysql: starting on port 42001")
	n, err := io.WriteString(sw, "[WARN] plugin: something\n")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if want := len("[WARN] plugin: something\n"); n != want {
		t.Errorf("Write returned n=%d, want %d (must not count escape sequences)", n, want)
	}
	sw.ClearStatus()

	want := "\r\x1b[K⠋ mysql: starting on port 42001" + // SetStatus paint
		"\r\x1b[K[WARN] plugin: something\n" + // erase + log line
		"⠋ mysql: starting on port 42001" + // repaint after the newline
		"\r\x1b[K" // ClearStatus erase
	if got := buf.String(); got != want {
		t.Errorf("byte stream mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestSyncWriter_PartialLineSuspendsStatus guards the mid-line case: a
// write without a trailing newline must not get the status frame glued
// onto it, subsequent status ticks must not paint over the unfinished
// row (SetStatus records, ClearStatus only forgets), and the repaint
// resumes — with the newest frame — once a later write completes the
// line.
func TestSyncWriter_PartialLineSuspendsStatus(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSyncWriter(&buf)

	sw.SetStatus("F1")
	fmt.Fprint(sw, "partial") // erase F1, start a foreign line, suspend repaints
	sw.SetStatus("F2")        // tick mid-line: recorded, NOT painted
	fmt.Fprint(sw, "rest\n")  // completes the line → repaint the newest frame

	want := "\r\x1b[KF1" + // initial paint
		"\r\x1b[K" + "partial" + // erase + first chunk (no repaint)
		"rest\n" + "F2" // completion chunk appended untouched, then repaint
	if got := buf.String(); got != want {
		t.Errorf("byte stream mismatch:\n got %q\nwant %q", got, want)
	}

	// ClearStatus while a foreign line is mid-flight must not erase the
	// row — the text there is not the status frame.
	buf.Reset()
	sw.SetStatus("F3")
	fmt.Fprint(sw, "unfinished")
	sw.ClearStatus()
	want = "\r\x1b[KF3" + "\r\x1b[K" + "unfinished"
	if got := buf.String(); got != want {
		t.Errorf("ClearStatus mid-line mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestSyncWriter_NoStatusIsPassthrough: without a status line the
// wrapper must add nothing — the hook/CI log stays byte-identical.
func TestSyncWriter_NoStatusIsPassthrough(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSyncWriter(&buf)
	fmt.Fprintf(sw, "[bough] plain line\n")
	if got, want := buf.String(), "[bough] plain line\n"; got != want {
		t.Errorf("passthrough mismatch: got %q, want %q", got, want)
	}
}

// TestSyncWriter_ClearStatusIdempotent: ClearStatus on a cleared (or
// never-set) status writes nothing further.
func TestSyncWriter_ClearStatusIdempotent(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSyncWriter(&buf)
	sw.ClearStatus() // never set → no bytes
	if buf.Len() != 0 {
		t.Errorf("ClearStatus without status wrote %q", buf.String())
	}
	sw.SetStatus("x")
	sw.ClearStatus()
	before := buf.Len()
	sw.ClearStatus() // second clear → no additional bytes
	if buf.Len() != before {
		t.Errorf("second ClearStatus wrote extra bytes: %q", buf.String())
	}
}

// TestWrap_Identity pins Wrap's routing: os.Stderr maps onto the one
// shared singleton (mutex identity is the fix), an existing SyncWriter
// passes through unchanged, anything else gets a fresh wrapper. The
// singleton's Unwrap must surface the real *os.File so TTY detection
// and exec fd inheritance work through it.
func TestWrap_Identity(t *testing.T) {
	if Wrap(os.Stderr) != Stderr {
		t.Errorf("Wrap(os.Stderr) did not return the shared Stderr singleton")
	}
	if Stderr.Unwrap() != io.Writer(os.Stderr) {
		t.Errorf("Stderr.Unwrap() = %T, want the current os.Stderr", Stderr.Unwrap())
	}
	var buf bytes.Buffer
	sw := NewSyncWriter(&buf)
	if Wrap(sw) != sw {
		t.Errorf("Wrap(*SyncWriter) did not pass through")
	}
	fresh := Wrap(&buf)
	if fresh == Stderr {
		t.Errorf("Wrap(buffer) returned the Stderr singleton")
	}
	if fresh.Unwrap() != io.Writer(&buf) {
		t.Errorf("Wrap(buffer) does not wrap the buffer")
	}
}

// TestExecWriter pins the exec-child contract: a SyncWriter must never
// reach os/exec (a non-*os.File writer forces a pipe + copy goroutine
// whose EOF a backgrounded grandchild can hold open forever, hanging
// create) — ExecWriter hands the child the fd underneath instead.
func TestExecWriter(t *testing.T) {
	if got := ExecWriter(Stderr); got != io.Writer(os.Stderr) {
		t.Errorf("ExecWriter(Stderr) = %T, want os.Stderr", got)
	}
	var buf bytes.Buffer
	if got := ExecWriter(NewSyncWriter(&buf)); got != io.Writer(&buf) {
		t.Errorf("ExecWriter(SyncWriter(buf)) = %T, want the buffer", got)
	}
	if got := ExecWriter(&buf); got != io.Writer(&buf) {
		t.Errorf("ExecWriter(plain writer) = %T, want it unchanged", got)
	}
}

// TestStderrSingleton_FollowsStderrSwap: the singleton resolves
// os.Stderr per write instead of latching the fd at init, so the
// standard capture pattern (swap os.Stderr around code under test)
// keeps seeing plugin log lines.
func TestStderrSingleton_FollowsStderrSwap(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	if _, err := io.WriteString(Stderr, "captured line\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	os.Stderr = orig
	_ = w.Close()

	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "captured line\n" {
		t.Errorf("swap capture got %q, want %q", got, "captured line\n")
	}
}
