package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ikeikeikeike/bough/internal/hooks"
)

// newHookCmd wires `bough hook install / uninstall / list / replay
// / doctor`. The v0.7.0 Bootstrap safety floor plan calls for hook
// auto-wire to ship alongside a replay harness on day one (= round
// 5 review insistence), so the cobra surface lands in the first
// v0.7.0 commit even though most subcommands return
// hooks.ErrNotYetWired until the body work catches up. Surfacing
// the CLI shape early lets fixture data, docs, and integration
// scripts develop in parallel rather than block on each other.
func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage Claude Code hook handlers bough writes into .claude/settings.json",
		Long: `bough hook manages the handlers an operator wires into
Claude Code's .claude/settings.json so bough's observer / bootstrap
loop fires on session lifecycle events.

The subcommands keep the JSON round-trip safe — hand-edited entries
the operator added by mouse stay put; only bough's canonical
entries get reconciled.

v0.7.0 first commit lands the cobra surface plus the
internal/hooks/ package skeleton. The Manager bodies (install /
uninstall / list / replay / doctor) wire in across the rest of the
v0.7.0 sprint per docs/ROADMAP.md.`,
	}
	cmd.AddCommand(
		newHookInstallCmd(),
		newHookUninstallCmd(),
		newHookListCmd(),
		newHookReplayCmd(),
		newHookDoctorCmd(),
		newHookHandleCmd(),
	)
	return cmd
}

func newHookInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install bough's canonical hook handlers into .claude/settings.json",
		RunE: func(c *cobra.Command, _ []string) error {
			settingsPath, err := defaultClaudeSettingsPath()
			if err != nil {
				return err
			}
			m := hooks.New(settingsPath)
			return m.Install(commandCtx(c), "bough hook handle")
		},
	}
}

func newHookUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove bough's hook handlers from .claude/settings.json",
		RunE: func(c *cobra.Command, _ []string) error {
			settingsPath, err := defaultClaudeSettingsPath()
			if err != nil {
				return err
			}
			m := hooks.New(settingsPath)
			return m.Uninstall(commandCtx(c))
		},
	}
}

func newHookListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print every hook handler currently wired in .claude/settings.json",
		RunE: func(c *cobra.Command, _ []string) error {
			settingsPath, err := defaultClaudeSettingsPath()
			if err != nil {
				return err
			}
			m := hooks.New(settingsPath)
			set, err := m.List(commandCtx(c))
			if err != nil {
				return err
			}
			if len(set) == 0 {
				fmt.Fprintf(c.OutOrStdout(), "(no hooks wired in %s)\n", settingsPath)
				return nil
			}
			for _, event := range hooks.AllEvents() {
				groups, ok := set[event]
				if !ok {
					continue
				}
				fmt.Fprintf(c.OutOrStdout(), "%s:\n", event)
				for _, g := range groups {
					matcher := g.Matcher
					if matcher == "" {
						matcher = "*"
					}
					for _, e := range g.Hooks {
						fmt.Fprintf(c.OutOrStdout(), "  - matcher=%s %s %q\n", matcher, e.Type, e.Command)
					}
				}
			}
			return nil
		},
	}
}

func newHookReplayCmd() *cobra.Command {
	var (
		event   string
		fixture string
	)
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay a fixture JSON payload through the bough hook handler for debugging",
		Long: `bough hook replay drives a recorded hook-event payload
through the bough handler so an operator can sanity-check the
wiring against a fixture file without touching a live Claude Code
session. v0.7.0 ships canonical fixtures under
internal/hooks/testdata/ that golden-test the install / handler
pair end-to-end.`,
		RunE: func(c *cobra.Command, _ []string) error {
			if event == "" {
				return fmt.Errorf("--event is required (e.g. --event PreToolUse)")
			}
			if fixture == "" {
				return fmt.Errorf("--fixture is required (= path to the JSON payload Claude Code would have sent on stdin)")
			}
			payload, err := os.ReadFile(fixture)
			if err != nil {
				return fmt.Errorf("read fixture %s: %w", fixture, err)
			}
			settingsPath, err := defaultClaudeSettingsPath()
			if err != nil {
				return err
			}
			m := hooks.New(settingsPath)
			result, err := m.Replay(commandCtx(c), hooks.HookEvent(event), payload)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(),
				"event=%s exitCode=%d\nstdout: %s\nstderr: %s\n",
				result.Event, result.ExitCode, result.Stdout, result.Stderr)
			return nil
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "hook event name (e.g. PreToolUse, PostToolUse, SessionEnd)")
	cmd.Flags().StringVar(&fixture, "fixture", "", "path to a JSON fixture file (e.g. internal/hooks/testdata/pretooluse.json)")
	return cmd
}

func newHookDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report bough's hook wiring + observer + cost posture in one place",
		Long: `bough hook doctor is the v0.7.0 transparency surface.
Round 5 review front-loaded this from v0.7.1 to v0.7.0 because
silent billing / silent observer / silent Haiku regressions are
exactly what ECC has historically struggled with and bough should
visibly avoid. Same body as the top-level "bough doctor" alias.`,
		RunE: func(c *cobra.Command, _ []string) error {
			return runDoctor(c)
		},
	}
}

// runDoctor is the shared body between `bough doctor` (= top-level
// alias) and `bough hook doctor`. Both surfaces print the same
// report so operators do not have to remember which spelling to
// use; the top-level alias matches the round 5 reviewer ask of
// having the transparency check reachable without remembering the
// `hook` namespace.
func runDoctor(c *cobra.Command) error {
	settingsPath, err := defaultClaudeSettingsPath()
	if err != nil {
		return err
	}
	m := hooks.New(settingsPath)
	report, err := m.Doctor(commandCtx(c))
	if err != nil {
		return err
	}
	report.Render(c.OutOrStdout())
	return nil
}

// newHookHandleCmd wires `bough hook handle`, the v0.7.0 O-1.6
// raw-event capture dispatcher. Claude Code invokes this command
// (one per registered hook entry, per the install layout) with
// the event name on the --event flag and the JSON payload on
// stdin; the dispatcher appends one JSONL record to
// `.bough/observations.jsonl` and exits cleanly.
//
// Hidden from the human surface because Claude Code is the only
// expected caller — wrapping it in a `bough hook` namespace lets
// `bough hook replay` reuse the same payload format for golden
// tests without colliding with operator workflows.
//
// The dispatcher intentionally does no parsing of the payload
// beyond decoding it once to validate the bytes are valid JSON;
// the observer + Bootstrap Agent (= v0.7.1) own the semantic
// analysis of what each event means. Keeping the dispatcher
// dumb means a Claude Code spec drift adds a new field without
// breaking the bough side until the analysis layer is ready to
// consume it.
func newHookHandleCmd() *cobra.Command {
	var (
		event   string
		outPath string
	)
	cmd := &cobra.Command{
		Use:    "handle",
		Hidden: true,
		Short:  "Receive a Claude Code hook event payload via stdin and append to .bough/observations.jsonl",
		RunE: func(c *cobra.Command, _ []string) error {
			if event == "" {
				return fmt.Errorf("--event is required (= called by Claude Code's settings.json wiring; see `bough hook install`)")
			}
			payload, err := io.ReadAll(c.InOrStdin())
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			// Validate the payload is JSON so a malformed Claude
			// Code event surfaces as a hook failure instead of
			// silently appending garbage to the log. We hold the
			// raw bytes through so downstream tooling can decode
			// fields bough does not yet know about.
			if len(payload) > 0 {
				var probe map[string]any
				if err := json.Unmarshal(payload, &probe); err != nil {
					return fmt.Errorf("payload is not valid JSON: %w", err)
				}
			}
			if outPath == "" {
				outPath = filepath.Join(".bough", "observations.jsonl")
			}
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", filepath.Dir(outPath), err)
			}
			record := struct {
				TS      string          `json:"ts"`
				Event   string          `json:"event"`
				Payload json.RawMessage `json:"payload"`
			}{
				TS:      time.Now().UTC().Format(time.RFC3339Nano),
				Event:   event,
				Payload: json.RawMessage(payload),
			}
			if len(record.Payload) == 0 {
				record.Payload = json.RawMessage(`null`)
			}
			line, err := json.Marshal(record)
			if err != nil {
				return fmt.Errorf("marshal observation: %w", err)
			}
			f, err := os.OpenFile(outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("open %s: %w", outPath, err)
			}
			defer f.Close()
			if _, err := f.Write(append(line, '\n')); err != nil {
				return fmt.Errorf("append %s: %w", outPath, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "Claude Code hook event name (e.g. PreToolUse)")
	cmd.Flags().StringVar(&outPath, "out", "", "observation log path (default: .bough/observations.jsonl)")
	return cmd
}

// defaultClaudeSettingsPath returns the per-project .claude/
// settings.json bough manages. v0.7.0 anchors against the CLI's
// current working directory so each monorepo gets an isolated hook
// wiring; v0.7.x adds --scope=user / --scope=project flags to
// reach the global surface explicitly.
func defaultClaudeSettingsPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	return filepath.Join(cwd, ".claude", "settings.json"), nil
}

// commandCtx returns the cobra command's context or background
// when the host did not propagate one. cobra >= v1.7 always sets
// the context, but the fallback keeps the surface safe across
// shim invocations the test harness might run in.
func commandCtx(c *cobra.Command) context.Context {
	if c == nil {
		return context.Background()
	}
	ctx := c.Context()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
