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

## v0.6.x — Patch + experimental compilers

Dogfooding the v0.6.0 ship surfaced one false-negative in the host
config validator and re-framed the v0.7 Bootstrap design. v0.6.x
absorbs both before the next minor.

- `bough config validate` accepts v0.5+ root sections (`instinct`,
  `engines`, `memory_backends`, `export`) — the v0.5 schema bump
  forgot to mirror the new fields into the LegacyConfig superset the
  validator's first-pass decoder uses, so `validate` reports
  `unknown field` while every other subcommand loads the file
  cleanly. (Post-ship finding, 2026-06-22.)
- `bough memory reconcile --from sqlite --to mem0` materialises the
  split-brain recovery story the v0.6 mem0 plugin promised in
  `docs/EXTERNAL_MEMORY_BACKENDS.md`.
- `bough-mcp-server --allow-write` and the three state-changing
  Tools (`memory.store`, `memory.forget`, `memory.promote`), all
  routed through the same audit log the v0.5 coordinator writes.
- Plugin signing strict mode (`require_signed: true` actually
  refuses to spawn unverified plugins; v0.6.0 only exposes the
  verify CLI).
- SkillX adapter (round 3 AI #3: zjunlp/SkillX research repo)
- Anything2Skill-style compiler
- Alita-G MCP tool compiler
- Experimental compilers ship as community / experimental plugins
  under `examples/`.

## v0.7.0 — Bootstrap safety floor (shipped 2026-06-23)

The "automation is safe to turn on" floor. Nothing in this release
calls an external LLM; every artifact lands in a reviewable form
before touching the memory backend. Round 5 review (= 2026-06-22,
two independent external AI passes) split the LLM-touching surface
into v0.7.1 and front-loaded the safety + observability surfaces
into v0.7.0. The eight sub-phases all shipped:

- ✅ O-1.1 cobra surface skeleton (`bough hook`)
- ✅ O-1.2 install / uninstall / list reconciliation
- ✅ O-1.3 replay harness + canonical testdata fixtures
- ✅ O-1.4 `bough doctor` body + top-level alias
- ✅ O-1.5 `bough bootstrap --dry-run` → `.bough/proposals/<ts>/*.md`
- ✅ O-1.6 `bough hook handle` (= raw event capture into
  `.bough/observations.jsonl`)
- ✅ O-1.7 MCP write hardening (rate-limit + scope boundary +
  append-only audit; host wires worktree-only / 60-per-min /
  `.bough/memory/mcp_audit.jsonl` defaults when `--allow-write` on)
- ✅ O-1.8 end-to-end integration test in `conformance/hooks/`

## v0.7 — Bootstrap layer (round 5 refined)

The v0.6 retrospective (2026-06-22) clarified that the user-facing
intent — "`claude --worktree X` materialises an isolated dev
environment **and** generates the artifacts the next session will
need" — needs a dedicated layer above the existing CapabilityCompiler.
Round 5 external review (2026-06-22, two independent AI passes)
agreed on the direction but flagged the original 14-day v0.7.0 scope
as overambitious: LLM-judge inference + 4-gate clustering + hook
auto-wire + cost transparency in one sprint underestimates the
Bash/Python→Go rewrite cost. Both reviewers recommended splitting
the LLM-touching layer (= GATE 5) into v0.7.1 and front-loading the
safety + observability surfaces into v0.7.0 instead. The Phase split
below incorporates that guidance.

### v0.7.0 — Bootstrap safety floor (~10 day)

The "automation is safe to turn on" floor. Nothing here calls an
external LLM; every artifact lands in a reviewable form before
touching the memory backend.

- `bough hook install` / `uninstall` (= writes / removes the
  Claude Code `WorktreeCreate` / `PreToolUse` / `PostToolUse` /
  `UserPromptSubmit` / `SessionEnd` / `PreCompact` / `Stop`
  entries against `.claude/settings.json`), with idempotent
  reconciliation so re-running on a partially-wired monorepo
  converges instead of duplicating handlers.
- **`bough hook replay --event <name> --fixture <json>`** harness
  + golden tests over a `testdata/` corpus of canonical hook
  payloads. Hook auto-wire without a replay harness was named as
  the single highest carryover risk by both round 5 reviewers.
- `bough hook doctor` / **`bough doctor`** — surfaces the
  observer status, current hook wiring, per-worktree token /
  cost meter, and signing posture in a single command. Front-
  loaded into v0.7.0 (was v0.7.1 in the pre-review plan) so
  silent-billing or silent-observer regressions get caught the
  moment Bootstrap turns on.
- `bough bootstrap --dry-run` writes candidate artifacts to
  `.bough/proposals/<timestamp>/*.md` (= Markdown frontmatter,
  one file per candidate). The operator reviews with `git diff`
  semantics and runs `bough instinct approve <id>` to promote
  into the backend. The DB never sees an artifact the operator
  has not already inspected.
- Observer event capture: raw observations persist to
  `.bough/observations.jsonl` (= append-only, signed via the
  same provenance schema the capability artifacts use). No
  inference yet; ingestion remains opt-in.
- All generated artifacts ship `state: candidate`. Promotion
  stays a human CLI action (no MCP promote tool, no auto-active
  path).
- Schema additions kept "Letta interop-ready": every artifact
  records `source_trace`, `provenance.generated_by`, a
  git-backed export path, and a `scope_boundary` field so v0.8
  can light up Letta Context Repositories or Graphiti memfs
  without a schema migration.
- MCP write surface gains the round 5 mitigation set: dry-run
  default, per-tool permission flag, per-worktree scope
  enforcement, rate limit per session, append-only audit log,
  schema validation before store. `memory.promote` stays
  refused on the MCP surface — promotion is a CLI human
  action (round 5 unanimous).

### v0.7.1 — Evolve + LLM judge (~7 day)

Splits from v0.7.0 so the LLM-touching surface ships with its
own debugging budget.

- 4-gate mechanical filter (`/evolve-skill-manual-v3` algorithm)
  ported as a single Go pipeline. v3 is the canonical algorithm;
  the upstream ECC `/evolve` clustering acts as parser / baseline
  reference, not a parallel port.
- GATE 5 LLM judge behind a `JudgeClient` interface in
  `plugins/capability/api/llm.go`. Three implementations:
  - `ClaudeJudgeClient` (= live LLM, gated by config)
  - `HeuristicJudgeClient` (= deterministic fallback for CI /
    offline)
  - `ReplayJudgeClient` (= fixture / cassette playback so the
    integration tests are reproducible)
- SQLite-backed judge cache keyed by
  `sha256(prompt_version | model_id | cluster_member_ids |
  cluster_member_hashes | nearest_prior_label |
  nearest_prior_description)` so a re-run of the same evolve
  pass never re-bills the operator.
- Audit dir `.evolve/judgements/<cache_key>.json` storing
  model, prompt_version, request hash, raw response, parsed
  verdict, cost estimate, timestamp. Append-only.
- JSON-schema-validated judge output (verdict ∈
  {PASS, DOUBT, FAIL}, confidence, reason,
  recommended_label, reuse_prior_label). temperature = 0,
  max_output_tokens fixed.
- CLAUDE.md proposal pipeline (= observe → propose → apply)
  using the same judge interface so the heuristic / replay
  fallbacks cover it too.
- Quality-gate framework concept (= user supplies the lint /
  typecheck / smoke command, bough sequences it as a Post-tool
  hook).
- Golden corpus driven by threecorp's live 346-instinct /
  21-skill / 6-agent / 116-command snapshot so the Go port's
  output diff-tests against the in-production Python output.

### v0.7.2 — ECC compatibility + dogfooding (~5 day)

- `bough ecc import` reads `~/.local/share/ecc-homunculus/
  projects/<id>/` and emits the canonical bough schema.
- Round-trip validation against the threecorp dogfooding
  corpus (= the same 346 / 21 / 6 / 116 the v0.7.1 judge
  golden tests use).
- Bug-fix budget reserved for whatever the dogfooding session
  surfaces.

### v0.7.3 — Polish (~3 day)

- `README-ja.md` (= threecorp / eiicon-company devs read the
  Japanese tagline first).
- `examples/` pack: a curated subset of upstream skills + threecorp
  commands so a first-time user sees what the bootstrap surface
  can actually emit. The full upstream catalogue stays out of the
  binary release.

### Reframed v1.0 / v2.0 vision (round 5 Q7)

Both round 5 reviewers warned against pivoting bough into a
multi-agent orchestrator (claude-flow / CrewAI territory) or a
generic memory database. The competitive moat is "worktree-native
AI development environment orchestrator" — keep it.

- **v1.0** stabilises the v0.7 surfaces (= hook install / replay,
  observer, candidate generation, `bough doctor`, MCP candidate
  tools, evolve-v3, ECC import) under semver guarantees.
- **v2.0** unlocks Letta Context Repositories interop,
  `SkillEvaluator` plus GEPA / TextGrad / MUSE-Autoskill adapters,
  signed skill registry, evaluator-driven skill retirement, and
  multi-host backends (Cursor / OpenCode / Codex). The CI-tier
  pivot (= "instincts that pass CI promote to global") becomes
  defensible only after the evaluator layer ships.

## What bough deliberately does NOT do

- weight updates (SEAL / SFT / RLHF) — model-tier concern, not orchestration
- `instinct → skill → command → agent` as a forced single chain — round 1 rejected the **chain** in favour of a parallel compile target set. This rejection is a Layer C decision and is **orthogonal** to the 2026 Layer A (memory CRUD) and Layer B (skill execution) anti-pattern literature; see `docs/CONCEPTS.md` for the three-layer split. The seven parallel targets (memory, rule, skill, command, tool, agent, evaluator) all remain valid sinks; the InstinctCandidate metadata picks which subset to materialise.
- proprietary vendor memory (OpenAI Memory, Anthropic Memory) — vendor lock-in
- GPL/AGPL backends — license drift for downstream MIT/Apache users

## Why these choices

The full design history lives in the round 1 / 2 / 2.5 / 3 synthesis notes (see PR #N). The recurring thread: **bough is a per-worktree memory orchestration layer, not an agent memory system.** Every choice on this roadmap reinforces that boundary.
