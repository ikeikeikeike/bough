// Package export holds the v0.6 CapabilityCompiler emitters that
// render a CapabilityArtifact into a target-specific shape (agent-
// skill / claude-skill / mcp-tool / mcp-resource / mcp-prompt /
// json). Round 4 priority A2 made agent-skill the default — bough
// is a host-neutral OSS orchestration layer and `gh skill` (GitHub
// Agent Skills) ships as the cross-runtime distribution surface.
//
// Each emitter implements plugins/capability/api/Emitter. The
// registry helper (default.go) wires the three builtin emitters
// into a populated *capability.Registry for the CLI bootstrap.
//
// v0.6.x graduates the emitters into gRPC plugin slots; the
// internal/export/ implementations stay as the reference shape so
// community emitters can mirror the SKILL.md / MCP wire layout.
package export

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikeikeikeike/bough/pkg/schema"
	capapi "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// AgentSkillEmitter renders a CapabilityArtifact as a `gh skill`
// frontmatter + markdown body (= 2026-04 GitHub Agent Skills
// surface). It is the v0.6 default target because the distribution
// channel is host-neutral — Claude Code, GitHub Copilot, Cursor,
// and Gemini CLI all accept agent-skill style files.
type AgentSkillEmitter struct{}

// Format identifies this emitter in the registry.
func (AgentSkillEmitter) Format() string { return "agent-skill" }

// Emit renders the artifact. Round 3 priority B + round 4 supply-
// chain: the frontmatter carries provenance (= source_ref, tree_sha,
// generated_by, checksum) so a downstream verifier can confirm the
// skill came from the recorded git state.
func (AgentSkillEmitter) Emit(_ context.Context, a schema.CapabilityArtifact, opts capapi.EmitOptions) (*capapi.EmitResult, error) {
	host := opts.Host
	if host == "" {
		host = "generic"
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", agentSkillEscape(a.Name))
	fmt.Fprintf(&b, "description: %s\n", agentSkillEscape(a.Description))
	if a.Version != "" {
		fmt.Fprintf(&b, "version: %s\n", a.Version)
	}
	fmt.Fprintf(&b, "kind: %s\n", a.Kind)
	fmt.Fprintf(&b, "host: %s\n", host)
	if a.Provenance.SourceRef != "" {
		fmt.Fprintf(&b, "source_ref: %s\n", a.Provenance.SourceRef)
	}
	if a.Provenance.TreeSHA != "" {
		fmt.Fprintf(&b, "tree_sha: %s\n", a.Provenance.TreeSHA)
	}
	if a.Provenance.GeneratedBy != "" {
		fmt.Fprintf(&b, "generated_by: %s\n", a.Provenance.GeneratedBy)
	}
	if a.Checksum != "" {
		fmt.Fprintf(&b, "checksum: %s\n", a.Checksum)
	}
	if a.Contract.StateChanging {
		b.WriteString("state_changing: true\n")
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", a.Name)
	if a.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", a.Description)
	}
	if a.InvocationCondition != "" {
		b.WriteString("## When to invoke\n\n")
		fmt.Fprintf(&b, "%s\n\n", a.InvocationCondition)
	}
	if len(a.Steps) > 0 {
		b.WriteString("## Steps\n\n")
		for _, s := range a.Steps {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}
	if len(a.Constraints) > 0 {
		b.WriteString("## Constraints\n\n")
		for _, s := range a.Constraints {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}
	if len(a.EvidenceRefs) > 0 {
		b.WriteString("## Evidence\n\n")
		for _, s := range a.EvidenceRefs {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}
	if len(a.Invocation.RequiredEnv) > 0 || len(a.Invocation.RequiredBins) > 0 {
		b.WriteString("## Prerequisites\n\n")
		for _, env := range a.Invocation.RequiredEnv {
			fmt.Fprintf(&b, "- env: %s\n", env)
		}
		for _, bin := range a.Invocation.RequiredBins {
			fmt.Fprintf(&b, "- bin: %s\n", bin)
		}
		b.WriteString("\n")
	}
	return &capapi.EmitResult{
		Filename:    safeFilename(a.ID) + ".md",
		ContentType: "text/markdown",
		Bytes:       []byte(b.String()),
	}, nil
}

// agentSkillEscape quotes values containing characters YAML would
// interpret. Matches the sqlite reference-fallback's escapeYAML so
// payloads round-trip across export targets.
func agentSkillEscape(s string) string {
	if strings.ContainsAny(s, ":#@\n") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// safeFilename normalises an Instinct ID into a filesystem-safe
// stem. Real Instinct IDs are sha256 hex by host convention, but
// imported artifacts may carry arbitrary characters so we sanitise
// defensively.
func safeFilename(id string) string {
	if id == "" {
		return "artifact"
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "artifact"
	}
	return b.String()
}
