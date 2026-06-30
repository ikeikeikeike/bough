package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/homunculus"
	"github.com/ikeikeikeike/bough/internal/prompts"
)

func TestReadConfidence_Clamps(t *testing.T) {
	cases := []struct {
		in   any
		want float64
	}{
		{0.7, 0.7},
		{5.0, 1.0},  // hallucinated over-range → clamp to 1
		{-2.0, 0.0}, // negative → clamp to 0
		{1.0, 1.0},
		{0.0, 0.0},
		{"0.85", 0.85},
		{"3", 1.0}, // string over-range → clamp
	}
	for _, c := range cases {
		if got := readConfidence(c.in); got != c.want {
			t.Errorf("readConfidence(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCheckInstinctSafety(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // expected rule trigger; "" = pass
	}{
		{"clean prose", "## Action\nDo the thing.\n", ""},
		{"fenced code block", "## Action\nDo it.\n\n```go\nfmt.Println()\n```\n", "no-code-snippets"},
		{"api key marker", "## Action\nKeep the api key safe.\n", "no-secrets"},
		{"password marker", "## Action\nThe password rotates.\n", "no-secrets"},
		{"oversize", "## Action\n" + strings.Repeat("x", 5000), "max-action-length"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &homunculus.Instinct{ID: "x", Body: tc.body}
			rule, err := checkInstinctSafety(in)
			if tc.want == "" {
				if err != nil {
					t.Errorf("expected pass, got %s: %v", rule, err)
				}
				return
			}
			if rule != tc.want {
				t.Errorf("rule = %q, want %q (err=%v)", rule, tc.want, err)
			}
		})
	}
}

func TestWriteInstinctsFromResult_HappyPath(t *testing.T) {
	dir := t.TempDir()
	ident := homunculus.ProjectIdentity{ID: "abc123", Name: "demo", Root: "/x"}
	parsed := map[string]any{
		"instincts": []any{
			map[string]any{
				"id":         "read-before-edit",
				"trigger":    "when editing unfamiliar files",
				"confidence": 0.7,
				"domain":     "workflow",
				"scope":      "project",
				"action":     "Read the surrounding implementation before editing.",
				"evidence":   []any{"Observed 5 times in session sess-1"},
			},
		},
	}
	emitted, skipped, errs := writeInstinctsFromResult(dir, ident, parsed, time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC))
	if emitted != 1 {
		t.Errorf("emitted = %d, want 1", emitted)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0 (errs=%v)", skipped, errs)
	}
	// Round-trip read
	got, err := homunculus.ReadInstinctFile(dir + "/read-before-edit.md")
	if err != nil {
		t.Fatalf("ReadInstinctFile: %v", err)
	}
	if got.Domain != "workflow" || got.Confidence != 0.7 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestWriteInstinctsFromResult_SkipsUnsafe(t *testing.T) {
	dir := t.TempDir()
	ident := homunculus.ProjectIdentity{ID: "abc", Name: "demo"}
	parsed := map[string]any{
		"instincts": []any{
			map[string]any{
				"id":         "good-one",
				"trigger":    "when ok",
				"confidence": 0.7,
				"domain":     "workflow",
				"scope":      "project",
				"action":     "Do the thing.",
				"evidence":   []any{"observed thrice"},
			},
			map[string]any{
				"id":         "bad-secrets",
				"trigger":    "when bad",
				"confidence": 0.7,
				"domain":     "workflow",
				"scope":      "project",
				"action":     "Keep the password rotated.",
				"evidence":   []any{"observed"},
			},
		},
	}
	emitted, skipped, errs := writeInstinctsFromResult(dir, ident, parsed, time.Now())
	if emitted != 1 || skipped != 1 {
		t.Errorf("emitted=%d skipped=%d, want 1/1 (errs=%v)", emitted, skipped, errs)
	}
}

func TestWriteInstinctsFromResult_RejectsBadID(t *testing.T) {
	dir := t.TempDir()
	ident := homunculus.ProjectIdentity{ID: "abc"}
	parsed := map[string]any{
		"instincts": []any{
			map[string]any{
				"id":         "Bad ID",
				"trigger":    "when bad",
				"confidence": 0.5,
				"domain":     "workflow",
				"scope":      "project",
				"action":     "Stuff.",
				"evidence":   []any{"once"},
			},
		},
	}
	emitted, skipped, errs := writeInstinctsFromResult(dir, ident, parsed, time.Now())
	if emitted != 0 {
		t.Errorf("expected 0 emitted, got %d", emitted)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", skipped)
	}
	if len(errs) == 0 {
		t.Errorf("expected at least one soft error explaining the bad id")
	}
}

func TestRenderForPreview_RendersData(t *testing.T) {
	body := "Project={{.Name}} Session={{.SID}}"
	got, err := renderForPreview(body, map[string]string{"Name": "demo", "SID": "sess-1"})
	if err != nil {
		t.Fatalf("renderForPreview: %v", err)
	}
	if got != "Project=demo Session=sess-1" {
		t.Errorf("rendered = %q", got)
	}
}

func TestBuildObserverData_TruncatesExistingIDs(t *testing.T) {
	existing := make([]*homunculus.Instinct, observerDefaultExistingPreview+10)
	for i := range existing {
		existing[i] = &homunculus.Instinct{ID: "instinct-" + string(rune('a'+i%26))}
	}
	d := buildObserverData(
		homunculus.ProjectIdentity{ID: "abc", Name: "demo"},
		nil,
		existing,
		prompts.Template{},
	)
	// Existing block must not contain ALL instincts (= preview cap),
	// and must be non-empty.
	if d.ExistingIDs == "" {
		t.Errorf("ExistingIDs empty")
	}
	if strings.Count(d.ExistingIDs, ",")+1 > observerDefaultExistingPreview {
		t.Errorf("ExistingIDs exceeded preview cap: %d entries", strings.Count(d.ExistingIDs, ",")+1)
	}
}
