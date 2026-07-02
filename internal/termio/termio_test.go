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

// TestSyncWriter_PartialLineDoesNotRepaint guards the glue case: a
// write without a trailing newline must not get the status frame
// appended to it (the next status tick repaints instead).
func TestSyncWriter_PartialLineDoesNotRepaint(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSyncWriter(&buf)

	sw.SetStatus("⠋ spin")
	fmt.Fprint(sw, "partial")

	want := "\r\x1b[K⠋ spin" + "\r\x1b[K" + "partial"
	if got := buf.String(); got != want {
		t.Errorf("byte stream mismatch:\n got %q\nwant %q", got, want)
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
// passes through unchanged, anything else gets a fresh wrapper.
func TestWrap_Identity(t *testing.T) {
	if Wrap(os.Stderr) != Stderr {
		t.Errorf("Wrap(os.Stderr) did not return the shared Stderr singleton")
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
