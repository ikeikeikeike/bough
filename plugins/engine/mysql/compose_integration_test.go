//go:build conformance

// End-to-end test of the mysql plugin's `backend: compose` path against a
// real docker + docker-compose CLI. Drives the public Provider API exactly
// as the host does — Up (adopts a fixture compose file, retargets the db
// service's port to a bough-allocated one) → ReadyCheck → a raw host-TCP
// handshake → Down → Cleanup — and asserts the per-worktree compose
// project is gone (containers + named volume) at the end.
//
// Build-tagged `conformance` so plain `go test ./...` never needs docker.
package mysql_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	api "github.com/ikeikeikeike/bough/plugins/engine/api"
	"github.com/ikeikeikeike/bough/pkg/composeutil"
	"github.com/ikeikeikeike/bough/plugins/engine/mysql"
)

const composeFixture = `services:
  db:
    image: mysql:8.4
    volumes:
      - dbstore:/var/lib/mysql
    ports:
      - "3306:3306"
    environment:
      - MYSQL_ALLOW_EMPTY_PASSWORD=1
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "127.0.0.1"]
      interval: 5s
      timeout: 3s
      retries: 40
      start_period: 10s
volumes:
  dbstore:
`

func TestComposeBackend_EndToEnd(t *testing.T) {
	if os.Getenv(mysqlConformancePluginEnv) == "" {
		t.Skipf("set %s to run the compose-backend integration test", mysqlConformancePluginEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mysqlConformanceReadyMax)
	defer cancel()

	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(composeFile, []byte(composeFixture), 0o644); err != nil {
		t.Fatalf("write fixture compose: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	extras := map[string]string{
		"backend":             "compose",
		"compose.file":        composeFile, // absolute (host normally resolves this)
		"compose.service":     "db",
		"compose.target_port": "3306",
	}
	req := &api.UpReq{
		Ports:   []api.PortSpec{{Role: "main", Port: port}},
		Datadir: filepath.Join(dir, ".local", "mysql-data"),
		Extras:  extras,
	}
	project := composeutil.Project("mysql", port)

	p := mysql.New()
	if err := p.Up(ctx, req); err != nil {
		t.Fatalf("Up (compose): %v", err)
	}
	// Always tear the project down, even on a mid-test failure.
	defer func() {
		_ = p.Down(context.Background(), &api.DownReq{Ports: []int{port}, GracefulTimeoutSec: 30})
		_ = p.Cleanup(context.Background(), req.Datadir, []int{port})
	}()

	ready, err := p.ReadyCheck(ctx, []int{port}, int(mysqlConformanceReadyMax.Seconds()))
	if err != nil || !ready {
		t.Fatalf("ReadyCheck (compose): ready=%v err=%v", ready, err)
	}

	// The whole point: the fixture's hardcoded 3306 was retargeted to the
	// bough-allocated host port, bound on 127.0.0.1, and reachable now.
	if !mysqlHandshakeOK(ctx, fmt.Sprintf("127.0.0.1:%d", port)) {
		t.Fatalf("no mysql handshake on the bough-allocated port %d after ReadyCheck=true", port)
	}

	// Down removes the containers; the project must no longer be running.
	if err := p.Down(ctx, &api.DownReq{Ports: []int{port}, GracefulTimeoutSec: 30}); err != nil {
		t.Fatalf("Down (compose): %v", err)
	}
	if composeutil.Running(ctx, project) {
		t.Fatalf("compose project %s still running after Down", project)
	}

	// Cleanup removes the named volume (the compose datadir).
	if err := p.Cleanup(ctx, req.Datadir, []int{port}); err != nil {
		t.Fatalf("Cleanup (compose): %v", err)
	}
}

// mysqlHandshakeOK does one raw MySQL Initial-Handshake read (protocol
// version 0x0a), reusing the same cheap stdlib probe shape as the main
// conformance suite — no client library needed.
func mysqlHandshakeOK(ctx context.Context, hostPort string) bool {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", hostPort)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var buf [5]byte
	if _, err := readFull(conn, buf[:]); err != nil {
		return false
	}
	return buf[4] == 0x0a
}

func readFull(c net.Conn, buf []byte) (int, error) {
	got := 0
	for got < len(buf) {
		n, err := c.Read(buf[got:])
		got += n
		if err != nil {
			return got, err
		}
	}
	return got, nil
}
