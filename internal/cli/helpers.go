package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ikeikeikeike/bough/internal/config"

	"github.com/spf13/cobra"
)

// hookRemoveGracefulSecs mirrors newRemoveCmd's --graceful-timeout
// default for the WorktreeRemove hook dispatch, which has no flag.
const hookRemoveGracefulSecs = 10

// hookInput is the shape Claude Code's WorktreeCreate / WorktreeRemove
// hook contracts emit on stdin.
type hookInput struct {
	Name         string `json:"name"`
	Cwd          string `json:"cwd"`
	WorktreePath string `json:"worktree_path"`
}

func readHookStdin(cmd *cobra.Command) (hookInput, error) {
	raw, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return hookInput{}, fmt.Errorf("read stdin: %w", err)
	}
	if len(raw) == 0 {
		return hookInput{}, fmt.Errorf("--stdin-json was set but stdin was empty")
	}
	return parseHookInput(raw)
}

// parseHookInput decodes a WorktreeCreate / WorktreeRemove payload.
// Shared by the --stdin-json path (readHookStdin) and the unified
// `bough hook handle --event Worktree*` dispatch, which has already
// drained the payload bytes off stdin before it routes.
func parseHookInput(raw []byte) (hookInput, error) {
	var in hookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, fmt.Errorf("parse hook JSON: %w", err)
	}
	return in, nil
}

// dispatchWorktreeCreate runs the full create pipeline from a
// WorktreeCreate hook payload and prints the worktree root path to
// stdout — the contract Claude Code reads to cd into the new tree.
//
// This is the fix for the dogfood bug where `bough hook install` wires
// WorktreeCreate → `bough hook handle --event WorktreeCreate` but the
// handler's switch had no WorktreeCreate case: the hook returned exit 0
// with empty stdout, so Claude Code aborted with "hook succeeded but
// returned no worktree path" and no worktree was ever created.
func dispatchWorktreeCreate(cmd *cobra.Command, payload []byte) error {
	in, err := parseHookInput(payload)
	if err != nil {
		return err
	}
	if in.Name == "" {
		return errors.New("WorktreeCreate hook payload has no worktree name")
	}
	monorepoRoot, cfg, err := loadConfigAndRoot(cmd, in.Cwd)
	if err != nil {
		return err
	}
	return runCreate(cmd.Context(), cmd.ErrOrStderr(), cmd.OutOrStdout(), cfg, monorepoRoot, in.Name, false, false)
}

// dispatchWorktreeRemove is the WorktreeRemove twin: it tears down the
// worktree named by the payload (worktree_path preferred, else name),
// mirroring newRemoveCmd's resolution.
func dispatchWorktreeRemove(cmd *cobra.Command, payload []byte) error {
	in, err := parseHookInput(payload)
	if err != nil {
		return err
	}
	var monorepoRoot, wtName, path string
	switch {
	case in.WorktreePath != "":
		path = in.WorktreePath
		wtName = filepath.Base(path)
		monorepoRoot = filepath.Dir(filepath.Dir(path))
	case in.Name != "":
		monorepoRoot = in.Cwd
		if monorepoRoot == "" {
			monorepoRoot, _ = os.Getwd()
		}
		wtName = in.Name
		path = filepath.Join(monorepoRoot, ".worktrees", in.Name)
	default:
		return errors.New("WorktreeRemove hook payload has no worktree_path or name")
	}
	abs, cfg, err := loadConfigAndRoot(cmd, monorepoRoot)
	if err != nil {
		return err
	}
	return runRemove(cmd.Context(), cmd.ErrOrStderr(), cfg, abs, wtName, path, hookRemoveGracefulSecs)
}

// resolveConfigPath answers "where does the bough YAML live?" in the
// standard order:
//
//  1. explicit --config FLAG
//  2. <monorepoRoot>/.bough.yaml (v0.4 canonical)
//  3. <monorepoRoot>/.worktree-isolation.yaml (v0.3 legacy; removed in v0.5)
func resolveConfigPath(cmd *cobra.Command, monorepoRoot string) string {
	if p, _ := cmd.Flags().GetString("config"); p != "" {
		return p
	}
	canonical := filepath.Join(monorepoRoot, ".bough.yaml")
	if _, err := os.Stat(canonical); err == nil {
		return canonical
	}
	legacy := filepath.Join(monorepoRoot, ".worktree-isolation.yaml")
	if _, err := os.Stat(legacy); err == nil {
		fmt.Fprintln(os.Stderr, "bough: WARNING .worktree-isolation.yaml is deprecated, rename to .bough.yaml (removed in v0.5.0)")
		return legacy
	}
	// Both absent — Load will surface the missing-file error against
	// the canonical path the operator should have created.
	return canonical
}

// loadConfigAndRoot resolves the monorepo root, the config file,
// parses the YAML, and applies `config.MonorepoRoot` as a relative-
// path override on top of the caller-supplied root.
func loadConfigAndRoot(cmd *cobra.Command, cwdHint string) (string, *config.Config, error) {
	if cwdHint == "" {
		var err error
		cwdHint, err = os.Getwd()
		if err != nil {
			return "", nil, fmt.Errorf("getwd: %w", err)
		}
	}
	abs, err := filepath.Abs(cwdHint)
	if err != nil {
		return "", nil, fmt.Errorf("abs %s: %w", cwdHint, err)
	}
	cfg, err := config.Load(resolveConfigPath(cmd, abs))
	if err != nil {
		return "", nil, err
	}
	if cfg.MonorepoRoot != "" && cfg.MonorepoRoot != "." {
		if filepath.IsAbs(cfg.MonorepoRoot) {
			abs = cfg.MonorepoRoot
		} else {
			abs = filepath.Join(abs, cfg.MonorepoRoot)
		}
	}
	return abs, cfg, nil
}

// rangeLen normalises a closed [low, high] port range into the half-
// open width the allocator wants.
func rangeLen(r [2]int) int {
	if r[1] <= r[0] {
		return 0
	}
	return r[1] - r[0] + 1
}

// engineProviderRepo returns the YAML-declared engine-provider
// repository when at least one engine is configured. Accepts both
// the v0.4 canonical role ("engine-provider") and the v0.3 alias
// ("db-provider"); removed alongside the legacy fallback in v0.5.0.
func engineProviderRepo(cfg *config.Config) *config.Repository {
	if len(cfg.Engines) == 0 {
		return nil
	}
	for i := range cfg.Repositories {
		role := cfg.Repositories[i].Role
		if role == "engine-provider" || role == "db-provider" {
			return &cfg.Repositories[i]
		}
	}
	return nil
}
