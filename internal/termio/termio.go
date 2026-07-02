// Package termio serializes concurrent writers onto one terminal
// stream. `bough create` paints a spinner frame from one goroutine
// while pluginhost's hclog logger forwards go-plugin subprocess lines
// from others; both target the same stderr fd, and unsynchronized
// writes garble the TTY (issue #67). SyncWriter is the shared choke
// point: ordinary writes are serialized whole, and a registered
// status line (the spinner frame, painted without a trailing newline)
// is erased before and repainted after each foreign write, so a
// plugin Warn line lands on its own clean row instead of gluing onto
// the spinner frame and being half-overwritten by the next redraw.
package termio

import (
	"bytes"
	"io"
	"os"
	"sync"
)

// eraseLine is carriage return + clear-to-end-of-line: every erase and
// repaint in this package (and nowhere else) spells it through this
// constant so a future change (\x1b[2K for wide-rune residue, cursor
// save/restore) cannot drift between call sites.
const eraseLine = "\r\x1b[K"

// stderrNow resolves os.Stderr at every Write instead of latching the
// *os.File at package init, so the standard capture pattern — swap
// os.Stderr for a pipe around the code under test — keeps working for
// everything routed through the Stderr singleton.
type stderrNow struct{}

func (stderrNow) Write(p []byte) (int, error) { return os.Stderr.Write(p) }

// Stderr is the process-wide synchronized wrapper for os.Stderr.
// Every bough writer that targets stderr must share this one instance
// — one mutex per fd is the whole point; two SyncWriters over the
// same fd would serialize nothing between each other.
var Stderr = NewSyncWriter(stderrNow{})

// SyncWriter serializes whole Write calls onto the underlying writer
// and coordinates them with an optional single-line status frame.
type SyncWriter struct {
	mu     sync.Mutex
	w      io.Writer
	status string // active status frame ("" = none); painted with no trailing newline
	// partial means the last foreign write ended mid-line: the terminal
	// row holds that unfinished text, so status repaints are suspended
	// (SetStatus records the frame without painting, ClearStatus does
	// not erase) until a newline-terminated write completes the row.
	// Without this, the next spinner tick would wipe the partial chunk.
	partial bool
}

// NewSyncWriter wraps w. Use the package-level Stderr for os.Stderr
// rather than constructing a second wrapper over the same fd.
func NewSyncWriter(w io.Writer) *SyncWriter {
	return &SyncWriter{w: w}
}

// Wrap returns the writer to use in place of w: the shared Stderr
// singleton when w is the real os.Stderr, w itself when it is already
// a *SyncWriter, and a fresh SyncWriter otherwise (test buffers,
// pipes). Callers wrap once at their entry point and pass the result
// down.
func Wrap(w io.Writer) *SyncWriter {
	if sw, ok := w.(*SyncWriter); ok {
		return sw
	}
	if w == os.Stderr {
		return Stderr
	}
	return NewSyncWriter(w)
}

// ExecWriter returns the writer an exec.Cmd child should inherit in
// place of w: the fd underneath when w is a *SyncWriter, w unchanged
// otherwise. Handing the SyncWriter itself to os/exec would make it
// substitute a pipe plus copy goroutine for the non-*os.File writer —
// Cmd.Wait then blocks until pipe EOF, which never comes while any
// backgrounded grandchild keeps the write end open (a post_create
// `devserver &` would hang create forever), and children lose
// TTY-ness (isatty false, block buffering, no color). Hook output
// bypassing the status mutex is fine: no spinner is active while
// hooks run.
func ExecWriter(w io.Writer) io.Writer {
	if sw, ok := w.(*SyncWriter); ok {
		return sw.Unwrap()
	}
	return w
}

// Write forwards p under the mutex. When a status frame is on screen
// it is erased first and repainted after, assembled with p into a
// single underlying Write so no other fd writer can land between the
// erase and the repaint. The returned n counts only p's bytes, never
// the escape sequences, keeping the io.Writer contract for wrappers
// like hclog. A p that does not end in a newline leaves the row
// mid-line: the repaint is skipped and status painting stays
// suspended (see the partial field) until a later write completes the
// line.
func (s *SyncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	endsLine := bytes.HasSuffix(p, []byte("\n"))
	if s.status == "" && !s.partial {
		n, err := s.w.Write(p)
		s.partial = !endsLine
		return n, err
	}

	prefix := ""
	if !s.partial && s.status != "" {
		prefix = eraseLine // clear the painted frame before the foreign line
	}
	suffix := ""
	if endsLine && s.status != "" {
		suffix = s.status // repaint after a completed line
	}
	buf := make([]byte, 0, len(prefix)+len(p)+len(suffix))
	buf = append(buf, prefix...)
	buf = append(buf, p...)
	buf = append(buf, suffix...)

	n, err := s.w.Write(buf)
	s.partial = !endsLine

	// Map the underlying count back onto p for the io.Writer contract.
	consumed := n - len(prefix)
	if consumed < 0 {
		consumed = 0
	}
	if consumed > len(p) {
		consumed = len(p)
	}
	if err == nil {
		return len(p), nil
	}
	return consumed, err
}

// SetStatus records frame as the persistent status line and paints it
// (eraseLine + frame, no trailing newline). While a foreign partial
// line occupies the row the paint is deferred — the frame is only
// recorded — and resumes when that line completes. Painting is
// best-effort, like the spinner's Fprintf it replaces.
func (s *SyncWriter) SetStatus(frame string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = frame
	if s.partial {
		return
	}
	_, _ = io.WriteString(s.w, eraseLine+frame)
}

// ClearStatus erases the status line and stops repainting it. No-op
// when no status is active; when a foreign partial line occupies the
// row only the bookkeeping is cleared (the row's text is not ours to
// erase).
func (s *SyncWriter) ClearStatus() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == "" {
		return
	}
	s.status = ""
	if s.partial {
		return
	}
	_, _ = io.WriteString(s.w, eraseLine)
}

// Unwrap exposes the underlying writer so TTY detection and exec fd
// inheritance can reach the real *os.File through the wrapper. The
// Stderr singleton reports the current os.Stderr, honoring runtime
// swaps.
func (s *SyncWriter) Unwrap() io.Writer {
	if _, ok := s.w.(stderrNow); ok {
		return os.Stderr
	}
	return s.w
}
