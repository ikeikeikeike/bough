// The mock_plugin binary is the in-tree go-plugin server the
// conformance suite uses to self-test itself. It implements every
// DBProvider method without launching a real container — Up binds a
// loopback listener on the requested port (so AssertReachable from
// the suite passes), Down closes it, Cleanup wipes the datadir.
//
// Two failure modes ride along, gated on BOUGH_MOCK_FAIL_MODE, so
// Λ-6.2 (invariants) can prove the suite catches them:
//
//   - "bridge-ip"      — EnvVars advertises 172.17.0.4 instead of
//     127.0.0.1, mimicking the v0.2.6 elasticsearch sniff bug.
//   - "shell-metachar" — EnvVars emits a DSN that contains `(`, `&`
//     and `$`, mimicking the v0.2.5 bash-source-aborts bug.
//
// The default (empty BOUGH_MOCK_FAIL_MODE) is the green path the
// Λ-6.1 lifecycle test relies on.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	api "github.com/ikeikeikeike/bough/plugins/db/api"

	"github.com/hashicorp/go-plugin"
)

const (
	failModeEnv      = "BOUGH_MOCK_FAIL_MODE"
	failBridgeIP     = "bridge-ip"
	failShellMeta    = "shell-metachar"
	mockPortRangeLow = 51000
	mockPortRangeHi  = 51999
	mockHost         = "127.0.0.1"
	bridgeIP         = "172.17.0.4"
)

type mockProvider struct {
	mu        sync.Mutex
	listeners map[int]net.Listener
	failMode  string
}

func newMockProvider() *mockProvider {
	return &mockProvider{
		listeners: map[int]net.Listener{},
		failMode:  os.Getenv(failModeEnv),
	}
}

func (p *mockProvider) Up(_ context.Context, req api.UpReq) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.listeners[req.Port]; exists {
		return nil // up-or-reuse: contract requires a second Up to be a no-op.
	}
	if err := os.MkdirAll(req.Datadir, 0o755); err != nil {
		return fmt.Errorf("mock: mkdir datadir: %w", err)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", mockHost, req.Port))
	if err != nil {
		return fmt.Errorf("mock: listen %s:%d: %w", mockHost, req.Port, err)
	}
	p.listeners[req.Port] = ln
	go acceptLoop(ln)
	sentinel := filepath.Join(req.Datadir, "up.sentinel")
	if err := os.WriteFile(sentinel, []byte("up"), 0o644); err != nil {
		_ = ln.Close()
		delete(p.listeners, req.Port)
		return fmt.Errorf("mock: write sentinel: %w", err)
	}
	return nil
}

func acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}
}

func (p *mockProvider) Down(_ context.Context, req api.DownReq) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ln, ok := p.listeners[req.Port]; ok {
		_ = ln.Close()
		delete(p.listeners, req.Port)
	}
	return nil
}

func (p *mockProvider) ReadyCheck(_ context.Context, _, _ int) (bool, error) {
	return true, nil
}

func (p *mockProvider) Cleanup(_ context.Context, datadir string, _ int) error {
	if datadir == "" {
		return nil
	}
	if err := os.RemoveAll(datadir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("mock: cleanup %s: %w", datadir, err)
	}
	return nil
}

func (p *mockProvider) PortRangeDefault(_ context.Context) (int, int, error) {
	return mockPortRangeLow, mockPortRangeHi, nil
}

func (p *mockProvider) EnvVars(_ context.Context, req api.EnvVarsReq) (map[string]string, error) {
	host := mockHost
	if p.failMode == failBridgeIP {
		host = bridgeIP
	}
	if p.failMode == failShellMeta {
		return map[string]string{
			"BOUGH_MOCK_DSN": fmt.Sprintf(
				"user:p$(whoami)@tcp(%s:%d)/db?parseTime=true&loc=UTC",
				host, req.Port,
			),
		}, nil
	}
	return map[string]string{
		"BOUGH_MOCK_HOST": host,
		"BOUGH_MOCK_PORT": fmt.Sprintf("%d", req.Port),
		"BOUGH_MOCK_URL":  fmt.Sprintf("mock://%s:%d", host, req.Port),
	}, nil
}

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]plugin.Plugin{
			api.DBProviderPluginKey: &api.DBProviderPlugin{Impl: newMockProvider()},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
