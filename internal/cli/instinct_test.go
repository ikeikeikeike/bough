package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/homunculus"
)

func mkInstinct(id, domain string, conf float64, last time.Time) *homunculus.Instinct {
	return &homunculus.Instinct{
		ID:         id,
		Trigger:    "when " + id,
		Confidence: conf,
		Domain:     domain,
		Scope:      "project",
		LastSeen:   last,
	}
}

func TestFilterInstincts(t *testing.T) {
	now := time.Now()
	all := []*homunculus.Instinct{
		mkInstinct("a", "workflow", 0.8, now),
		mkInstinct("b", "testing", 0.5, now),
		mkInstinct("c", "workflow", 0.4, now),
	}
	got := filterInstincts(all, "workflow", 0.6)
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("filterInstincts result = %+v, want only ID a", got)
	}
}

func TestSortInstincts(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want []string // expected order of IDs
	}{
		{"by id", "id", []string{"a", "b", "c"}},
		{"by confidence desc", "confidence", []string{"a", "b", "c"}},
		{"by recent desc", "recent", []string{"c", "b", "a"}},
	}
	base := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := []*homunculus.Instinct{
				mkInstinct("c", "workflow", 0.5, base.Add(2*time.Hour)),
				mkInstinct("a", "workflow", 0.9, base),
				mkInstinct("b", "workflow", 0.7, base.Add(time.Hour)),
			}
			sortInstincts(rows, tc.key)
			ids := []string{rows[0].ID, rows[1].ID, rows[2].ID}
			if ids[0] != tc.want[0] || ids[1] != tc.want[1] || ids[2] != tc.want[2] {
				t.Errorf("order = %v, want %v", ids, tc.want)
			}
		})
	}
}

func TestRenderStatus_HistogramAndCount(t *testing.T) {
	var buf bytes.Buffer
	ident := homunculus.ProjectIdentity{ID: "abc123", Name: "demo", Root: "/x", Remote: "https://github.com/x/demo.git"}
	layout := homunculus.FromRoot("/tmp/test-homunculus")
	now := time.Now()
	rows := []*homunculus.Instinct{
		mkInstinct("a", "workflow", 0.85, now),
		mkInstinct("b", "workflow", 0.70, now),
		mkInstinct("c", "testing", 0.50, now),
		mkInstinct("d", "git", 0.30, now),
	}
	renderStatus(&buf, ident, layout, rows)
	out := buf.String()
	for _, want := range []string{
		"project: demo (abc123)",
		"count:   4",
		"confidence histogram:",
		"<0.40",
		"most recent:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderList_HeaderAndRows(t *testing.T) {
	var buf bytes.Buffer
	rows := []*homunculus.Instinct{
		mkInstinct("io-lives-in-data-layer", "workflow", 0.85, time.Now()),
	}
	renderList(&buf, rows)
	out := buf.String()
	if !strings.Contains(out, "CONF") || !strings.Contains(out, "DOMAIN") {
		t.Errorf("list header missing: %s", out)
	}
	if !strings.Contains(out, "io-lives-in-data-layer") {
		t.Errorf("row missing: %s", out)
	}
}

func TestRenderList_EmptyHint(t *testing.T) {
	var buf bytes.Buffer
	renderList(&buf, nil)
	if !strings.Contains(buf.String(), "no instincts matched") {
		t.Errorf("empty hint missing: %s", buf.String())
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdefghij", 6); got != "abcde…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Errorf("short truncate = %q", got)
	}
}

func TestLastSeen(t *testing.T) {
	now := time.Now()
	cases := []struct {
		dur  time.Duration
		want string
	}{
		{0, "just now"},
		{2 * time.Minute, "2 min ago"},
		{3 * time.Hour, "3 hr ago"},
		{50 * time.Hour, "2 days ago"},
	}
	for _, tc := range cases {
		in := &homunculus.Instinct{LastSeen: now.Add(-tc.dur)}
		got := lastSeen(in)
		if !strings.Contains(got, tc.want) && got != tc.want {
			t.Errorf("lastSeen(%s) = %q, want substring %q", tc.dur, got, tc.want)
		}
	}
}
