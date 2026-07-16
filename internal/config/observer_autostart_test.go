package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestInstinctObserverAutostartParses pins the YAML tags for the
// autostart opt-in: instinct.observer.autostart / .interval_sec parse,
// and an absent observer block leaves the cost-safe defaults (off / 0)
// so bough never starts the minting daemon unless the operator asks.
func TestInstinctObserverAutostartParses(t *testing.T) {
	var ic InstinctConfig
	src := "observer:\n  autostart: true\n  interval_sec: 300\n"
	if err := yaml.Unmarshal([]byte(src), &ic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !ic.Observer.Autostart {
		t.Errorf("Observer.Autostart = false, want true")
	}
	if ic.Observer.IntervalSec != 300 {
		t.Errorf("Observer.IntervalSec = %d, want 300", ic.Observer.IntervalSec)
	}

	var empty InstinctConfig
	if err := yaml.Unmarshal([]byte("enabled: true\n"), &empty); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if empty.Observer.Autostart {
		t.Errorf("absent observer block: Autostart = true, want false (cost-safe default)")
	}
	if empty.Observer.IntervalSec != 0 {
		t.Errorf("absent observer block: IntervalSec = %d, want 0", empty.Observer.IntervalSec)
	}
}

// TestIntervalSecBelowFloorDoesNotRejectWholeConfig is the regression
// test for a validator/floor contradiction this retrospective review
// found: IntervalSec originally carried `validate:"omitempty,min=60"`,
// but internal/cli/hook.go's observerAutostartInterval already floors
// any sub-60 value to the 10-minute default at read time. Because
// Config.Validate() validates the WHOLE struct in one pass, that tag
// made ANY interval_sec below 60 — a typo, or someone reading "seconds"
// too literally — reject the ENTIRE .bough.yaml, silently breaking every
// unrelated feature sharing the same config load (autostart itself, plus
// e.g. quality gates) instead of just gracefully degrading to the
// default the runtime code was already designed to apply.
func TestIntervalSecBelowFloorDoesNotRejectWholeConfig(t *testing.T) {
	src := []byte(`
schema_version: 1
monorepo_root: "."
repositories:
  - {name: a, branch_strategy: develop}
registry: {path: .worktree-ports.json}
instinct:
  observer:
    autostart: true
    interval_sec: 30
`)
	c, err := LoadFromBytes(src, "test")
	if err != nil {
		t.Fatalf("LoadFromBytes with a sub-floor interval_sec must not reject the whole config: %v", err)
	}
	if !c.Instinct.Observer.Autostart {
		t.Errorf("Autostart = false, want true (unrelated to the sub-floor interval_sec)")
	}
	if c.Instinct.Observer.IntervalSec != 30 {
		t.Errorf("IntervalSec = %d, want the raw 30 preserved (flooring is the caller's job, not the parser's)", c.Instinct.Observer.IntervalSec)
	}
}
