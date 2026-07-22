package termio

import (
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// Status is a health signal for one doctor line or one section rollup.
// The glyphs and the two-colour scheme are the whole point of the doctor
// redesign: an operator should see at a glance which block needs them.
type Status int

const (
	// StatusOK — fine, nothing to do. Green.
	StatusOK Status = iota
	// StatusWarn — works, but the operator should look (a caveat, a
	// possible-elsewhere conflict). Red, because with two colours "look
	// here" and "broken" share the attention hue.
	StatusWarn
	// StatusError — broken, action required. Red.
	StatusError
	// StatusNeutral — informational, not a health signal (a path, a not-yet
	// -implemented meter). No colour, so it never competes for attention.
	StatusNeutral
)

// Glyph is the inline marker: the thing that sits before a detail line.
func (s Status) Glyph() string {
	switch s {
	case StatusOK:
		return "✓"
	case StatusWarn:
		return "!"
	case StatusError:
		return "✗"
	default:
		return "•"
	}
}

// Bracket is the section-header marker, flutter-doctor style: [✓] / [!] /
// [✗] / [·]. A section takes the most severe status of its lines (see Worst),
// so the operator can triage by the brackets alone.
func (s Status) Bracket() string {
	switch s {
	case StatusOK:
		return "[✓]"
	case StatusWarn:
		return "[!]"
	case StatusError:
		return "[✗]"
	default:
		return "[·]"
	}
}

// severity orders statuses for Worst. Neutral is deliberately the FLOOR, not
// a middle value: a section of purely informational lines rolls up to [·],
// while a single OK line lifts it to [✓]. Error outranks Warn outranks OK.
func (s Status) severity() int {
	switch s {
	case StatusError:
		return 3
	case StatusWarn:
		return 2
	case StatusOK:
		return 1
	default:
		return 0
	}
}

// Worst folds a section's line statuses into its header status. Empty folds
// to Neutral (nothing to report is not a health claim).
func Worst(statuses ...Status) Status {
	worst := StatusNeutral
	for _, s := range statuses {
		if s.severity() > worst.severity() {
			worst = s
		}
	}
	return worst
}

// IsTTY reports whether w is a terminal bough can safely paint. A SyncWriter
// wrapper is unwrapped first (bough's spinner path); anything that is not an
// *os.File — a bytes.Buffer in a test, a pipe, a redirect — is not a TTY.
func IsTTY(w io.Writer) bool {
	if sw, ok := w.(*SyncWriter); ok {
		w = sw.Unwrap()
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// ANSI SGR codes. Only two hues plus reset — the redesign is deliberately
// two-colour so it degrades to "just the glyphs" cleanly.
const (
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiReset = "\x1b[0m"
)

// Styler paints status markers, or doesn't. It never colours message text —
// only the marker glyph — so a token a test or a grep looks for is never
// split by an escape sequence.
type Styler struct {
	color bool
}

// NewStyler decides once, from the writer and the environment, whether this
// run gets colour. Colour is on only when ALL of these hold, so a pipe, a
// CI log, a NO_COLOR user, or a dumb terminal all get plain glyphs:
//
//   - w is a real terminal (IsTTY)
//   - NO_COLOR is unset (the https://no-color.org convention)
//   - TERM is not "dumb"
//
// That is the "only colour where it renders reliably; otherwise leave it
// white" contract — the detection is a library (go-isatty), not a guess.
func NewStyler(w io.Writer) Styler {
	if !IsTTY(w) {
		return Styler{}
	}
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return Styler{}
	}
	if os.Getenv("TERM") == "dumb" {
		return Styler{}
	}
	return Styler{color: true}
}

// paint wraps s in an SGR code, or returns it untouched when colour is off.
func (st Styler) paint(code, s string) string {
	if !st.color {
		return s
	}
	return code + s + ansiReset
}

// hue maps a status to its colour under the two-colour scheme: OK green,
// Warn/Error red (both "your attention"), Neutral uncoloured.
func (st Styler) hue(s Status, glyph string) string {
	switch s {
	case StatusOK:
		return st.paint(ansiGreen, glyph)
	case StatusWarn, StatusError:
		return st.paint(ansiRed, glyph)
	default:
		return glyph
	}
}

// Mark is the coloured inline glyph for a detail line.
func (st Styler) Mark(s Status) string { return st.hue(s, s.Glyph()) }

// Section is the coloured [x] header marker for a section rollup.
func (st Styler) Section(s Status) string { return st.hue(s, s.Bracket()) }
