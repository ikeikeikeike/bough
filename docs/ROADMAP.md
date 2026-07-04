# bough roadmap

Round 3 external review (June 2026) settled the v0.5 → v0.6 → v0.7+ shape. This document is the canonical reference; the release CHANGELOG ties specific commits back to each item.

## v0.9.2 — Full loop (shipped 2026-06-25)

Closes the continuous-learning loop. v0.9.0 observed, v0.9.1
evolved; v0.9.2 injects what was learned into the next session,
reinforces useful instincts at session end, and migrates existing
ECC corpora.

- ✅ `bough inject-context` — UserPromptSubmit hook, confidence-
  ranked instinct block (~9.5 KB cap), pure filesystem. Wired into
  `bough hook handle --event UserPromptSubmit` so one entry records
  + injects.
- ✅ `bough session-end` — SessionEnd hook, reinforces exercised
  instincts one confidence band up + appends eval/scores.jsonl.
- ✅ `bough preserve-instincts` — PreCompact hook, MEMORY.md top-5
  snapshot.
- ✅ `bough observer start/stop/status` — opt-in background daemon
  (PID-file lifecycle, Setsid detach, no systemd/launchd).
- ✅ `bough ecc import` — migrate an existing ECC corpus into
  bough's namespace (dry-run default; --apply copies).

The `claude --worktree X` → observe → evolve → inject loop is now
end-to-end. The v0.9 ECC port is complete.

## v0.9.1 — Evolve pipeline (shipped 2026-06-25)

The evolve half of the ECC port. v0.9.0 shipped the observer (=
instinct extraction); v0.9.1 ships the five-gate clustering pipeline
that turns instincts into skills / agents / commands.

- ✅ `bough evolve` (preview, no LLM) / `bough evolve --generate`
  (GATE 5 + emit). The ECC `/evolve-skill-manual-v3` UX.
- ✅ `internal/evolve/` — tokenize / Jaccard / coverage, connected-
  component clustering, the four mechanical gates (ECC v3 verbatim:
  MEMBER_MIN=2 / COH_MIN=0.20 / LEXICON_COVERAGE_MAX=0.55 /
  REL_ISOLATION_MIN=0.40), GATE 5 LLM judge via `claude --print`,
  cluster-labels.json with the sacred-string rule + backup, and the
  SKILL.md / agent / command emitters.
- ✅ GATE 5 verdict routing: PASS mints a fresh label, DOUBT reuses
  the nearest prior label, FAIL rejects.
- ✅ Agent eligibility (cluster >= 3 + avg conf >= 0.75) + command
  eligibility (workflow domain + conf >= 0.70), ECC thresholds.
- ✅ `claudecli.Provider.GenerateRaw` for pre-rendered prompts.

v0.9.2 (= upcoming): `bough inject-context` UserPromptSubmit hook +
SessionEnd handlers + PreCompact + optional observer daemon +
`bough ecc import`.

## v0.9.0 — ECC verbatim port (shipped 2026-06-23)

The "reset to the operator's vision" release. v0.5-v0.8 accreted
memory backends, capability compilers, MCP server, evaluator
adapters, judges, ECC import helpers — none of which the operator's
vision needs. v0.9 deletes them and ships threecorp ECC's
continuous-learning architecture verbatim in Go.

Mechanism: `claude --print` subprocess. No Anthropic API call. LLM
cost stays inside the operator's existing Claude Code subscription.

- ✅ `internal/homunculus/` — `~/.local/share/bough-homunculus/`
  layout, project_id (= sha256[:12] of git remote URL stripped),
  atomic registry, instinct file IO with filename ↔ id enforcement.
- ✅ `internal/observe/` — `observations.jsonl` writer (O_APPEND
  per-line atomic) + Anthropic env scrub.
- ✅ `internal/prompts/` — //go:embed defaults + 3-layer override
  resolver. Template.Version is sha256[:12] of body for cache
  pinning.
- ✅ `internal/provider/claudecli/` — Option A′ subprocess provider
  + Limiter (10 calls/session, 30/hour, 3-failure breaker, 15min
  cooldown).
- ✅ `bough observer run-once` — synchronous single-shot extraction
  pass with `--dry-run` preview.
- ✅ `bough instinct status / list / show` — read-side corpus
  inspection (5-bucket confidence histogram, filterable list).
- ✅ `bough doctor` — continuous-learning posture block (claude CLI
  on PATH, Anthropic env scrub warning, Limiter defaults,
  homunculus root).

v0.9.1 + v0.9.2 (= upcoming):

- 5-gate evolve pipeline (= ECC v3 verbatim, MEMBER_MIN=2 / COH_MIN
  =0.20 / LEXICON_COVERAGE_MAX=0.55 / REL_ISOLATION_MIN=0.40) +
  GATE 5 LLM judge + cluster-labels.json + SKILL.md / agent /
  command emit.
- `bough inject-context` UserPromptSubmit hook (9.5KB cap +
  confidence-sorted LRU) + SessionEnd handlers (summary /
  evaluate / evolve-claudemd) + PreCompact + optional observer
  daemon + `bough ecc import` migration.

## v0.5.0 - v0.8.0 — Superseded memory-orchestration surface

v0.5.0 through v0.8.0 (June 2026) built a `MemoryBackend` /
`InstinctMinter` plugin architecture — pluggable SQLite / mem0 /
Graphiti backends, a `CapabilityCompiler` that materialised instincts
into memory / rule / skill / command / tool / agent / evaluator
artifacts, a read-only `bough-mcp-server`, `SkillEvaluator` adapters
(GEPA / TextGrad / MUSE / SkillAudit), and a "v0.7 Bootstrap" plan for
LLM-judged clustering on top of it. v0.9.0 reset all of it in favour
of the ECC-verbatim continuous-learning port described above; none of
it shipped past v0.8.1. See CHANGELOG.md for the release-by-release
detail if you need the history.

## What bough deliberately does not do

These are durable non-goals, independent of which continuous-learning
design is current:

- Weight updates (SEAL / SFT / RLHF) — a model-tier concern, not
  something an orchestration layer does.
- Proprietary vendor memory (OpenAI Memory, Anthropic Memory) —
  avoids vendor lock-in.
- Forcing every instinct through a single `skill → command → agent`
  chain — `bough evolve` clusters an instinct into whichever kind
  (skill / command / agent) its content warrants, not a chain every
  instinct must pass through.

bough is a per-worktree development-environment orchestrator, not an
agent memory system; these non-goals keep it from drifting into either.
