package export

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikeikeikeike/bough/pkg/schema"
	capapi "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// ClaudeSkillEmitter renders a CapabilityArtifact as an Anthropic
// Agent Skills SKILL.md (= YAML frontmatter + markdown body, per
// the spec at https://platform.claude.com/docs/en/agents-and-tools/
// agent-skills/overview). v0.6.0 ships the minimal frontmatter
// (name + description, the only fields Anthropic's loader reads
// into the system prompt) while preserving the round 4 supply-chain
// provenance the agent-skill emitter exposes.
//
// The two emitters are intentionally close — opts.Host =
// "claude-code" tells the CLI to prefer this emitter, but the
// downstream bytes mostly differ in frontmatter keys (= SKILL.md
// uses `description` for Anthropic's matcher; gh skill carries
// additional `kind` / `host` / `state_changing` fields).
type ClaudeSkillEmitter struct{}

func (ClaudeSkillEmitter) Format() string { return "claude-skill" }

func (ClaudeSkillEmitter) Emit(_ context.Context, a schema.CapabilityArtifact, opts capapi.EmitOptions) (*capapi.EmitResult, error) {
	_ = opts
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", agentSkillEscape(a.Name))
	fmt.Fprintf(&b, "description: %s\n", agentSkillEscape(a.Description))
	if a.Provenance.SourceRef != "" {
		fmt.Fprintf(&b, "source_ref: %s\n", a.Provenance.SourceRef)
	}
	if a.Provenance.TreeSHA != "" {
		fmt.Fprintf(&b, "tree_sha: %s\n", a.Provenance.TreeSHA)
	}
	if a.Checksum != "" {
		fmt.Fprintf(&b, "checksum: %s\n", a.Checksum)
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", a.Name)
	if a.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", a.Description)
	}
	if a.InvocationCondition != "" {
		b.WriteString("## When to apply\n\n")
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
	return &capapi.EmitResult{
		Filename:    safeFilename(a.ID) + "/SKILL.md",
		ContentType: "text/markdown",
		Bytes:       []byte(b.String()),
	}, nil
}
