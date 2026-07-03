package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikeikeikeike/bough/internal/config"
	"github.com/ikeikeikeike/bough/internal/registry"
)

// setupVerifyFixture creates <root>/.worktrees/<name> (runVerify's
// first existence check) and writes reg to the canonical
// .bough-ports.json registry path, returning root.
func setupVerifyFixture(t *testing.T, name string, reg registry.Registry) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".worktrees", name), 0o755); err != nil {
		t.Fatalf("mkdir worktree dir: %v", err)
	}
	store := registry.NewStore(filepath.Join(root, registry.CanonicalPath), "")
	if err := store.Save(reg, "test-fixture"); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	return root
}

// TestRunVerify_NonEnginePortDottedKey is the regression guard for the
// #17-review finding: create.go's allocateNonEnginePorts writes
// non-engine ports (cfg.Ports, e.g. "api"/"gateway") under the
// composite key "<kind>.main" — the same convention as engine ports —
// but verify's own check still looked up the bare kind ("api"), which
// registry.Load's legacy-key upgrade guarantees never exists post-v0.4.
// A worktree bough create just wrote cleanly must not fail verify.
func TestRunVerify_NonEnginePortDottedKey(t *testing.T) {
	root := setupVerifyFixture(t, "F-Foo", registry.Registry{
		"F-Foo": {"api.main": 45123, "gateway.main": 48123},
	})
	cfg := &config.Config{
		Registry: config.RegistryConfig{Path: ".bough-ports.json"},
		Ports: map[string]config.PortRange{
			"api":     {Range: [2]int{45000, 47999}},
			"gateway": {Range: [2]int{48000, 50099}},
		},
	}
	var stderr, stdout bytes.Buffer
	if err := runVerify(&stderr, &stdout, cfg, root, "F-Foo"); err != nil {
		t.Errorf("runVerify reported DRIFT on a cleanly-written registry: %v\nstderr:\n%s", err, stderr.String())
	}
}

// TestRunVerify_EngineMainRoleOnly is the regression guard for the
// #17-review finding: verify used to demand a registry entry for
// EVERY role an engine's port_ranges declared, but allocateEngines
// (create.go) only ever allocates/writes "main" (v0.4.0: single-port
// engines only). A multi-role port_ranges declaration — valid per
// config validation, and the exact shape docs/PLUGIN_AUTHOR_GUIDE.md
// shows for future multi-port plugins — must not report permanent
// DRIFT against a key nothing will ever populate.
func TestRunVerify_EngineMainRoleOnly(t *testing.T) {
	root := setupVerifyFixture(t, "F-Foo", registry.Registry{
		"F-Foo": {"rabbitmq.main": 60123},
	})
	cfg := &config.Config{
		Registry: config.RegistryConfig{Path: ".bough-ports.json"},
		Engines: []config.Engine{
			{
				Kind: "rabbitmq",
				PortRanges: map[string][2]int{
					"main":       {60000, 60999},
					"management": {61000, 61999}, // never allocated by create.go today
				},
			},
		},
	}
	var stderr, stdout bytes.Buffer
	if err := runVerify(&stderr, &stdout, cfg, root, "F-Foo"); err != nil {
		t.Errorf("runVerify reported DRIFT for an undeclared-by-design 'management' role: %v\nstderr:\n%s", err, stderr.String())
	}
}

// TestRunVerify_UsesCanonicalRegistryPath is the regression guard for
// the #17-review finding: create/remove resolve the registry file via
// resolveRegistryPath (preferring .bough-ports.json when present),
// but verify (and status/list/backfill) built their Store straight
// from cfg.Registry.Path — so an operator who has renamed the on-disk
// file to .bough-ports.json without also editing a still-v0.3 YAML
// (docs/MIGRATION-v0.3-to-v0.4.md explicitly says this is fine to
// leave "at your convenience") got a stale-path "no registry entry"
// failure against a worktree create/remove see just fine.
func TestRunVerify_UsesCanonicalRegistryPath(t *testing.T) {
	root := setupVerifyFixture(t, "F-Foo", registry.Registry{
		"F-Foo": {},
	})
	cfg := &config.Config{
		// Stale v0.3 YAML value — the canonical .bough-ports.json this
		// fixture actually wrote to exists at root, and must win.
		Registry: config.RegistryConfig{Path: ".worktree-ports.json"},
	}
	var stderr, stdout bytes.Buffer
	if err := runVerify(&stderr, &stdout, cfg, root, "F-Foo"); err != nil {
		t.Errorf("runVerify did not find the canonical registry despite a stale YAML path: %v\nstderr:\n%s", err, stderr.String())
	}
}
