# CapabilityCompiler (v0.6.0)

The v0.6 capability subsystem turns approved instincts into
publishable artifacts (Anthropic Agent Skills, GitHub Agent Skills,
MCP tool / resource / prompt entries). This document covers the
contract, the dispatch loop, and what plugin authors should know
before extending the registry.

## What it does

```
instincts (memory)
    │
    ▼
CompileRequest                 // host-side wrapper
    │
    ▼
capability.Compiler            // internal orchestrator
    │   ├ synthesise(instinct, kind) → CapabilityArtifact
    │   ├ ComputeChecksum()                 // round 4 AI #1 idempotency
    │   └ for each Target:
    │         registry.Lookup(Format) → Emitter
    │         Emitter.Emit(...) → EmitResult
    ▼
CompileResult{Artifacts, Emissions}
```

The v0.6 dispatch is deterministic — every (instinct × kind ×
target) triple produces one artifact and one emission. The
Checksum is computed before emit so the CLI can skip already-up-to-
date files on the next run.

## CapabilityArtifact (round 4 priority A3)

`pkg/schema/capability.go` holds the v0.6 schema:

- **Identity** (v0.5): `ID`, `Kind`, `Name`, `Description`,
  `InvocationCondition`, `Inputs`, `Outputs`, `Steps`,
  `Constraints`, `EvidenceRefs`, `Confidence`, `Version`,
  `SourceInstincts`, `Scope`, `CreatedAt`, `Payload`.
- **Target** (v0.6): `Format`, `Host`, `MCPKind`. Format picks
  the emitter (`agent-skill` / `claude-skill` / `mcp`); Host
  pins the runtime layout; MCPKind is `tool` / `resource` /
  `prompt` for the MCP emitter.
- **Invocation** (v0.6): `Trigger`, `Contraindications`,
  `RequiredEnv`, `RequiredBins`.
- **Contract** (v0.6): `Inputs`, `Outputs`, `SideEffects`,
  `StateChanging`.
- **Validation** (v0.6): `Probes`, `TestCommands`,
  `ExpectedSignals` (recorded but not executed by v0.6.0 — `bough
  capability lint` arrives in v0.6.x).
- **Provenance** (v0.6, round 4 priority B + supply chain):
  `InstinctIDs`, `TraceBundleIDs`, `EvidenceFingerprints`,
  `SourceRef`, `TreeSHA`, `GeneratedBy`.
- **Checksum** (v0.6): sha256 of canonical bytes, used by the CLI
  to short-circuit no-op compiles.

The wire proto (`plugins/capability/api/proto/capability.proto`)
stays at v0.5's 17 fields; the v0.6 groups round-trip through
Payload until v0.7 graduates them to proto fields alongside the
MemoryBackend v2 bump.

## Targets (round 4 priority A2)

| Format | Description | Default Host |
|---|---|---|
| `agent-skill` | GitHub Agent Skills (gh skill) — host-neutral | `generic` |
| `claude-skill` | Anthropic Agent Skills (`SKILL.md`) | `claude-code` |
| `mcp` | MCP `tool` / `resource` / `prompt` | n/a |

`agent-skill` is the v0.6 default because bough is a host-neutral
OSS orchestration layer. Pick a different target with `--to <name>`;
use `--profile claude-code` to switch the agent-skill emitter into
the Claude-compatible layout.

## CLI

```sh
# Discover the registered emitters.
bough capability list

# Compile every active instinct in scope into agent-skill files.
bough capability compile --out-dir ./skills

# Dry-run: synthesise artifacts and print as JSON.
bough capability preview --to mcp

# Specific instincts; explicit target host.
bough capability compile \
    --instinct-id rule-1 --instinct-id rule-2 \
    --to claude-skill --profile claude-code \
    --out-dir ~/.claude/skills/bough
```

`install` and `lint` are stubs in v0.6.0 — they land in v0.6.x.

## Extending the registry

Implement the `plugins/capability/api/Emitter` interface (= `Format()
string` + `Emit(ctx, artifact, opts) (*EmitResult, error)`) and
register it before constructing the compiler:

```go
import (
    "github.com/ikeikeikeike/bough/internal/capability"
    "github.com/ikeikeikeike/bough/internal/export"
)

func main() {
    reg := export.DefaultRegistry()
    reg.Register(myEmitter{})
    compiler := capability.NewCompiler(reg)
    // ...
}
```

v0.6.x graduates emitters into gRPC plugin slots (= the Emitter
interface already lives in `plugins/capability/api/` so the
in-process implementation in `internal/export/` is migration-ready).

## Conformance

`conformance/capability/Run(t, cfg)` exercises the dispatch loop
against the caller-supplied emitter slice. Run it with
`go test ./conformance/capability/...` to ship.
