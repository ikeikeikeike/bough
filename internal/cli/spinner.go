package cli

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ikeikeikeike/bough/internal/termio"
)

// spinnerFrames is the braille progress cycle painted during a long
// silent step.
var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

const spinnerInterval = 100 * time.Millisecond

// spinner renders a single-line "<frame> <msg>" progress indicator to
// an interactive terminal so a long silent step (engine boot, git
// clone) visibly shows bough is alive rather than hung. When the writer
// is not a TTY — the WorktreeCreate hook pipes stderr, CI captures it, a
// shell redirect — the spinner is INERT: no goroutine, no bytes written,
// so the plain [bough] lines remain the entire, greppable log. That
// inertness is the contract the hook path relies on.
//
// Every frame is registered as the SyncWriter's status line, so
// concurrent writers sharing the same wrapper — pluginhost's hclog
// forwarding go-plugin subprocess lines — erase and repaint it around
// their own lines instead of garbling the row (issue #67).
type spinner struct {
	w        *termio.SyncWriter
	tty      bool
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// startSpinner begins animating msg on w and returns a handle whose
// Stop() halts the animation and clears the line. Stop() is safe to call
// on a non-TTY (inert) spinner. w is normalized through termio.Wrap so
// there is exactly one paint path; a raw os.Stderr maps onto the shared
// termio.Stderr singleton the plugin logger also writes through.
func startSpinner(w io.Writer, msg string) *spinner {
	sw := termio.Wrap(w)
	s := &spinner{w: sw, tty: isInteractive(sw)}
	if !s.tty {
		return s
	}
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.run(msg)
	return s
}

func (s *spinner) run(msg string) {
	defer close(s.done)
	t := time.NewTicker(spinnerInterval)
	defer t.Stop()
	// Paint frame 0 immediately so there is no interval-long blank before
	// the first tick.
	s.w.SetStatus(fmt.Sprintf("%c %s", spinnerFrames[0], msg))
	i := 0
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			i = (i + 1) % len(spinnerFrames)
			s.w.SetStatus(fmt.Sprintf("%c %s", spinnerFrames[i], msg))
		}
	}
}

// Stop halts the animation and erases the spinner line (deregistering
// the status frame so later log lines stop repainting it) — the
// caller's next [bough] line starts on a clean row. No-op for an inert
// (non-TTY) spinner.
func (s *spinner) Stop() {
	if !s.tty {
		return
	}
	// sync.Once so a second Stop() (a future extra call site, or an
	// error path that also defers Stop) cannot panic on a double
	// `close(s.stop)` and crash `bough create`.
	s.stopOnce.Do(func() {
		close(s.stop)
		<-s.done
		s.w.ClearStatus()
	})
}

// isInteractive reports whether w is a terminal bough may animate on. A
// The SyncWriter-unwrap + *os.File + isatty logic lives in termio.IsTTY now
// that the doctor's colouriser needs the same check; isInteractive is kept as
// the spinner's name for it so this file's call sites read naturally.
func isInteractive(w io.Writer) bool {
	return termio.IsTTY(w)
}
