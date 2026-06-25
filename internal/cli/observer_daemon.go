package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ikeikeikeike/bough/internal/homunculus"
)

// The observer daemon is an opt-in background process that runs
// `bough observer run-once` on an interval. v0.9.0 shipped run-once
// (= the synchronous extraction pass a hook fires); v0.9.2 adds the
// daemon for operators who want continuous extraction without wiring
// SessionEnd. Default is disabled — the operator starts it explicitly.
//
// State lives under <homunculus>/projects/<id>/ so the daemon is
// per-project + survives across shells:
//
//	observer.pid   = the daemon's PID
//	observer.log   = append-only run log (= already the run-once log)
//
// We deliberately do NOT use systemd / launchd — that would bind the
// daemon to a single OS's service manager and complicate cleanup. A
// PID file + signal is portable across macOS / Linux.

func observerPidFile(layout homunculus.Layout, projectID string) string {
	return filepath.Join(layout.ProjectDir(projectID), "observer.pid")
}

// newObserverStartCmd / Stop / Status extend the `bough observer`
// namespace. They are added to newObserverCmd in observer.go.
func newObserverStartCmd() *cobra.Command {
	var (
		root     string
		interval int
	)
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the background observer daemon (opt-in; runs observer run-once on an interval)",
		Long: `bough observer start launches a background process that runs
the extraction pass every --interval seconds. It is opt-in — the
operator starts it explicitly; nothing auto-starts it. The PID lives
under the project's homunculus dir so a later "bough observer stop"
finds it across shells.

Each tick spawns the same claude --print pass "bough observer
run-once" runs, so the v0.9.0 self-DoS limiter still caps the call
rate.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ident, layout, err := resolveObserverProject(root)
			if err != nil {
				return err
			}
			if err := layout.EnsureProjectDirs(ident.ID); err != nil {
				return err
			}
			pidPath := observerPidFile(layout, ident.ID)
			if running, pid := daemonRunning(pidPath); running {
				return fmt.Errorf("observer already running (pid %d); `bough observer stop` first", pid)
			}
			// Re-exec ourselves in --daemon mode as a detached child.
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("observer start: locate self: %w", err)
			}
			child := makeDetachedCmd(exe, []string{
				"observer", "_run-daemon",
				"--root", ident.Root,
				"--interval", strconv.Itoa(interval),
			})
			if err := child.Start(); err != nil {
				return fmt.Errorf("observer start: spawn: %w", err)
			}
			if err := os.WriteFile(pidPath, []byte(strconv.Itoa(child.Process.Pid)), 0o644); err != nil {
				return fmt.Errorf("observer start: write pid: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "observer started (pid %d, interval %ds)\nlog: %s\n",
				child.Process.Pid, interval, layout.ObserverLog(ident.ID))
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "monorepo root (default: $PWD)")
	cmd.Flags().IntVar(&interval, "interval", 600, "seconds between extraction passes (>= 60 recommended)")
	return cmd
}

func newObserverStopCmd() *cobra.Command {
	var root string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the background observer daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ident, layout, err := resolveObserverProject(root)
			if err != nil {
				return err
			}
			pidPath := observerPidFile(layout, ident.ID)
			running, pid := daemonRunning(pidPath)
			if !running {
				fmt.Fprintln(cmd.OutOrStdout(), "observer not running")
				_ = os.Remove(pidPath)
				return nil
			}
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				return fmt.Errorf("observer stop: signal pid %d: %w", pid, err)
			}
			_ = os.Remove(pidPath)
			fmt.Fprintf(cmd.OutOrStdout(), "observer stopped (pid %d)\n", pid)
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "monorepo root (default: $PWD)")
	return cmd
}

func newObserverStatusCmd() *cobra.Command {
	var root string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report whether the observer daemon is running",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ident, layout, err := resolveObserverProject(root)
			if err != nil {
				return err
			}
			pidPath := observerPidFile(layout, ident.ID)
			running, pid := daemonRunning(pidPath)
			if running {
				fmt.Fprintf(cmd.OutOrStdout(), "observer running (pid %d)\nlog: %s\n", pid, layout.ObserverLog(ident.ID))
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "observer not running")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "monorepo root (default: $PWD)")
	return cmd
}

// newObserverRunDaemonCmd is the hidden inner loop the detached child
// runs. It sleeps --interval seconds, runs one extraction pass, and
// repeats until SIGTERM. Not for direct operator use.
func newObserverRunDaemonCmd() *cobra.Command {
	var (
		root     string
		interval int
	)
	cmd := &cobra.Command{
		Use:    "_run-daemon",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ident, layout, err := resolveObserverProject(root)
			if err != nil {
				return err
			}
			logPath := layout.ObserverLog(ident.ID)
			if interval < 60 {
				interval = 60
			}
			for {
				appendDaemonLog(logPath, fmt.Sprintf("tick: observer run-once (interval %ds)", interval))
				// run the extraction pass in-process by invoking the
				// run-once command path. We shell out to keep the
				// limiter / provider lifecycle identical to a manual
				// run.
				runObserverOnceQuiet(cmd, ident.Root)
				time.Sleep(time.Duration(interval) * time.Second)
			}
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "monorepo root")
	cmd.Flags().IntVar(&interval, "interval", 600, "seconds between passes")
	return cmd
}

func resolveObserverProject(root string) (homunculus.ProjectIdentity, homunculus.Layout, error) {
	cwd := root
	if cwd == "" {
		w, err := os.Getwd()
		if err != nil {
			return homunculus.ProjectIdentity{}, homunculus.Layout{}, err
		}
		cwd = w
	}
	ident, err := homunculus.DetectIdentity(cwd)
	if err != nil {
		return homunculus.ProjectIdentity{}, homunculus.Layout{}, err
	}
	return ident, homunculus.NewLayout(), nil
}

// daemonRunning reads the PID file and checks whether that process is
// alive (= signal 0). A stale PID file (process gone) reads as not
// running.
func daemonRunning(pidPath string) (bool, int) {
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return false, 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return false, 0
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false, pid
	}
	return true, pid
}

func appendDaemonLog(path, msg string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[daemon] %s\n", msg)
}
