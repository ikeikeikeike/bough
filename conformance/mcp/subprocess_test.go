package mcp_test

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/mcp"
)

// TestSubprocessLifecycle exercises the production stdio path the
// in-process conformance suite cannot reach. Run drives Server.Run
// directly through an io.Pipe, so review #23 #2/#3 (the WatchStdin
// + Run race on os.Stdin) shipped CI-green even though Claude
// Desktop / Cursor would have seen JSON-RPC frame corruption.
//
// This test spawns the real bough-mcp-server binary, exchanges the
// MCP initialize handshake over the actual stdio surface, closes
// stdin, and asserts the server exits 0 within a short bound. A
// zombie regression (= server reading os.Stdin from a second
// goroutine, or missing the EOF return path) would either steal
// the initialize bytes (= no response → ReadBytes timeout) or fail
// to shutdown (= cmd.Wait times out and the assertion fires).
//
// The test builds both binaries (bough-mcp-server +
// bough-plugin-memory-sqlite) into a shared temp dir so the
// server's discoverSQLite (= exec.LookPath) finds the plugin
// without polluting the operator's PATH.
func TestSubprocessLifecycle(t *testing.T) {
	bins := buildSubprocessBinaries(t)
	defer bins.cleanup()

	dbPath := filepath.Join(bins.dir, "subprocess.db")
	cmd := exec.Command(bins.mcpServer)
	cmd.Env = append(os.Environ(),
		"PATH="+bins.dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"BOUGH_MEMORY_SQLITE_PATH="+dbPath,
	)
	stderr, _ := cmd.StderrPipe()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp server: %v", err)
	}

	// initialize handshake: writes one JSON-RPC frame, reads exactly
	// one back. A WatchStdin-style second reader on os.Stdin would
	// steal the bytes and starve the bufio.Scanner inside Run.
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}
	raw, _ := json.Marshal(req)
	if _, err := stdin.Write(append(raw, '\n')); err != nil {
		t.Fatalf("write initialize: %v", err)
	}

	reader := bufio.NewReader(stdout)
	line, err := readWithTimeout(reader, 10*time.Second)
	if err != nil {
		drainStderr(t, stderr)
		_ = cmd.Process.Kill()
		t.Fatalf("read initialize response: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal initialize response: %v: %q", err, string(line))
	}
	if resp["error"] != nil {
		t.Fatalf("initialize returned error: %+v", resp["error"])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize result missing: %+v", resp)
	}
	if result["protocolVersion"] != mcp.MCPSpecVersion {
		t.Errorf("protocolVersion: %v want %v", result["protocolVersion"], mcp.MCPSpecVersion)
	}
	caps, _ := result["capabilities"].(map[string]any)
	vendor, _ := caps["bough_mcp_server"].(map[string]any)
	if vendor["read_only"] != true {
		t.Errorf("subprocess advertises read_only=%v want true", vendor["read_only"])
	}

	// Close stdin → Server.Run's bufio.Scanner returns false → main
	// returns → defer server.Shutdown() reaps the SQLite plugin. The
	// pre-fix WatchStdin code closed inside the watcher goroutine and
	// the SQLite subprocess outlived the server; the timeout below is
	// the regression backstop.
	if err := stdin.Close(); err != nil {
		t.Errorf("stdin close: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			drainStderr(t, stderr)
			t.Errorf("server exited non-zero after stdin close: %v", err)
		}
	case <-time.After(10 * time.Second):
		drainStderr(t, stderr)
		_ = cmd.Process.Kill()
		t.Fatalf("server did not exit within 10s of stdin close (= zombie regression)")
	}
}

// subprocessBinaries holds the compiled artifacts plus a cleanup
// hook so a test failure does not leak the temp directory.
type subprocessBinaries struct {
	dir       string
	mcpServer string
	cleanup   func()
}

func buildSubprocessBinaries(t *testing.T) subprocessBinaries {
	t.Helper()
	dir, err := os.MkdirTemp("", "bough-mcp-subproc-*")
	if err != nil {
		t.Fatalf("mktempdir: %v", err)
	}
	repoRoot := findRepoRoot(t)
	type buildSpec struct {
		out string
		pkg string
	}
	specs := []buildSpec{
		{filepath.Join(dir, "bough-plugin-memory-sqlite"), "./cmd/bough-plugin-memory-sqlite"},
		{filepath.Join(dir, "bough-mcp-server"), "./cmd/bough-mcp-server"},
	}
	for _, b := range specs {
		buildCmd := exec.Command("go", "build", "-o", b.out, b.pkg)
		buildCmd.Dir = repoRoot
		buildCmd.Env = os.Environ()
		out, err := buildCmd.CombinedOutput()
		if err != nil {
			_ = os.RemoveAll(dir)
			t.Fatalf("build %s: %v\n%s", b.pkg, err, out)
		}
	}
	return subprocessBinaries{
		dir:       dir,
		mcpServer: specs[1].out,
		cleanup:   func() { _ = os.RemoveAll(dir) },
	}
}

// findRepoRoot resolves the bough module root so `go build` runs
// from the right working directory regardless of where `go test`
// stashed the test binary. We use `go env GOMOD` rather than walking
// upward for .git so the test still works in an exported tarball.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" {
		t.Fatalf("go env GOMOD empty — not in a module")
	}
	return filepath.Dir(mod)
}

// readWithTimeout wraps bufio.Reader.ReadBytes with a deadline so a
// hung subprocess does not stall CI indefinitely.
func readWithTimeout(r *bufio.Reader, d time.Duration) ([]byte, error) {
	type result struct {
		line []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := r.ReadBytes('\n')
		ch <- result{line, err}
	}()
	select {
	case res := <-ch:
		return res.line, res.err
	case <-time.After(d):
		return nil, io.ErrNoProgress
	}
}

// drainStderr surfaces the server's stderr to the test log so a
// failure carries diagnostic context instead of a bare timeout.
func drainStderr(t *testing.T, r io.Reader) {
	t.Helper()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if n > 0 {
		t.Logf("mcp-server stderr: %s", string(buf[:n]))
	}
}
