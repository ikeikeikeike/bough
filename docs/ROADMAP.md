# bough roadmap

Round 3 external review (June 2026) settled the v0.5 → v0.6 → v0.7+ shape. This document is the canonical reference; the release CHANGELOG ties specific commits back to each item.

## v0.5.0 — Foundation

- ✅ 4 plugin contracts frozen (`plugins/{memory,instinct,capability,evaluator}/api/`)
- ✅ Canonical schemas in `pkg/schema/` (TraceBundle, InstinctCandidate, Instinct, CapabilityArtifact)
- ✅ `MemoryBackend` interface with 7 methods (Health, Capabilities, Store, Query, Forget, Export, Import)
- ✅ `InstinctMinter` interface with 1 method; bough core ships a built-in default
- ✅ `CapabilityCompiler` and `SkillEvaluator` interfaces frozen as stubs for v0.6 / v0.7+
- ✅ SQLite reference-fallback plugin with WAL + busy_timeout + FTS5 + metadata column
- ✅ Coordinator with redaction, poisoning guard, source-aware confidence, decay scheduler, promote, events.jsonl audit
- ✅ Stdin ingest as the PRIMARY observer path
- ✅ Claude `.jsonl` file watch as opt-in beta with inode rotation + truncate handling
- ✅ `bough instinct` and `bough memory` CLI subcommands
- ✅ conformance/memory + conformance/instinct suites with mock plugins
- ✅ pluginhost legacy v0.3 fallback removed

## v0.5.1 — Round 3 follow-up patch (shipped 2026-06-20)

Drop-in patch on top of v0.5.0; no schema, plugin contract, or binary-API changes.

- ✅ `instinct.fallback_on_error` consumed by `coordinator.Query` (MEDIUM #15)
- ✅ `bough memory import` / `bough instinct import` actually restore rows (MEDIUM #17)
- ✅ `events.jsonl` path required to be absolute, anchored to the monorepo root (LOW #18)
- ✅ Round-trip regression tests for SQLite YAML/JSONL Import

## v0.6.0 — External memory + capability compilation

Round 4 external review (June 2026) scoped v0.6.0 to mem0 first-
class + capability compile + read-only MCP + signing scaffolding;
Graphiti is deferred to v0.6.x as a separate GoReleaser archive.

- ✅ mem0 official plugin (`bough-plugin-memory-mem0`) with
  namespace mapping + 30s TTL Query cache + Read-only fallback to
  the SQLite reference-fallback (round 4 AI #1 + #2 split-brain
  Blocker 1 mitigation)
- ✅ MemoryBackend.Capabilities advertise widened to 17 fields
  (semantic / vector / graph / temporal / namespace / metadata /
  soft_delete / ttl / dedupe_key / source_event_id / bulk_import /
  bulk_export / eventual_consistency / max_batch_size /
  max_query_tokens, round 4 priority A12)
- ✅ `CapabilityCompiler` materialised with deterministic Checksum
  + Target / Invocation / Contract / Validation / Provenance
  metadata groups (round 4 priority A3)
- ✅ `bough capability compile --to <agent-skill|claude-skill|mcp>
  --profile <host>` — agent-skill is the v0.6 default (round 4
  priority A2: bough is a host-neutral OSS layer)
- ✅ Three builtin emitters (`agent-skill`, `claude-skill`,
  `mcp`) with the Emitter interface lifted into
  `plugins/capability/api/` so v0.6.x can graduate them into
  plugin slots (round 4 priority A13)
- ✅ `bough-mcp-server` companion binary with read-only first
  surface, MCP spec_version pin 2025-11-25, and the round 4 AI #1
  stdin-EOF zombie guard
- ✅ Plugin signing scaffolding: cosign + minisign acceptance,
  `bough plugins verify`, GoReleaser keyless integration
  (full enforcement timeline in docs/SIGNING.md)
- ✅ Graphiti skeleton + docker-compose snippet (binary in v0.6.x)
- ✅ docs/CAPABILITY_COMPILER.md, docs/MCP_SERVER.md,
  docs/SIGNING.md ship alongside the binaries

## v0.6.x — Experimental compilers

- SkillX adapter (round 3 AI #3: zjunlp/SkillX research repo)
- Anything2Skill-style compiler
- Alita-G MCP tool compiler
- All ship as community / experimental plugins under `examples/`

## v0.7+ — Evaluator-driven evolution

- `SkillEvaluator` materialised
- GEPA reflective prompt optimiser adapter
- TextGrad gradient evaluator adapter
- MUSE-Autoskill lifecycle evaluator adapter
- SkillAudit paired-trajectory auditor adapter (round 3 AI #3)

## What bough deliberately does NOT do

- weight updates (SEAL / SFT / RLHF) — model-tier concern, not orchestration
- `instinct → skill → command → agent` as a forced single chain — round 1 rejected the rigid hierarchy in favour of a parallel compile target set
- proprietary vendor memory (OpenAI Memory, Anthropic Memory) — vendor lock-in
- GPL/AGPL backends — license drift for downstream MIT/Apache users

## Why these choices

The full design history lives in the round 1 / 2 / 2.5 / 3 synthesis notes (see PR #N). The recurring thread: **bough is a per-worktree memory orchestration layer, not an agent memory system.** Every choice on this roadmap reinforces that boundary.
