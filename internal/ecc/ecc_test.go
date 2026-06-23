package ecc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/pkg/schema"
)

const instinctFixture = `---
id: bash-command-description-discipline
trigger: when invoking the Bash tool
confidence: 0.80
domain: workflow
source: session-observation
scope: project
project_id: abc123
project_name: claude
---

# Bash Command Description Discipline

## Action
Always include a clear description parameter.

## Evidence
- 15+ instances observed.

## Rationale
Descriptions provide context.
`

const skillFixture = `---
name: background-process-detachment
description: Apply when launching long-running background services
evolved_from:
  - bash-subshell-process-detach
  - nohup-disown-background-launching
---

# background-process-detachment

Evolved from 2 instincts.

## Actions

- Use subshell with trap.
`

const agentFixture = `---
model: sonnet
tools: Read, Grep, Glob
---
# shell-diagnostic-batching

Evolved from 24 instincts.
`

const commandFixture = `# creating-github-issues

Evolved from instinct: github-issue-structure
Confidence: 70%

## Action
Structure all issues.
`

func TestParseInstinct(t *testing.T) {
	in, err := ParseInstinct(instinctFixture, "/tmp/fixture.md")
	if err != nil {
		t.Fatalf("ParseInstinct: %v", err)
	}
	if in.ID != "bash-command-description-discipline" {
		t.Errorf("ID = %q", in.ID)
	}
	if in.Confidence != 0.80 {
		t.Errorf("Confidence = %f, want 0.80", in.Confidence)
	}
	if in.Scope != "project" {
		t.Errorf("Scope = %q", in.Scope)
	}
	if !strings.Contains(in.BodyAction, "clear description") {
		t.Errorf("BodyAction missing expected text: %q", in.BodyAction)
	}
	if in.BodyTitle != "Bash Command Description Discipline" {
		t.Errorf("BodyTitle = %q", in.BodyTitle)
	}
}

func TestParseSkill(t *testing.T) {
	s, err := ParseSkill(skillFixture, "/tmp/s.md")
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	if s.Name != "background-process-detachment" {
		t.Errorf("Name = %q", s.Name)
	}
	if len(s.EvolvedFrom) != 2 {
		t.Errorf("EvolvedFrom = %v", s.EvolvedFrom)
	}
}

func TestParseAgent(t *testing.T) {
	a, err := ParseAgent(agentFixture, "/tmp/a.md")
	if err != nil {
		t.Fatalf("ParseAgent: %v", err)
	}
	if a.Model != "sonnet" {
		t.Errorf("Model = %q", a.Model)
	}
	if len(a.Tools) != 3 {
		t.Errorf("Tools = %v", a.Tools)
	}
	if a.Name != "shell-diagnostic-batching" {
		t.Errorf("Name = %q", a.Name)
	}
}

func TestParseCommand(t *testing.T) {
	c, err := ParseCommand(commandFixture, "/tmp/c.md")
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if c.Name != "creating-github-issues" {
		t.Errorf("Name = %q", c.Name)
	}
	if c.EvolvedFromInstinct != "github-issue-structure" {
		t.Errorf("EvolvedFromInstinct = %q", c.EvolvedFromInstinct)
	}
	if c.Confidence != 0.70 {
		t.Errorf("Confidence = %f", c.Confidence)
	}
}

func TestDiscover_EndToEnd(t *testing.T) {
	root := t.TempDir()
	// Project layout
	mustMkdir(t, filepath.Join(root, "projects/abc123/instincts/personal"))
	mustMkdir(t, filepath.Join(root, "projects/abc123/evolved/skills"))
	mustMkdir(t, filepath.Join(root, "projects/abc123/evolved/agents"))
	mustMkdir(t, filepath.Join(root, "projects/abc123/evolved/commands"))
	mustWriteFile(t, filepath.Join(root, "projects/abc123/instincts/personal/x.md"), instinctFixture)
	mustWriteFile(t, filepath.Join(root, "projects/abc123/evolved/skills/y.md"), skillFixture)
	mustWriteFile(t, filepath.Join(root, "projects/abc123/evolved/agents/z.md"), agentFixture)
	mustWriteFile(t, filepath.Join(root, "projects/abc123/evolved/commands/w.md"), commandFixture)
	mustWriteFile(t, filepath.Join(root, "projects.json"), `{"abc123":{"id":"abc123","name":"claude","root":"/x"}}`)

	corp, err := Discover(root, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(corp.Instincts) != 1 {
		t.Errorf("Instincts = %d, want 1", len(corp.Instincts))
	}
	if len(corp.Skills) != 1 {
		t.Errorf("Skills = %d, want 1", len(corp.Skills))
	}
	if len(corp.Agents) != 1 {
		t.Errorf("Agents = %d, want 1", len(corp.Agents))
	}
	if len(corp.Commands) != 1 {
		t.Errorf("Commands = %d, want 1", len(corp.Commands))
	}
	if _, ok := corp.Projects["abc123"]; !ok {
		t.Errorf("Projects map missing abc123: %+v", corp.Projects)
	}
}

func TestMigrate_InstinctRoundTrip(t *testing.T) {
	corp := &Corpus{
		Instincts: []Instinct{
			{
				ID:            "x",
				Trigger:       "when foo",
				Confidence:    0.8,
				Scope:         "project",
				ProjectName:   "claude",
				BodyAction:    "Do foo.",
				BodyRationale: "Because bar.",
				SourcePath:    "/tmp/x.md",
				Source:        "session-observation",
			},
		},
	}
	res := Migrate(corp, MigrateOptions{
		NowFn:        func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) },
		MonorepoName: "bough",
	})
	if len(res.InstinctCandidates) != 1 {
		t.Fatalf("InstinctCandidates = %d, want 1", len(res.InstinctCandidates))
	}
	cand := res.InstinctCandidates[0]
	if cand.ID != "ecc_x" {
		t.Errorf("ID = %q", cand.ID)
	}
	if cand.Rule != "Do foo." {
		t.Errorf("Rule = %q", cand.Rule)
	}
	if cand.Why != "Because bar." {
		t.Errorf("Why = %q", cand.Why)
	}
	if cand.State != schema.InstinctStateCandidate {
		t.Errorf("State = %q", cand.State)
	}
	if cand.Scope.Level != schema.ScopeRepo {
		t.Errorf("Scope.Level = %q", cand.Scope.Level)
	}
	if cand.Scope.RepoName != "claude" {
		t.Errorf("Scope.RepoName = %q", cand.Scope.RepoName)
	}
}

func TestMigrate_SkillCarriesEvolvedFrom(t *testing.T) {
	corp := &Corpus{
		Skills: []Skill{
			{
				Name:        "background-process-detachment",
				Description: "x",
				EvolvedFrom: []string{"a", "b"},
				Body:        "body",
				SourcePath:  "/tmp/y.md",
			},
		},
	}
	res := Migrate(corp, MigrateOptions{
		NowFn:        func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) },
		MonorepoName: "bough",
	})
	if len(res.SkillArtifacts) != 1 {
		t.Fatalf("SkillArtifacts = %d", len(res.SkillArtifacts))
	}
	art := res.SkillArtifacts[0]
	if art.Kind != schema.ArtifactKindSkill {
		t.Errorf("Kind = %q", art.Kind)
	}
	if len(art.SourceInstincts) != 2 {
		t.Errorf("SourceInstincts = %v", art.SourceInstincts)
	}
	if art.Provenance.GeneratedBy == "" {
		t.Errorf("Provenance.GeneratedBy empty")
	}
}

func TestParseConfidence(t *testing.T) {
	cases := map[string]float64{
		"0.8":  0.8,
		"80%":  0.8,
		"80":   0.8,
		"":     0,
		"100%": 1.0,
		"5":    0.05,
	}
	for in, want := range cases {
		if got := parseConfidence(in); got != want {
			t.Errorf("parseConfidence(%q) = %f, want %f", in, got, want)
		}
	}
}

// Helpers

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWriteFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}
