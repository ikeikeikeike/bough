package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ikeikeikeike/bough/internal/config"
	"github.com/ikeikeikeike/bough/internal/registry"
	"github.com/spf13/cobra"
)

func newBackfillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Register pre-existing worktrees into the port registry without re-launching anything",
		Long: `backfill walks the monorepo's worktrees/ (or the pre-v0.11 hidden
.worktrees/) looking for directories that resemble bough worktrees and
registers them in the port registry so subsequent allocations don't
accidentally re-use the same port. The engine is not restarted —
pre-existing .env.local files keep their port assignments.

Use this after upgrading to bough from a hand-rolled hook, or after a
registry corruption recovered from ~/.bough/backups/. Subsequent
` + "`bough create <name>`" + ` calls remain deterministic against the freshly-
written registry.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			monorepoRoot, cfg, err := loadConfigAndRoot(cmd, "")
			if err != nil {
				return err
			}
			identityRoot, err := resolveIdentityRoot("")
			if err != nil {
				return err
			}
			return runBackfill(cmd.ErrOrStderr(), cfg, monorepoRoot, identityRoot)
		},
	}
	return cmd
}

// runBackfill walks the monorepo's worktrees dir, adds any unregistered
// name to the registry with an empty entry, and relinks every discovered
// worktree's project-scoped .claude/{skills,agents,commands} symlinks. The
// follow-up `bough create <name>` is what actually allocates ports —
// backfill alone is the "stop allocator from re-issuing this name's slot"
// pass, plus a non-destructive repair for worktrees that predate the
// project-scope evolved-artifact move (only `bough create` wired
// linkWorktreeArtifacts, so a hand-created / pre-v0.9.20 / already-backfilled
// worktree never got the link and silently loaded zero evolved artifacts —
// #61). The relink runs for every worktree dir, not just newly-registered
// ones, since ensureSymlink is idempotent and a no-op on an already-correct
// link.
func runBackfill(stderr io.Writer, cfg *config.Config, monorepoRoot, identityRoot string) error {
	root := worktreesDir(monorepoRoot)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stderr, "[backfill] %s does not exist — nothing to do\n", root)
			return nil
		}
		return fmt.Errorf("read %s: %w", root, err)
	}
	store := registry.NewStore(
		resolveRegistryPath(monorepoRoot, cfg.Registry.Path),
		cfg.Registry.BackupDir,
	)
	reg, err := store.Load()
	if err != nil {
		return err
	}
	added := 0
	relinked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		linkWorktreeArtifacts(stderr, identityRoot, filepath.Join(root, name))
		relinked++
		if _, exists := reg[name]; exists {
			continue
		}
		reg[name] = map[string]int{}
		added++
	}
	if relinked > 0 {
		fmt.Fprintf(stderr, "[backfill] relinked project-scoped .claude artifacts for %d worktree dir(s)\n", relinked)
	}
	if added == 0 {
		fmt.Fprintln(stderr, "[backfill] all worktree dirs already registered — no changes")
		return nil
	}
	if err := store.Save(reg, "backfill"); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "[backfill] registered %d worktree dir(s) (run `bough create <name>` to allocate ports)\n", added)
	return nil
}
