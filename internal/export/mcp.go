package export

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ikeikeikeike/bough/pkg/schema"
	capapi "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// MCPEmitter renders a CapabilityArtifact as an MCP wire object —
// one of tool / resource / prompt depending on Target.MCPKind. The
// spec pin is MCP 2025-11-25 (https://modelcontextprotocol.io/
// specification/2025-11-25). Round 4 AI #2: v0.6 is read-only first,
// so tools advertise `state_changing: false` by default and the CLI
// refuses to emit state-changing tools unless `--allow-write` is
// passed (the gate sits at the CLI, not here).
//
// One emitter handles all three MCP kinds; the host-side CLI sets
// Target.MCPKind based on the artifact's Kind:
//
//	tool       → executable Tool         (= ArtifactKindTool / Command)
//	resource   → readable Resource       (= ArtifactKindMemory / Rule)
//	prompt     → reusable Prompt         (= ArtifactKindWorkflow / Skill)
type MCPEmitter struct{}

const mcpSpecVersion = "2025-11-25"

func (MCPEmitter) Format() string { return "mcp" }

// Emit dispatches on Target.MCPKind. Unknown / empty kinds default
// to "tool" so callers that omit MCPKind still produce a valid
// payload.
func (MCPEmitter) Emit(_ context.Context, a schema.CapabilityArtifact, _ capapi.EmitOptions) (*capapi.EmitResult, error) {
	kind := a.Target.MCPKind
	if kind == "" {
		kind = "tool"
	}
	var (
		payload  any
		filename string
	)
	switch kind {
	case "resource":
		payload = mcpResource(a)
		filename = safeFilename(a.ID) + ".resource.json"
	case "prompt":
		payload = mcpPrompt(a)
		filename = safeFilename(a.ID) + ".prompt.json"
	default: // tool
		payload = mcpTool(a)
		filename = safeFilename(a.ID) + ".tool.json"
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("mcp emit %s: marshal: %w", a.ID, err)
	}
	return &capapi.EmitResult{
		Filename:    filename,
		ContentType: "application/json",
		Bytes:       raw,
	}, nil
}

// mcpTool builds the MCP Tool wire object. Round 4 AI #2: v0.6
// surfaces tools as read-only by default. The CLI flips
// state_changing only when the operator passed --allow-write.
func mcpTool(a schema.CapabilityArtifact) map[string]any {
	tool := map[string]any{
		"$schema":       mcpSpecVersion,
		"kind":          "tool",
		"name":          a.Name,
		"description":   a.Description,
		"inputSchema":   inputSchemaFromContract(a.Contract),
		"state_changing": a.Contract.StateChanging,
		"provenance":    provenancePayload(a),
	}
	if a.Checksum != "" {
		tool["checksum"] = a.Checksum
	}
	return tool
}

// mcpResource builds the MCP Resource wire object. Resources hold
// rule / memory text the agent reads but does not invoke.
func mcpResource(a schema.CapabilityArtifact) map[string]any {
	resource := map[string]any{
		"$schema":     mcpSpecVersion,
		"kind":        "resource",
		"uri":         fmt.Sprintf("bough://capabilities/%s", a.ID),
		"name":        a.Name,
		"description": a.Description,
		"mimeType":    "text/markdown",
		"text":        a.Description,
		"provenance":  provenancePayload(a),
	}
	if a.Checksum != "" {
		resource["checksum"] = a.Checksum
	}
	return resource
}

// mcpPrompt builds the MCP Prompt wire object. Prompts package a
// workflow / skill so a client can re-invoke it with named args.
func mcpPrompt(a schema.CapabilityArtifact) map[string]any {
	args := make([]map[string]any, 0, len(a.Contract.Inputs))
	for _, in := range a.Contract.Inputs {
		args = append(args, map[string]any{
			"name":        in,
			"description": in,
			"required":    false,
		})
	}
	prompt := map[string]any{
		"$schema":     mcpSpecVersion,
		"kind":        "prompt",
		"name":        a.Name,
		"description": a.Description,
		"arguments":   args,
		"provenance":  provenancePayload(a),
	}
	if a.Checksum != "" {
		prompt["checksum"] = a.Checksum
	}
	return prompt
}

// inputSchemaFromContract synthesises a minimal JSON Schema from the
// Contract group. v0.6 keeps it dumb — every input is a string —
// because round 4 AI #2 deferred richer type inference to v0.6.x.
func inputSchemaFromContract(c schema.Contract) map[string]any {
	props := make(map[string]any, len(c.Inputs))
	for _, in := range c.Inputs {
		props[in] = map[string]any{"type": "string"}
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
	}
}

// provenancePayload mirrors the agent-skill emitter's frontmatter
// so an MCP consumer can verify the artifact against the source
// git state.
func provenancePayload(a schema.CapabilityArtifact) map[string]any {
	out := map[string]any{
		"generated_by": a.Provenance.GeneratedBy,
	}
	if a.Provenance.SourceRef != "" {
		out["source_ref"] = a.Provenance.SourceRef
	}
	if a.Provenance.TreeSHA != "" {
		out["tree_sha"] = a.Provenance.TreeSHA
	}
	if len(a.Provenance.InstinctIDs) > 0 {
		out["instinct_ids"] = a.Provenance.InstinctIDs
	}
	if len(a.Provenance.EvidenceFingerprints) > 0 {
		out["evidence_fingerprints"] = a.Provenance.EvidenceFingerprints
	}
	return out
}
