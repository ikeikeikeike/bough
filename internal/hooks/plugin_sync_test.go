package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The Claude Code plugin (shipped from this repo's `hooks/hooks.json`)
// wires every hook event to the same dispatcher `bough hook install`
// writes into settings.json. `hooks/hooks.json` is a hand-authored
// static file, so it can silently drift from CanonicalCommand /
// AllEvents when the event set changes. These tests are the guard:
// the committed plugin manifest must mirror the canonical wiring
// exactly, in both directions.

// pluginHooksFile mirrors the top-level shape of hooks/hooks.json,
// which reuses Claude Code's settings.json hook layout: a "hooks" map
// from event name to an ordered list of matcher groups.
type pluginHooksFile struct {
	Hooks map[HookEvent][]HookGroup `json:"hooks"`
}

// pluginHooksPath resolves <repoRoot>/hooks/hooks.json from this test
// file's own location, so the test is independent of the working
// directory `go test` is invoked from.
func pluginHooksPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate repo root")
	}
	// thisFile = <repoRoot>/internal/hooks/plugin_sync_test.go
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(repoRoot, "hooks", "hooks.json")
}

// loadPluginHooks reads + parses the committed plugin hooks.json.
func loadPluginHooks(t *testing.T) map[HookEvent][]HookGroup {
	t.Helper()
	data, err := os.ReadFile(pluginHooksPath(t))
	if err != nil {
		t.Fatalf("read plugin hooks.json: %v", err)
	}
	var f pluginHooksFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse plugin hooks.json: %v", err)
	}
	return f.Hooks
}

// diffPluginHooks reports every way `got` (the parsed plugin
// hooks.json event map) diverges from the canonical wiring: a missing
// event, an event whose wired command is not CanonicalCommand(event),
// or an event present in the manifest but absent from AllEvents().
// An empty result means the manifest is an exact mirror.
func diffPluginHooks(got map[HookEvent][]HookGroup) []string {
	var problems []string
	want := make(map[HookEvent]bool, len(AllEvents()))
	for _, ev := range AllEvents() {
		want[ev] = true
		groups := got[ev]
		if len(groups) == 0 {
			problems = append(problems, fmt.Sprintf("event %s: missing from plugin hooks.json", ev))
			continue
		}
		found := false
		for _, g := range groups {
			for _, e := range g.Hooks {
				if strings.TrimSpace(e.Command) == CanonicalCommand(ev) {
					found = true
				}
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("event %s: no entry with command %q", ev, CanonicalCommand(ev)))
		}
	}
	for ev := range got {
		if !want[ev] {
			problems = append(problems, fmt.Sprintf("event %s: present in plugin hooks.json but not in AllEvents()", ev))
		}
	}
	return problems
}

// TestPluginHooksMatchCanonical is the normal-path guard: the shipped
// hooks/hooks.json must mirror CanonicalCommand for every AllEvents()
// event and declare no extras.
func TestPluginHooksMatchCanonical(t *testing.T) {
	got := loadPluginHooks(t)
	if problems := diffPluginHooks(got); len(problems) > 0 {
		t.Fatalf("plugin hooks.json drifted from the canonical wiring:\n  %s", strings.Join(problems, "\n  "))
	}
}

// TestDiffPluginHooksDetectsDrift is the failure-path guard: a
// manifest that is missing an event, wires a wrong command, or adds an
// unknown event must be reported. If diffPluginHooks ever went silent,
// the normal-path test above would rot into a rubber stamp.
func TestDiffPluginHooksDetectsDrift(t *testing.T) {
	canonical := func() map[HookEvent][]HookGroup {
		m := map[HookEvent][]HookGroup{}
		for _, ev := range AllEvents() {
			m[ev] = []HookGroup{{Hooks: []HookEntry{{Type: "command", Command: CanonicalCommand(ev)}}}}
		}
		return m
	}

	t.Run("missing event", func(t *testing.T) {
		m := canonical()
		delete(m, EventPreCompact)
		if len(diffPluginHooks(m)) == 0 {
			t.Fatal("expected a missing event to be reported")
		}
	})

	t.Run("wrong command", func(t *testing.T) {
		m := canonical()
		m[EventUserPromptSubmit] = []HookGroup{{Hooks: []HookEntry{{Type: "command", Command: "bough hook handle --event Bogus"}}}}
		if len(diffPluginHooks(m)) == 0 {
			t.Fatal("expected a wrong command to be reported")
		}
	})

	t.Run("unknown extra event", func(t *testing.T) {
		m := canonical()
		m["NotARealEvent"] = []HookGroup{{Hooks: []HookEntry{{Type: "command", Command: "bough hook handle --event NotARealEvent"}}}}
		if len(diffPluginHooks(m)) == 0 {
			t.Fatal("expected an unknown extra event to be reported")
		}
	})

	t.Run("canonical passes", func(t *testing.T) {
		if problems := diffPluginHooks(canonical()); len(problems) > 0 {
			t.Fatalf("a canonical map must not be flagged: %v", problems)
		}
	})
}
