package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/ikeikeikeike/bough/internal/config"
	"github.com/ikeikeikeike/bough/internal/homunculus"
)

func TestCollectClaudemdProposals(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -40) // older than the 30-day remove gate
	recent := now.AddDate(0, 0, -5)

	ins := []*homunculus.Instinct{
		{ID: "top-band", Confidence: 0.85},                          // ADD
		{ID: "add-floor", Confidence: 0.80},                         // ADD (gate is >=0.80)
		{ID: "middling", Confidence: 0.70},                          // neither
		{ID: "decayed-old", Confidence: 0.30, FirstSeen: old},       // REMOVE
		{ID: "decayed-recent", Confidence: 0.30, FirstSeen: recent}, // neither (not aged)
		{ID: "decayed-undated", Confidence: 0.30},                   // neither (no first_seen)
	}
	p := collectClaudemdProposals(ins, now)

	gotAdd := ids(p.add)
	wantAdd := []string{"top-band", "add-floor"} // sorted by confidence desc
	if strings.Join(gotAdd, ",") != strings.Join(wantAdd, ",") {
		t.Errorf("add = %v, want %v", gotAdd, wantAdd)
	}
	gotRemove := ids(p.remove)
	if strings.Join(gotRemove, ",") != "decayed-old" {
		t.Errorf("remove = %v, want [decayed-old]", gotRemove)
	}
}

func TestFirstActionLine(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{"## Action\nRun the migration after schema changes\n", "Run the migration after schema changes"},
		{"# Title\n\nJust prose here\n", "Just prose here"},
		{"## Action\n\n  Indented action line  ", "Indented action line"},
		{"", ""},
	}
	for _, c := range cases {
		if got := firstActionLine(c.body); got != c.want {
			t.Errorf("firstActionLine(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}

func TestRenderClaudemdProposals(t *testing.T) {
	now := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	p := claudemdProposals{
		add:    []*homunculus.Instinct{{ID: "a1", Confidence: 0.85, Domain: "workflow", Trigger: "when X", Body: "## Action\ndo X"}},
		remove: []*homunculus.Instinct{{ID: "r1", Confidence: 0.30, FirstSeen: now.AddDate(0, 0, -40)}},
	}
	doc := renderClaudemdProposals(p, now)
	for _, want := range []string{
		"# CLAUDE.md Evolution Proposals",
		"## Proposed Additions",
		"### ADD — a1",
		"- rule: do X",
		"## Proposed Removals",
		"### REMOVE — r1",
		"bough never edits CLAUDE.md automatically",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("rendered doc missing %q\n---\n%s", want, doc)
		}
	}
}

// TestRunEvolveClaudeMD_PreviewAndWrite exercises the command body end to
// end against a temp homunculus: preview prints to stdout without writing,
// --write saves the proposals file under <root>/.claude/.
func TestRunEvolveClaudeMD_PreviewAndWrite(t *testing.T) {
	t.Setenv("BOUGH_HOMUNCULUS_DIR", t.TempDir())
	repo := t.TempDir()
	now := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)

	ident, err := homunculus.DetectIdentity(repo)
	if err != nil {
		t.Fatalf("DetectIdentity: %v", err)
	}
	layout := homunculus.NewLayout()
	writeProjInstinct(t, layout, ident.ID, "high-conf-rule", 0.85)

	proposalsPath := filepath.Join(repo, ".claude", claudemdProposalsRelName)

	// preview: prints, writes nothing
	var preview bytes.Buffer
	if err := runEvolveClaudeMD(&preview, repo, "", false, now); err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !strings.Contains(preview.String(), "### ADD — high-conf-rule") {
		t.Errorf("preview missing ADD proposal:\n%s", preview.String())
	}
	if _, err := os.Stat(proposalsPath); !os.IsNotExist(err) {
		t.Errorf("preview wrote a file (stat err=%v)", err)
	}

	// --write: saves the proposals file
	var written bytes.Buffer
	if err := runEvolveClaudeMD(&written, repo, "", true, now); err != nil {
		t.Fatalf("write: %v", err)
	}
	body, err := os.ReadFile(proposalsPath)
	if err != nil {
		t.Fatalf("proposals file not written: %v", err)
	}
	if !strings.Contains(string(body), "### ADD — high-conf-rule") {
		t.Errorf("proposals file missing ADD:\n%s", body)
	}
}

// TestDispatchEvolveClaudeMD_OptIn is the v0.9.14 gate: the SessionEnd
// dispatch writes .claude/claudemd-proposals.md ONLY when the monorepo's
// .bough.yaml opts in via instinct.evolve_claudemd_on_session_end. Off
// (the default) must leave the repo working tree untouched.
func TestDispatchEvolveClaudeMD_OptIn(t *testing.T) {
	const baseCfg = `schema_version: 1
monorepo_root: "."
repositories:
  - name: demo-api
    branch_strategy: develop
registry:
  path: ".worktree-ports.json"
instinct:
  enabled: true
`
	run := func(t *testing.T, flagOn bool) string {
		t.Helper()
		t.Setenv("BOUGH_HOMUNCULUS_DIR", t.TempDir())
		repo := t.TempDir()
		cfg := baseCfg
		if flagOn {
			cfg += "  evolve_claudemd_on_session_end: true\n"
		}
		if err := os.WriteFile(filepath.Join(repo, ".bough.yaml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		// precondition: the fixture must be a valid config, else the gate
		// test would pass for the wrong reason (early return on load error).
		if _, err := config.Load(filepath.Join(repo, ".bough.yaml")); err != nil {
			t.Fatalf("test fixture .bough.yaml is invalid: %v", err)
		}
		ident, err := homunculus.DetectIdentity(repo)
		if err != nil {
			t.Fatal(err)
		}
		writeProjInstinct(t, homunculus.NewLayout(), ident.ID, "top-rule", 0.85)
		t.Chdir(repo)
		cmd := &cobra.Command{}
		cmd.SetOut(io.Discard)
		dispatchEvolveClaudeMD(cmd)
		return repo
	}

	t.Run("opt-in on writes proposals", func(t *testing.T) {
		repo := run(t, true)
		if _, err := os.Stat(filepath.Join(repo, ".claude", "claudemd-proposals.md")); err != nil {
			t.Errorf("flag on: proposals file not written: %v", err)
		}
	})
	t.Run("opt-in off writes nothing", func(t *testing.T) {
		repo := run(t, false)
		if _, err := os.Stat(filepath.Join(repo, ".claude", "claudemd-proposals.md")); !os.IsNotExist(err) {
			t.Errorf("flag off: a file was written into the repo (stat err=%v)", err)
		}
	})
}

func ids(in []*homunculus.Instinct) []string {
	out := make([]string, len(in))
	for i, x := range in {
		out[i] = x.ID
	}
	return out
}
