package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ikeikeikeike/bough/internal/ecc"
)

// newEccCmd wires `bough ecc import` — the v0.7.2 ECC compat
// bridge. v0.7.0 / v0.7.1 ship the safety floor + LLM judge;
// v0.7.2 surfaces existing ECC corpora (= the threecorp + upstream
// dogfood inventory) so an operator can promote 300+ pre-existing
// instincts into bough without re-running them through the evolve
// pipeline.
//
// `bough ecc import --dry-run` (default) parses + projects the
// corpus and writes a Markdown manifest under .bough/ecc-imports/
// <ts>/ so the operator can git-diff what would land before any
// row reaches the memory backend. `--apply` actually writes the
// rows.
func newEccCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ecc",
		Short: "Interoperate with affaan-m/everything-claude-code corpora",
		Long: `bough ecc bridges the upstream ECC homunculus corpus into bough's
canonical schemas. v0.7.2 ships the read-side (= bough ecc import)
which walks ~/.local/share/ecc-homunculus/ and projects each
instinct / skill / agent / command onto pkg/schema types ready for
memory backend Store or capability compile.`,
	}
	cmd.AddCommand(newEccImportCmd())
	return cmd
}

func newEccImportCmd() *cobra.Command {
	var (
		root      string
		outDir    string
		kindsRaw  string
		dryRun    bool
		monorepo  string
		writeJSON bool
	)
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Walk an ECC corpus and project its artifacts onto bough schemas",
		Long: `bough ecc import reads an ECC homunculus corpus (default:
~/.local/share/ecc-homunculus) and writes a Markdown manifest of
what would be migrated. Use --apply to actually persist the rows
(= v0.7.x lands the backend write loop; for now --apply behaves
like dry-run plus prints the would-write counts).

--kinds selects which artifact families to import. Comma-separated.
Default: instinct,skill,agent,command (= all four).`,
		RunE: func(c *cobra.Command, _ []string) error {
			kinds := parseKinds(kindsRaw)
			corp, err := ecc.Discover(root, kinds)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			res := ecc.Migrate(corp, ecc.MigrateOptions{
				NowFn:        func() time.Time { return now },
				MonorepoName: monorepo,
			})
			ts := now.Format("20060102T150405Z")
			target := filepath.Join(outDir, ts)
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
			manifest := renderManifest(corp, res, dryRun, target)
			manifestPath := filepath.Join(target, "_manifest.md")
			if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
				return fmt.Errorf("write manifest %s: %w", manifestPath, err)
			}
			fmt.Fprintf(c.OutOrStdout(), "wrote %s\n", manifestPath)
			fmt.Fprintf(c.OutOrStdout(),
				"discovered: instincts=%d skills=%d agents=%d commands=%d errors=%d\n",
				len(corp.Instincts), len(corp.Skills), len(corp.Agents), len(corp.Commands), len(corp.Errors))
			fmt.Fprintf(c.OutOrStdout(),
				"migrated:   instincts=%d skills=%d agents=%d commands=%d (skipped instincts=%d)\n",
				len(res.InstinctCandidates), len(res.SkillArtifacts), len(res.AgentArtifacts), len(res.CommandArtifacts),
				res.SkippedInstincts)
			if writeJSON {
				if err := writeJSONOutputs(target, res); err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "wrote JSON outputs under %s\n", target)
			}
			if !dryRun {
				fmt.Fprintf(c.OutOrStdout(),
					"NOTE: --apply backend persistence wires in v0.7.x (= P4 memory backend write loop). For v0.7.2 the JSON manifests + counts are the contract.\n")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", "~/.local/share/ecc-homunculus", "ECC corpus root (`~` resolves to $HOME)")
	cmd.Flags().StringVar(&outDir, "out-dir", ".bough/ecc-imports", "parent directory for the per-run manifest")
	cmd.Flags().StringVar(&kindsRaw, "kinds", "instinct,skill,agent,command", "comma-separated artifact kinds to import")
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "write manifest + counts only; do not persist to backend (default true in v0.7.2)")
	cmd.Flags().StringVar(&monorepo, "monorepo-name", "", "fallback Scope.RepoName when an ECC instinct's project_name is empty")
	cmd.Flags().BoolVar(&writeJSON, "json", false, "also write the projected schema rows as JSON under <out>/<kind>.json")
	return cmd
}

func parseKinds(raw string) []ecc.ArtifactKind {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := []ecc.ArtifactKind{}
	for _, k := range strings.Split(raw, ",") {
		kk := strings.TrimSpace(strings.ToLower(k))
		switch kk {
		case "instinct":
			out = append(out, ecc.KindInstinct)
		case "skill":
			out = append(out, ecc.KindSkill)
		case "agent":
			out = append(out, ecc.KindAgent)
		case "command":
			out = append(out, ecc.KindCommand)
		}
	}
	return out
}

func renderManifest(corp *ecc.Corpus, res ecc.MigrationResult, dryRun bool, target string) string {
	var b strings.Builder
	mode := "dry-run"
	if !dryRun {
		mode = "apply"
	}
	fmt.Fprintf(&b, "# bough ecc import — %s\n\n", mode)
	fmt.Fprintf(&b, "Source root: `%s`\n\n", corp.Root)
	fmt.Fprintf(&b, "Output directory: `%s`\n\n", target)

	fmt.Fprintf(&b, "## Discovery\n\n")
	fmt.Fprintf(&b, "| Kind | Files | Migrated | Skipped |\n|---|---:|---:|---:|\n")
	fmt.Fprintf(&b, "| instinct | %d | %d | %d |\n", len(corp.Instincts), len(res.InstinctCandidates), res.SkippedInstincts)
	fmt.Fprintf(&b, "| skill | %d | %d | %d |\n", len(corp.Skills), len(res.SkillArtifacts), res.SkippedSkills)
	fmt.Fprintf(&b, "| agent | %d | %d | %d |\n", len(corp.Agents), len(res.AgentArtifacts), res.SkippedAgents)
	fmt.Fprintf(&b, "| command | %d | %d | %d |\n\n", len(corp.Commands), len(res.CommandArtifacts), res.SkippedCommands)

	if len(corp.Projects) > 0 {
		fmt.Fprintf(&b, "## Projects discovered\n\n")
		for id, pj := range corp.Projects {
			fmt.Fprintf(&b, "- `%s` — %s (`%s`)\n", id, pj.Name, pj.Root)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(corp.Errors) > 0 {
		fmt.Fprintf(&b, "## Soft errors (= one bad file does not abort the walk)\n\n")
		max := 25
		for i, e := range corp.Errors {
			if i >= max {
				fmt.Fprintf(&b, "- ... and %d more (showing first %d)\n", len(corp.Errors)-max, max)
				break
			}
			fmt.Fprintf(&b, "- %s\n", e)
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "## Sample migrated candidates\n\n")
	limit := 5
	for i, cand := range res.InstinctCandidates {
		if i >= limit {
			break
		}
		fmt.Fprintf(&b, "- %s — `%s` (scope=%s, conf=%.2f)\n", cand.ID, oneLineShort(cand.Rule, 100), cand.Scope.Level, cand.Confidence)
	}

	fmt.Fprintf(&b, "\n## Next step\n\n")
	fmt.Fprintf(&b, "v0.7.2 ships the read-side. The memory backend write loop\nwires in v0.7.x (= P4 memory backend integration). Until then\nthis manifest + the --json outputs are how an operator pipes the\nECC corpus into bough's downstream tooling.\n")
	return b.String()
}

func oneLineShort(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func writeJSONOutputs(target string, res ecc.MigrationResult) error {
	if err := writeIndentedJSON(filepath.Join(target, "instincts.json"), res.InstinctCandidates); err != nil {
		return err
	}
	if err := writeIndentedJSON(filepath.Join(target, "skills.json"), res.SkillArtifacts); err != nil {
		return err
	}
	if err := writeIndentedJSON(filepath.Join(target, "agents.json"), res.AgentArtifacts); err != nil {
		return err
	}
	return writeIndentedJSON(filepath.Join(target, "commands.json"), res.CommandArtifacts)
}

func writeIndentedJSON(path string, v interface{}) error {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return os.WriteFile(path, buf, 0o644)
}
