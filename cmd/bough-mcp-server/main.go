// Command bough-mcp-server exposes bough's memory subsystem to MCP
// clients (Claude Desktop, Cursor, etc.) over stdio JSON-RPC per the
// MCP 2025-11-25 spec.
//
// v0.6.1 surface (= read-only by default, write opt-in):
//
//   - memory.query                Tool      (always read-only)
//   - memory.store / .forget      Tools     (require --allow-write)
//   - memory.promote              refused   (= needs the host
//     coordinator; v0.7)
//   - bough://memory/scopes       Resource  (read-only)
//
// The --allow-write flag unlocks the two state-changing Tools so an
// MCP client (Claude Desktop, Cursor) can persist or retire rules
// from the same stdio surface that already serves memory.query. The
// host writes every new row with state=candidate; promotion to
// active still requires `bough instinct approve <id>`. memory.promote
// stays refused even with --allow-write because it needs the host
// coordinator (Store(target) + Forget(source) pair), which this
// server intentionally does not embed.
//
// The round 4 AI #1 zombie-process guard fires Graceful Shutdown the
// moment stdin closes so the MemoryBackend subprocess (= SQLite
// reference-fallback) never lingers and the DB file lock is
// released. See plugins/memory/sqlite/sqlite.go for the underlying
// lock semantics.
//
// Configuration is read from CLI flags + environment variables so an
// MCP client (= Claude Desktop's `mcpServers` block) can wire bough
// by setting the same env block it would for any other MCP server.
//
// CLI flags:
//
//	--allow-write             enable memory.store + memory.forget
//	                          (default: false; v0.6.0 read-only)
//
// Environment variables (optional):
//
//	BOUGH_MCP_SERVER_VERSION   reported in initialize response
//	                           (default: linker-set Version constant)
//	BOUGH_MCP_ALLOW_WRITE      "true" / "1" sets --allow-write
//	                           without a CLI argument
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/go-plugin"

	"github.com/ikeikeikeike/bough/internal/mcp"
	memapi "github.com/ikeikeikeike/bough/plugins/memory/api"
)

// Version is reported in the MCP initialize handshake. Bumped per
// release; the v0.6.1 ship commit replaces the -dev suffix.
const Version = "v0.6.1"

func main() {
	allowWriteFlag := flag.Bool("allow-write", false,
		"enable memory.store and memory.forget tools (state-changing). Off by default; v0.6 read-only first.")
	flag.Parse()
	allowWrite := *allowWriteFlag
	if !allowWrite {
		if env := strings.TrimSpace(os.Getenv("BOUGH_MCP_ALLOW_WRITE")); env != "" {
			switch strings.ToLower(env) {
			case "1", "true", "yes", "on":
				allowWrite = true
			}
		}
	}

	backend, kill, err := discoverSQLite()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bough-mcp-server: "+err.Error())
		os.Exit(1)
	}

	version := Version
	if env := os.Getenv("BOUGH_MCP_SERVER_VERSION"); env != "" {
		version = env
	}

	if allowWrite {
		fmt.Fprintln(os.Stderr, "bough-mcp-server: --allow-write enabled; memory.store and memory.forget are reachable. Rows persist with state=candidate; promote with `bough instinct approve <id>`.")
	}

	server := mcp.NewServer(backend, kill, version, allowWrite)
	if allowWrite {
		// v0.7.0 O-1.7 write hardening: append-only audit log
		// under .bough/memory/mcp_audit.jsonl, rate-limit at
		// 60 writes / minute, and refuse non-worktree scopes
		// so an MCP client cannot accidentally promote into the
		// repo or global tier without an explicit CLI action.
		// Operators wiring a multi-scope MCP surface can swap
		// these defaults out in a future v0.7.x flag rev.
		server.SetAuditLogPath(filepath.Join(".bough", "memory", "mcp_audit.jsonl"))
		server.SetRateLimit(60, time.Minute)
		server.SetAllowedScopes([]string{"worktree"})
	}

	// Round 4 AI #1 zombie-process guard lives inside Server.Run:
	// bufio.Scanner returns false when stdin closes, Run returns,
	// and the defer below invokes Shutdown which terminates the
	// MemoryBackend subprocess. Spawning a second goroutine to read
	// os.Stdin would race with Run's scanner and steal JSON-RPC
	// bytes — review #23 #2/#3 — so the watchdog stays inline.
	defer server.Shutdown()

	ctx := context.Background()
	if err := server.Run(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "bough-mcp-server: "+err.Error())
		os.Exit(1)
	}
}

// discoverSQLite spawns the bough-plugin-memory-sqlite binary as
// the v0.6 read-only MCP server's memory source. mem0 / Graphiti
// backends will land in v0.6.x once the CLI grows --backend.
func discoverSQLite() (memapi.MemoryBackend, func(), error) {
	binName := "bough-plugin-memory-sqlite"
	binPath, err := exec.LookPath(binName)
	if err != nil {
		return nil, nil, fmt.Errorf("%s not found on PATH: %w", binName, err)
	}
	cmd := exec.Command(binPath)
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  memapi.Handshake,
		Plugins:          memapi.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})
	rpc, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("gRPC dial: %w", err)
	}
	raw, err := rpc.Dispense(memapi.MemoryBackendPluginKey)
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispense: %w", err)
	}
	backend, ok := raw.(memapi.MemoryBackend)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("plugin returned %T, not MemoryBackend", raw)
	}
	return backend, func() { client.Kill() }, nil
}
