package cli

import (
	"context"
	"testing"

	"github.com/ikeikeikeike/bough/internal/config"
)

// TestComputeEngineBackends_ExplicitBackendFieldWins is unaffected by
// the wave-3 review fixes but pins the base case: an explicit
// `backend:` YAML value must never trigger a Detect() probe.
func TestComputeEngineBackends_ExplicitBackendFieldWins(t *testing.T) {
	cfg := &config.Config{Engines: []config.Engine{
		{Kind: "mysql", Backend: "docker"},
	}}
	got := computeEngineBackends(context.Background(), cfg, map[string]bool{"mysql": true})
	if got["mysql"] != "docker" {
		t.Errorf("Backend[mysql] = %q, want %q", got["mysql"], "docker")
	}
}

// TestComputeEngineBackends_ExtrasBackendOverrideIsHonored is the
// regression guard for the wave-3 review finding: computeEngineBackends
// only checked eng.Backend, never eng.Extras["backend"] — the override
// path create.go's buildEngineExtras treats as equally authoritative
// (eng.Backend > extras["backend"] > auto-detect). An engine pinned via
// extras.backend used to be silently treated as auto-detect by status.
func TestComputeEngineBackends_ExtrasBackendOverrideIsHonored(t *testing.T) {
	cfg := &config.Config{Engines: []config.Engine{
		{Kind: "redis", Extras: map[string]string{"backend": "docker"}},
	}}
	got := computeEngineBackends(context.Background(), cfg, map[string]bool{"redis": true})
	if got["redis"] != "docker" {
		t.Errorf("Backend[redis] = %q, want %q (extras.backend override was ignored)", got["redis"], "docker")
	}
}

// TestComputeEngineBackends_SkipsUnregisteredKinds is the regression
// guard for the wave-3 review finding: computeEngineBackends used to
// call Detect() unconditionally for every auto-detect engine in the
// YAML, even ones with zero rows in the registry (e.g. no worktree
// created yet), paying probe latency status's own caller had no way
// to avoid. A kind absent from registeredKinds must never trigger
// Detect() and must be absent from the returned map.
func TestComputeEngineBackends_SkipsUnregisteredKinds(t *testing.T) {
	cfg := &config.Config{Engines: []config.Engine{
		{Kind: "mysql"}, // auto-detect, but NOT in registeredKinds below
	}}
	got := computeEngineBackends(context.Background(), cfg, map[string]bool{})
	if _, ok := got["mysql"]; ok {
		t.Errorf("Backend map contains %q for an engine kind absent from the registry: %v", "mysql", got)
	}
}

func TestEngineKindFromRegistryKey(t *testing.T) {
	cases := map[string]string{
		"mysql.main": "mysql",
		"mysql":      "mysql",
		"api":        "api",
	}
	for key, want := range cases {
		if got := engineKindFromRegistryKey(key); got != want {
			t.Errorf("engineKindFromRegistryKey(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestBuildStatus_NonEngineKindHasEmptyBackend(t *testing.T) {
	cfg := &config.Config{Engines: []config.Engine{{Kind: "mysql", Backend: "docker"}}}
	reg := map[string]map[string]int{
		"F-Test": {"mysql.main": 42000, "api": 45000},
	}
	out := buildStatus(context.Background(), reg, cfg)
	byKind := make(map[string]statusEntry, len(out))
	for _, e := range out {
		byKind[e.Kind] = e
	}
	if got := byKind["mysql.main"].Backend; got != "docker" {
		t.Errorf("mysql.main Backend = %q, want %q", got, "docker")
	}
	if got := byKind["api"].Backend; got != "" {
		t.Errorf("api (non-engine kind) Backend = %q, want empty", got)
	}
}
