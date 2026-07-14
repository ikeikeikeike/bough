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
