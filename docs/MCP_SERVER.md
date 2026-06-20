# bough-mcp-server (v0.6.0)

`bough-mcp-server` is a companion binary that exposes bough's
memory subsystem to MCP clients (Claude Desktop, Cursor, etc.) over
stdio JSON-RPC 2.0. It speaks the MCP 2025-11-25 spec
(https://modelcontextprotocol.io/specification/2025-11-25).

## Read-only first (round 4 AI #2)

v0.6.0 ships a deliberately narrow surface:

- **Tools**: `memory.query` (read-only)
- **Resources**: `bough://memory/scopes`
- **Prompts**: none (lands in v0.6.x)

State-changing tools (`memory.store`, `memory.forget`,
`memory.promote`) refuse with JSON-RPC error code `-32001`
(`codeWriteForbidden`). The v0.6.x CLI adds an `--allow-write`
flag to flip the write surface on after the operator has confirmed
the audit log + MCP caller pinning story.

The server's capabilities advertise the policy so clients can
probe it before the first tool call:

```jsonc
{
  "capabilities": {
    "bough_mcp_server": {
      "spec_version": "2025-11-25",
      "read_only": true,
      "state_changing_tools": false,
      "host_version": "v0.6.0"
    }
  }
}
```

## Lifecycle (round 4 AI #1 zombie guard)

The server runs a watchdog goroutine that fires Graceful Shutdown
the moment `os.Stdin` closes. The MemoryBackend subprocess
(`bough-plugin-memory-sqlite` in v0.6.0) is killed as part of the
shutdown so SQLite file locks never linger after a Claude Desktop
restart.

`Shutdown` is idempotent — calling it twice is safe. Post-shutdown
dispatches receive a JSON-RPC internal-error response so the host
drains cleanly.

## Wiring into Claude Desktop

```json
{
  "mcpServers": {
    "bough": {
      "command": "bough-mcp-server"
    }
  }
}
```

The server reads no required environment variables; the
`bough-plugin-memory-sqlite` binary it spawns picks up its database
path from `BOUGH_MEMORY_SQLITE_PATH` (default
`$TMPDIR/bough-memory-sqlite.db`).

Set `BOUGH_MCP_SERVER_VERSION` if you want the `initialize`
response to advertise a different version string than the compiled
default.

## Spec version pin

`internal/mcp/types.go` exposes `MCPSpecVersion = "2025-11-25"` as
a single const so a patch release can bump the spec when MCP
publishes an additive update. Breaking spec changes wait for v0.7.

## Conformance

`conformance/mcp/Run(t, cfg)` walks every supported method through
an in-process server + `io.Pipe`. Plugin authors building a custom
backend pass `cfg.Backend` to exercise their wire against the v0.6
read-only contract before shipping.
