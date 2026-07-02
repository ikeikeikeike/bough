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

// Stderr is the process-wide synchronized wrapper for os.Stderr.
// Every bough writer that targets stderr must share this one instance
// — one mutex per fd is the whole point; two SyncWriters over the
// same fd would serialize nothing between each other.
var Stderr = NewSyncWriter(os.Stderr)

// SyncWriter serializes whole Write calls onto the underlying writer
// and coordinates them with an optional single-line status frame.
type SyncWriter struct {
	mu     sync.Mutex
	w      io.Writer
	status string // active status frame ("" = none); painted with no trailing newline
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

// Write forwards p under the mutex. When a status frame is on screen
// it is erased first and repainted after, so p — a log line — gets its
// own row. The returned n counts only p's bytes, never the repaint
// escape sequences, keeping the io.Writer contract for wrappers like
// hclog. The repaint is skipped when p does not end in a newline: a
// partial line must not have the frame glued onto it (the next status
// tick repaints within one interval anyway).
func (s *SyncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == "" {
		return s.w.Write(p)
	}
	if _, err := io.WriteString(s.w, "\r\x1b[K"); err != nil {
		return 0, err
	}
	n, err := s.w.Write(p)
	if err != nil {
		return n, err
	}
	if bytes.HasSuffix(p, []byte("\n")) {
		_, err = io.WriteString(s.w, s.status)
	}
	return n, err
}

// SetStatus paints frame as the persistent status line (carriage
// return + clear-to-end-of-line + frame, no trailing newline) and
// remembers it so foreign writes can erase and repaint it. Painting is
// best-effort, like the spinner's Fprintf it replaces.
func (s *SyncWriter) SetStatus(frame string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = frame
	_, _ = io.WriteString(s.w, "\r\x1b[K"+frame)
}

// ClearStatus erases the status line and stops repainting it. No-op
// when no status is active, so it is safe on every exit path.
func (s *SyncWriter) ClearStatus() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == "" {
		return
	}
	s.status = ""
	_, _ = io.WriteString(s.w, "\r\x1b[K")
}

// Unwrap exposes the underlying writer so TTY detection can reach the
// real *os.File through the wrapper.
func (s *SyncWriter) Unwrap() io.Writer {
	return s.w
}
