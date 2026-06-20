# bough-plugin-memory-graphiti-skeleton

Skeleton for a v0.6.x Graphiti (https://github.com/getzep/graphiti) adapter. v0.6.0 ships this skeleton **without a binary**; the official `bough-plugin-memory-graphiti` lands in v0.6.x as a separate release artifact (round 4 AI #2 deferral).

## Why deferred to v0.6.x

Graphiti is a temporal knowledge graph backed by Neo4j (or FalkorDB / Postgres). Round 4 external review observed that bundling Graphiti into v0.6.0 would inflate scope past what one release can land safely — the sidecar lifecycle (graph DB + Graphiti server + LLM extraction + ontology) does not fit the per-worktree plugin model the bough engine plugins use.

v0.6.0 therefore ships:

- this skeleton + adapter guide (= you, reading this)
- a `docker-compose.yml` snippet that brings up Neo4j + Graphiti for local development
- the contract for what the v0.6.x adapter must honour (= the v0.6 `CapabilitiesResp` cluster the bough mem0 plugin already covers)

v0.6.x ships:

- `plugins/memory/graphiti/` working adapter
- `cmd/bough-plugin-memory-graphiti/main.go` (4-line plugin.Serve)
- GoReleaser builder ID `graphiti` so the binary lands as a separate archive in the same GitHub Release

## What the v0.6.x adapter will look like

The shape mirrors the mem0 plugin (https://github.com/ikeikeikeike/bough/tree/main/plugins/memory/mem0):

```go
type graphitiProvider struct {
    client   *http.Client    // Graphiti REST / Bolt client
    endpoint string          // Graphiti base URL or Bolt URI
    apiKey   string          // optional auth token
}
```

- `Store(req)` POSTs to Graphiti's `add_episode` endpoint with `req.Instinct.Rule` as the body and a namespaced `group_id` derived from `req.Instinct.Scope`.
- `Query(req)` GETs / searches Graphiti's `search` endpoint with `req.Term`, applies the host's `MaxResults` / `MaxTokens` caps, and computes `EstimatedTokens` per result.
- `Forget(req)` invalidates the matching fact in the temporal graph (Graphiti's soft-delete semantics).
- `Health` checks the Graphiti endpoint's `/health`.
- `Capabilities` declares Graphiti's strengths: SemanticQuery + GraphQuery + TemporalQuery + VectorSearch + SoftDelete + NamespaceIsolation (= the v0.6.x adapter sets these to true; `TemporalQuery` is the field that distinguishes Graphiti from mem0).
- `Export` / `Import` walk Graphiti's bulk endpoints (= same YAML / JSONL shape as the sqlite reference-fallback so a Graphiti dataset can be migrated to / from any other backend).

## Namespace mapping

Graphiti calls the partition key `group_id`. The bough scope translates as:

```
schema.Scope{Level: "worktree", WorktreeID: "F-x", RepoName: "auba"}
  → graphiti group_id = repo/<sha-of-remote>/worktree/F-x/<sha-of-root>
```

(Same `<sha-of-remote>` and `<sha-of-root>` derivation as the mem0 plugin — see `docs/NAMESPACE_MAPPING.md`.)

## Local development with Graphiti

`docker-compose.yml` in this directory brings up Neo4j + Graphiti for local development. Set `OPENAI_API_KEY` in your environment before `docker compose up` — Graphiti relies on an LLM for entity / relation extraction.

```sh
cd examples/memory-plugin-graphiti-skeleton
export OPENAI_API_KEY=sk-...
docker compose up -d
```

Then `.bough.yaml`:

```yaml
instinct:
  enabled: true
  default_memory_backend: graphiti
  fallback_on_error: true

memory_backends:
  - kind: graphiti
    role: external
    endpoint: http://localhost:8000
  - kind: sqlite
    role: reference-fallback
    path: ".bough/memory/fallback.db"
```

The SQLite reference-fallback is the secondary backend the host coordinator spawns when `fallback_on_error: true` lets a Graphiti outage degrade Read to "stale but available" (round 4 AI #1 + #2 split-brain mitigation — Store / Forget / Import remain fail-fast and never fall back to SQLite).

## When the v0.6.x adapter lands

Once `bough-plugin-memory-graphiti` ships, swap the skeleton for the official binary. Existing `.bough.yaml` files do not need to change beyond updating `kind: graphiti` to point at the official adapter.
