# Attic

Design docs for the v0.5-v0.8 memory-orchestration surface
(`MemoryBackend` / `InstinctMinter` plugin contracts, the mem0 /
Graphiti backends, the CapabilityCompiler, `bough-mcp-server`,
evaluator adapters). v0.9.0 superseded that surface wholesale with
the ECC-verbatim continuous-learning port described in the top-level
README — there is no plan to build these backends. The docs stay
here as design history rather than in git log alone; pin **v0.8.1**
if you need the working implementation they describe.

- [BACKENDS.md](BACKENDS.md)
- [CAPABILITY_COMPILER.md](CAPABILITY_COMPILER.md)
- [CONCEPTS.md](CONCEPTS.md) — the v0.5-v0.8 three-layer model (Layer A memory backends / Layer B skill execution / Layer C `CapabilityCompiler`). Superseded along with the rest of this surface; the top-level README describes the current architecture directly.
- [EXTERNAL_MEMORY_BACKENDS.md](EXTERNAL_MEMORY_BACKENDS.md)
- [INSTINCTS.md](INSTINCTS.md) — the v0.5-v0.8 instinct subsystem (`memory_backends:` config, `bough instinct mint/approve/promote/query/forget`). The current `bough instinct status/list/show` (v0.9+, homunculus-backed) is a different, read-only command under the same name — see the top-level README's "Continuous learning" section instead.
- [MCP_SERVER.md](MCP_SERVER.md)
- [MEMORY_PLUGIN_AUTHOR_GUIDE.md](MEMORY_PLUGIN_AUTHOR_GUIDE.md)
- [NAMESPACE_MAPPING.md](NAMESPACE_MAPPING.md)
