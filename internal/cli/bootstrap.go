package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// newBootstrapCmd wires `bough bootstrap` — the v0.7.0 Bootstrap
// safety floor's user-facing entry point.
//
// Round 5 review insisted (AI A Alternative 2 = Dry-run First) that
// Bootstrap Agent output land as Markdown proposals under
// `.bough/proposals/<ts>/` first, so the operator can `git diff`
// the candidates and approve them through `bough instinct approve`
// rather than the agent reaching directly into SQLite. v0.7.0
// implements only the `--dry-run` path; the active path that
// promotes candidates into the memory backend lands in v0.7.1
// alongside the evolve-v3 + LLM-judge integration.
//
// The v0.7.0 dry-run reads raw observations from
// `.bough/observations.jsonl` (= the file `bough hook handle`
// writes from O-1.6), groups them by (event, tool), and writes
// one Markdown file per group plus a `_manifest.md` index. With
// no observations yet, the dry-run still produces the manifest
// + an empty group summary so the operator sees the shape the
// approval flow will consume.
func newBootstrapCmd() *cobra.Command {
	var (
		dryRun bool
		outDir string
		obsLog string
	)
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Generate candidate artifacts (memory / rule / skill / command / tool / agent / evaluator) from observations",
		Long: `bough bootstrap synthesises CapabilityArtifact candidates
from the raw event log Claude Code hooks produce. v0.7.0 ships
only the --dry-run path: candidates land as Markdown proposals
under .bough/proposals/<timestamp>/ so the operator can git-diff
the output before any row reaches the memory backend.

The live path (= candidates auto-stored as state="candidate" with
the LLM judge ranking + the evolve-v3 cluster framework) ships in
v0.7.1.`,
		RunE: func(c *cobra.Command, _ []string) error {
			if !dryRun {
				return fmt.Errorf("bough bootstrap requires --dry-run in v0.7.0 (= the live ingest path lands in v0.7.1 with evolve-v3 + LLM judge)")
			}
			ts := time.Now().UTC().Format("20060102T150405Z")
			target := filepath.Join(outDir, ts)
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
			summary, err := summariseObservations(obsLog)
			if err != nil {
				return err
			}
			manifest := buildManifest(ts, target, obsLog, summary)
			manifestPath := filepath.Join(target, "_manifest.md")
			if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
				return fmt.Errorf("write manifest %s: %w", manifestPath, err)
			}
			fmt.Fprintf(c.OutOrStdout(), "wrote %s\n", manifestPath)
			for event, count := range summary.PerEvent {
				if count == 0 {
					continue
				}
				groupFile := filepath.Join(target, fmt.Sprintf("%s.md", event))
				body := fmt.Sprintf(
					"# %s candidates (dry-run)\n\nObservations counted: %d\n\nv0.7.1 fills this file with the per-observation\nCandidate rule the LLM judge minted; v0.7.0 reports the count so\nan operator can verify the observer is capturing what they\nexpect before the live path turns on.\n",
					event, count,
				)
				if err := os.WriteFile(groupFile, []byte(body), 0o644); err != nil {
					return fmt.Errorf("write %s: %w", groupFile, err)
				}
				fmt.Fprintf(c.OutOrStdout(), "wrote %s\n", groupFile)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "write candidate artifacts to .bough/proposals/<ts>/*.md instead of the memory backend (REQUIRED in v0.7.0)")
	cmd.Flags().StringVar(&outDir, "out-dir", ".bough/proposals", "parent directory for the per-run proposals subdirectory")
	cmd.Flags().StringVar(&obsLog, "observations", ".bough/observations.jsonl", "raw observation log path (= the file `bough hook handle` writes; defaults to .bough/observations.jsonl)")
	return cmd
}

// observationSummary aggregates the raw observations.jsonl file
// into per-event counts so the manifest can render a "what the
// observer saw" line without dumping the full file.
type observationSummary struct {
	Total    int
	PerEvent map[string]int
	Present  bool
}

func summariseObservations(path string) (observationSummary, error) {
	sum := observationSummary{PerEvent: map[string]int{}}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sum, nil
		}
		return sum, fmt.Errorf("open observations %s: %w", path, err)
	}
	defer f.Close()
	sum.Present = true
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		var event string
		if raw, ok := rec["event"]; ok {
			_ = json.Unmarshal(raw, &event)
		}
		if event == "" {
			continue
		}
		sum.Total++
		sum.PerEvent[event]++
	}
	if err := scanner.Err(); err != nil {
		return sum, fmt.Errorf("scan observations: %w", err)
	}
	return sum, nil
}

func buildManifest(ts, target, obsLog string, sum observationSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# bough bootstrap proposals — %s (dry-run)\n\n", ts)
	fmt.Fprintf(&b, "Output directory: `%s`\n\n", target)
	fmt.Fprintf(&b, "Observation source: `%s`", obsLog)
	if !sum.Present {
		fmt.Fprintf(&b, " (absent on disk; the dry-run still runs so the proposal shape is visible)")
	}
	fmt.Fprintf(&b, "\n\n")
	fmt.Fprintf(&b, "## Summary\n\n")
	if sum.Total == 0 {
		fmt.Fprintf(&b, "No observations were available for this dry-run. v0.7.0 ships the proposal\nlayer skeleton + the approval flow shape; the live observer that fills\n`.bough/observations.jsonl` wires in O-1.6, and the LLM-judge candidate\ngeneration that fills the per-event group files wires in v0.7.1.\n\n")
	} else {
		fmt.Fprintf(&b, "Observations counted: **%d**\n\n", sum.Total)
		fmt.Fprintf(&b, "| Event | Count |\n|---|---:|\n")
		for event, count := range sum.PerEvent {
			fmt.Fprintf(&b, "| %s | %d |\n", event, count)
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "## How to approve\n\n")
	fmt.Fprintf(&b, "The v0.7.1 release ships `bough instinct approve --from <proposals-dir>`\nso an operator can review the per-event Markdown files (= one per\nartifact kind the LLM judge proposed) and promote the ones they want\ninto the memory backend.\n\nFor v0.7.0 the proposals layer is informational only — the per-event\nfiles describe the observations the agent saw, not yet the candidate\nrules the agent minted from them.\n")
	return b.String()
}
