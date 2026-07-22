package cli

import (
	"strings"
	"testing"

	"github.com/ikeikeikeike/bough/internal/config"
	"github.com/ikeikeikeike/bough/internal/termio"
)

// TestObserverAutostartInterval pins the cadence resolution: an unset or
// sub-floor interval_sec falls back to the 10-minute default, a value at
// or above the 60s floor is used verbatim.
func TestObserverAutostartInterval(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"unset defaults to 10min", 0, 600},
		{"below floor defaults", 59, 600},
		{"at floor kept", 60, 60},
		{"explicit kept", 1800, 1800},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Instinct.Observer.IntervalSec = tc.in
			if got := observerAutostartInterval(cfg); got != tc.want {
				t.Errorf("observerAutostartInterval(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestObserverAutostartLine covers the three doctor postures, and the
// invariant that an OFF autostart never claims the daemon is running even
// when one happens to be up from a manual start.
func TestObserverAutostartLine(t *testing.T) {
	if _, got := observerAutostartLine(true, true); !strings.Contains(got, "daemon running") {
		t.Errorf("autostart ON + running should report running: %q", got)
	}
	if _, got := observerAutostartLine(true, false); !strings.Contains(got, "not running yet") {
		t.Errorf("autostart ON + idle should report not-running: %q", got)
	}
	if _, got := observerAutostartLine(false, false); !strings.Contains(got, "autostart OFF") {
		t.Errorf("autostart OFF should report OFF: %q", got)
	}
	if _, got := observerAutostartLine(false, true); strings.Contains(got, "daemon running") {
		t.Errorf("autostart OFF must not claim the daemon is running: %q", got)
	}
	// On+running is the only OK state; the two idle/off states are neutral.
	if s, _ := observerAutostartLine(true, true); s != termio.StatusOK {
		t.Errorf("autostart ON + running should be OK, got %v", s)
	}
	if s, _ := observerAutostartLine(false, false); s != termio.StatusNeutral {
		t.Errorf("autostart OFF should be neutral, got %v", s)
	}
}
