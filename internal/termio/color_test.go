package termio

import (
	"bytes"
	"strings"
	"testing"
)

// TestStatusGlyphAndBracket pins the marker vocabulary so a future edit that
// swaps a glyph has to say so on purpose — the glyphs are the redesign.
func TestStatusGlyphAndBracket(t *testing.T) {
	cases := []struct {
		s       Status
		glyph   string
		bracket string
	}{
		{StatusOK, "✓", "[✓]"},
		{StatusWarn, "!", "[!]"},
		{StatusError, "✗", "[✗]"},
		{StatusNeutral, "•", "[·]"},
	}
	for _, c := range cases {
		if got := c.s.Glyph(); got != c.glyph {
			t.Errorf("Glyph(%v) = %q, want %q", c.s, got, c.glyph)
		}
		if got := c.s.Bracket(); got != c.bracket {
			t.Errorf("Bracket(%v) = %q, want %q", c.s, got, c.bracket)
		}
	}
}

// TestWorst is the section-rollup rule: Error > Warn > OK > Neutral, and an
// empty section is Neutral (nothing to report is not a health claim).
func TestWorst(t *testing.T) {
	cases := []struct {
		in   []Status
		want Status
	}{
		{nil, StatusNeutral},
		{[]Status{StatusNeutral, StatusNeutral}, StatusNeutral},
		{[]Status{StatusNeutral, StatusOK}, StatusOK},
		{[]Status{StatusOK, StatusWarn}, StatusWarn},
		{[]Status{StatusWarn, StatusError, StatusOK}, StatusError},
		{[]Status{StatusOK, StatusOK}, StatusOK},
	}
	for _, c := range cases {
		if got := Worst(c.in...); got != c.want {
			t.Errorf("Worst(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestStylerPlainOnNonTTY is the whole "leave it white unless we can render
// colour reliably" contract: a bytes.Buffer is not a TTY, so the marker comes
// back as the bare glyph with no escape codes.
func TestStylerPlainOnNonTTY(t *testing.T) {
	st := NewStyler(&bytes.Buffer{})
	for _, s := range []Status{StatusOK, StatusWarn, StatusError, StatusNeutral} {
		if got := st.Mark(s); got != s.Glyph() {
			t.Errorf("non-TTY Mark(%v) = %q, want bare glyph %q", s, got, s.Glyph())
		}
		if strings.Contains(st.Section(s), "\x1b[") {
			t.Errorf("non-TTY Section(%v) leaked an escape code: %q", s, st.Section(s))
		}
	}
}

// TestStylerColorScheme pins the two-colour mapping directly on a
// colour-forced styler (the TTY path is environment-dependent and covered by
// the plain-path test above): OK green, Warn/Error red, Neutral uncoloured.
func TestStylerColorScheme(t *testing.T) {
	st := Styler{color: true}
	cases := []struct {
		s        Status
		wantCode string // "" = expect no colour
	}{
		{StatusOK, ansiGreen},
		{StatusWarn, ansiRed},
		{StatusError, ansiRed},
		{StatusNeutral, ""},
	}
	for _, c := range cases {
		got := st.Mark(c.s)
		if c.wantCode == "" {
			if strings.Contains(got, "\x1b[") {
				t.Errorf("Mark(%v) should be uncoloured, got %q", c.s, got)
			}
			continue
		}
		if !strings.HasPrefix(got, c.wantCode) || !strings.HasSuffix(got, ansiReset) {
			t.Errorf("Mark(%v) = %q, want %s…%s", c.s, got, c.wantCode, ansiReset)
		}
	}
}

// TestStylerNeverColoursMessageText is the safety property the tests and greps
// depend on: only the marker glyph is painted, never the message, so a token
// like "WARNING" is never split by an escape sequence. A colour-forced styler
// applied to a marker leaves any following text the caller concatenates clean.
func TestStylerNeverColoursMessageText(t *testing.T) {
	st := Styler{color: true}
	line := st.Mark(StatusError) + " WARNING: something"
	if !strings.Contains(line, "WARNING: something") {
		t.Errorf("message text was mangled by colouring: %q", line)
	}
}
