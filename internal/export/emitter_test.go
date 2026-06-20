package export

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ikeikeikeike/bough/pkg/schema"
	capapi "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// sampleArtifact builds a non-empty CapabilityArtifact every
// emitter test uses. ComputeChecksum is called so the round 4
// AI #1 idempotency token is part of the frontmatter assertions.
func sampleArtifact() schema.CapabilityArtifact {
	a := schema.CapabilityArtifact{
		Stability:           schema.StabilityExperimental,
		ID:                  "rule-early-returns",
		Kind:                schema.ArtifactKindRule,
		Name:                "prefer early returns",
		Description:         "prefer early returns over nested conditionals",
		InvocationCondition: "when nesting depth would exceed 2",
		Steps:               []string{"check guard clauses", "return early"},
		Constraints:         []string{"do not duplicate cleanup"},
		EvidenceRefs:        []string{"evt-1", "evt-2"},
		Confidence:          0.8,
		Version:             "v0.6.0",
		SourceInstincts:     []string{"rule-early-returns"},
		Scope:               schema.Scope{Level: schema.ScopeWorktree, WorktreeID: "F-x", RepoName: "auba"},
		Provenance: schema.Provenance{
			InstinctIDs:    []string{"rule-early-returns"},
			SourceRef:      "abc1234",
			TreeSHA:        "deadbeef",
			GeneratedBy:    "bough@v0.6.0",
		},
		Contract: schema.Contract{
			Inputs:        []string{"diff"},
			Outputs:       []string{"narrated rationale"},
			StateChanging: false,
		},
	}
	a.ComputeChecksum()
	return a
}

func TestAgentSkillEmitter_FormatAndShape(t *testing.T) {
	e := AgentSkillEmitter{}
	if e.Format() != "agent-skill" {
		t.Errorf("format: %q", e.Format())
	}
	out, err := e.Emit(context.Background(), sampleArtifact(), capapi.EmitOptions{Host: "claude-code"})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	body := string(out.Bytes)
	if !strings.HasPrefix(body, "---\n") {
		t.Errorf("frontmatter should open with `---`: %q", body[:10])
	}
	for _, key := range []string{"name:", "description:", "source_ref:", "tree_sha:", "generated_by:", "checksum:", "host: claude-code"} {
		if !strings.Contains(body, key) {
			t.Errorf("frontmatter should include %q: %q", key, body)
		}
	}
	if !strings.Contains(body, "## Steps") || !strings.Contains(body, "## Constraints") || !strings.Contains(body, "## Evidence") {
		t.Errorf("body should include Steps / Constraints / Evidence sections: %q", body)
	}
	if !strings.HasSuffix(out.Filename, ".md") {
		t.Errorf("filename should end .md: %q", out.Filename)
	}
}

func TestClaudeSkillEmitter_FormatAndShape(t *testing.T) {
	e := ClaudeSkillEmitter{}
	if e.Format() != "claude-skill" {
		t.Errorf("format: %q", e.Format())
	}
	out, err := e.Emit(context.Background(), sampleArtifact(), capapi.EmitOptions{Host: "claude-code"})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	body := string(out.Bytes)
	if !strings.Contains(body, "name:") || !strings.Contains(body, "description:") {
		t.Errorf("SKILL.md frontmatter requires name + description: %q", body)
	}
	if !strings.HasSuffix(out.Filename, "/SKILL.md") {
		t.Errorf("filename should be <id>/SKILL.md: %q", out.Filename)
	}
}

func TestMCPEmitter_Tool(t *testing.T) {
	e := MCPEmitter{}
	a := sampleArtifact()
	a.Target.MCPKind = "tool"
	out, err := e.Emit(context.Background(), a, capapi.EmitOptions{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.HasSuffix(out.Filename, ".tool.json") {
		t.Errorf("tool filename: %q", out.Filename)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes, &got); err != nil {
		t.Fatalf("Unmarshal tool: %v", err)
	}
	if got["kind"] != "tool" {
		t.Errorf("kind: %v", got["kind"])
	}
	if got["state_changing"] != false {
		t.Errorf("v0.6 read-only first should default state_changing=false: %v", got["state_changing"])
	}
	if got["$schema"] != "2025-11-25" {
		t.Errorf("spec_version pin: %v", got["$schema"])
	}
	prov, ok := got["provenance"].(map[string]any)
	if !ok || prov["source_ref"] != "abc1234" {
		t.Errorf("provenance.source_ref: %v", got["provenance"])
	}
}

func TestMCPEmitter_Resource(t *testing.T) {
	e := MCPEmitter{}
	a := sampleArtifact()
	a.Target.MCPKind = "resource"
	out, err := e.Emit(context.Background(), a, capapi.EmitOptions{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got["kind"] != "resource" {
		t.Errorf("kind: %v", got["kind"])
	}
	uri, _ := got["uri"].(string)
	if !strings.HasPrefix(uri, "bough://capabilities/") {
		t.Errorf("resource uri scheme: %q", uri)
	}
}

func TestMCPEmitter_PromptDefault(t *testing.T) {
	e := MCPEmitter{}
	a := sampleArtifact()
	a.Target.MCPKind = "prompt"
	out, err := e.Emit(context.Background(), a, capapi.EmitOptions{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got["kind"] != "prompt" {
		t.Errorf("kind: %v", got["kind"])
	}
	// Inputs contract is single "diff" → one argument entry.
	args, _ := got["arguments"].([]any)
	if len(args) != 1 {
		t.Errorf("arguments length: %d", len(args))
	}
}

func TestDefaultRegistry_RegistersThree(t *testing.T) {
	r := DefaultRegistry()
	for _, f := range []string{"agent-skill", "claude-skill", "mcp"} {
		if _, err := r.Lookup(f); err != nil {
			t.Errorf("DefaultRegistry missing %s: %v", f, err)
		}
	}
	if got := r.Formats(); len(got) != 3 {
		t.Errorf("DefaultRegistry should hold 3 emitters, got %v", got)
	}
}
