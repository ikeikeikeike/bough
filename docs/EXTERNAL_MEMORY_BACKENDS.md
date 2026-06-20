# External memory backends (mem0 / Graphiti / Letta)

The `bough memory status` CLI notice that suggests "consider mem0 or graphiti" points here. This document explains how to wire an external memory backend and what the adapter author needs to know.

## Status

| backend  | v0.5.x                                                         | v0.6.0                                              | v0.6.x                                       |
|----------|----------------------------------------------------------------|-----------------------------------------------------|----------------------------------------------|
| mem0     | adapter skeleton (`examples/memory-plugin-mem0-skeleton/`)     | ✅ official `bough-plugin-memory-mem0` ships         | feature parity refinements                    |
| Graphiti | not yet                                                        | skeleton + docker-compose snippet (`examples/memory-plugin-graphiti-skeleton/`) | ✅ official `bough-plugin-memory-graphiti` ships as separate release artifact |
| Letta    | not yet                                                        | not yet (Letta is agent runtime, not memory layer)  | community plugin                              |

v0.5 shipped the wire contract and the SQLite reference-fallback; v0.6 adds mem0 as the first external backend. v0.6 keeps Graphiti deferred (round 4 AI #2) because its sidecar lifecycle (graph DB + Graphiti server + LLM extraction) does not fit the per-worktree plugin model the bough engine plugins use.

## Why we delegate

bough is intentionally not a memory engine. Round 1 → 2.5 → 3 of external AI design review converged on a clear rule:

- Memory store / retrieval is a solved problem with mature OSS (mem0 / Graphiti / Letta).
- Skill extraction / capability compilation is still a research-stage open question.
- bough therefore owns canonical schemas, scope, lifecycle, safety, and conformance — but never the memory intelligence itself.

The SQLite reference-fallback exists for offline development, conformance testing, GitHub Release single-binary install, and audit fallback when the configured external backend is unreachable. It is not competing with mem0 / Graphiti.

## v0.6 mem0 plugin

The official mem0 adapter ships as `bough-plugin-memory-mem0` in v0.6.0. Set `kind: mem0` in `memory_backends`:

```yaml
instinct:
  enabled: true
  default_memory_backend: mem0
  fallback_on_error: true

memory_backends:
  - kind: mem0
    role: external
  - kind: sqlite
    role: reference-fallback
    path: ".bough/memory/fallback.db"
```

The plugin reads its endpoint / API key from environment variables (see `plugins/memory/mem0/CONTRACT.md`):

- `BOUGH_MEMORY_MEM0_ENDPOINT` (required) — mem0 base URL (cloud or self-hosted).
- `BOUGH_MEMORY_MEM0_API_KEY` (optional) — organisation API key.
- `BOUGH_MEMORY_MEM0_NAMESPACE` (optional) — multi-tenant prefix prepended to every `user_id`.
- `BOUGH_MEMORY_MEM0_TIMEOUT` (optional) — Go duration; default 10s.

### Namespace mapping (mem0-layered, round 4 AI #2)

mem0's `user_id` + `session_id` pair carries the bough scope as:

```
global    →  user_id    = global/<user@host>
repo      →  user_id    = repo/<repo_hash>/<root_hash>
worktree  →  user_id    = repo/<repo_hash>/<root_hash>
             session_id = worktree/<worktree_id>
```

The repo / worktree split puts the long-lived identity on `user_id` and the per-branch identity on `session_id` so two worktrees of the same repo share the user namespace but stay queryable per branch through `session_id`.

The canonical hash derivation (sha256 of the git remote URL and the monorepo root path, truncated to 16 hex chars) appears in `docs/NAMESPACE_MAPPING.md`.

### Cache

The plugin keeps a 30 s TTL + LRU 512 read-through cache for `Query`. Cache entries are dropped whenever a `Store` / `Forget` / `Import` succeeds on the same scope, so subsequent queries never observe a row mem0 no longer holds. See `plugins/memory/mem0/cache.go`.

### Fallback policy (round 4 AI #1 + #2 split-brain mitigation)

bough's `instinct.fallback_on_error: true` is consulted **only from `Query`**. The host coordinator replays the same `QueryReq` against the SQLite reference-fallback when mem0 errors, so a primary outage degrades to "stale but available" rather than "no memory at all".

`Store` / `Forget` / `Import` deliberately **never** fall back to SQLite — falling back on writes would split-brain the dataset (mem0 holds N rows, SQLite holds N+1). The plugin advertises `DedupeKey=false` and `SourceEventID=false` so the coordinator does not retry the write either; failures surface loud.

The v0.6.x roadmap adds `bough memory reconcile --from sqlite --to mem0` for the asynchronous Sync Queue case where local fallback writes need to be replayed after mem0 recovers.

## v0.6 Graphiti plugin (skeleton only)

Graphiti (Zep) is a temporal knowledge graph backend best suited for:

- facts that change over time, with temporal queries
- entity / relation queries beyond raw text matches
- team-scale memory needing an audit / provenance graph

Graphiti requires a graph DB sidecar (Neo4j / FalkorDB / Postgres) plus its own service process plus an LLM for entity extraction. Round 4 AI #2: that lifecycle does not fit cleanly into v0.6.0 alongside mem0 + CapabilityCompiler + MCP server + signing, so v0.6.0 ships **only the skeleton** at `examples/memory-plugin-graphiti-skeleton/`:

- `README.md` explaining the contract and namespace mapping
- `docker-compose.yml` bringing up Neo4j + Graphiti for local development

The official `bough-plugin-memory-graphiti` binary ships in v0.6.x as a separate release artifact (= GoReleaser builder ID `graphiti`) so installing the heavyweight stack stays opt-in.

## Community plugins

Adapter authors should:

1. Read `plugins/memory/api/CONTRACT.md` for the wire spec.
2. Copy `examples/memory-plugin-template/` as a starting point.
3. Run the conformance suite against your binary:
   ```
   BOUGH_CONFORMANCE_MEMORY_PLUGIN_BIN=$PWD/dist/bough-plugin-memory-<name> \
       go test -tags=conformance ./...
   ```
4. Publish the binary alongside your own `docs/INTEGRATION.md` covering namespace mapping, auth, and the v0.5-stable feature subset your backend implements.

See [SECURITY.md](SECURITY.md) for the trust model around third-party plugins.
