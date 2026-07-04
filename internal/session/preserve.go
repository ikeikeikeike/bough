package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ikeikeikeike/bough/internal/homunculus"
)

// PreservedTopN is how many top-confidence instincts the PreCompact
// handler snapshots. ECC snapshots the top 5 before context
// compaction so the most-reliable patterns survive the window reset;
// bough keeps the same count.
const PreservedTopN = 5

// PreserveInstincts writes a MEMORY.md snapshot of the top-confidence
// instincts into the project's instincts dir, so a context
// compaction does not lose the operator's most-reliable learned
// patterns. The PreCompact hook calls this; it is pure filesystem,
// no LLM. Returns the snapshot path AND the rendered top-N block, so
// the caller can also print the block to stdout for operator visibility
// in the transcript. NOTE: a PreCompact hook's plain stdout does NOT reach
// the model's post-compaction context (only SessionStart/Setup inject via
// stdout; PreCompact itself supports only blocking). The durable MEMORY.md
// is the real artifact, and the top instincts re-surface into context on the
// next prompt via the UserPromptSubmit inject — so nothing is lost. (ECC's
// preserve-instincts.sh assumes the stdout fold; that assumption is stale.)
//
// MEMORY.md is one of the catalog filenames ScanInstincts skips, so
// the snapshot never gets re-ingested as an instinct.
func PreserveInstincts(layout homunculus.Layout, projectID string, now time.Time) (string, string, error) {
	instincts, _ := homunculus.ScanInstincts(layout.InstinctsDir(projectID))
	if len(instincts) == 0 {
		return "", "", nil
	}
	sort.SliceStable(instincts, func(i, j int) bool {
		if instincts[i].Confidence != instincts[j].Confidence {
			return instincts[i].Confidence > instincts[j].Confidence
		}
		return instincts[i].ID < instincts[j].ID
	})
	top := instincts
	if len(top) > PreservedTopN {
		top = top[:PreservedTopN]
	}

	var content string
	content += "# MEMORY — top instincts preserved before context compaction\n\n"
	content += fmt.Sprintf("Snapshot: %s\n\n", now.UTC().Format(time.RFC3339))
	for _, in := range top {
		content += fmt.Sprintf("- [%.0f%%] %s\n", in.Confidence*100, firstActionLine(in.Body))
	}

	dir := layout.InstinctsDir(projectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("session.PreserveInstincts: mkdir: %w", err)
	}
	path := filepath.Join(dir, "MEMORY.md")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", "", fmt.Errorf("session.PreserveInstincts: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", "", fmt.Errorf("session.PreserveInstincts: rename: %w", err)
	}
	return path, content, nil
}

// firstActionLine mirrors internal/inject/inject.go's helper of the
// same name so session has no cross-package import for one small
// function. Matches "## Action" case-insensitively like every other
// implementation of this helper in the codebase (inject.go,
// evolve/judge.go, cli/claudemd.go) — a case-sensitive match here was
// the one exception, silently falling through to the wrong line for a
// differently-cased heading.
func firstActionLine(body string) string {
	lines := strings.Split(body, "\n")
	inAction := false
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.EqualFold(t, "## Action") {
			inAction = true
			continue
		}
		if inAction {
			if t == "" {
				continue
			}
			if strings.HasPrefix(t, "## ") {
				break
			}
			return t
		}
	}
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		return t
	}
	return "(no action)"
}
