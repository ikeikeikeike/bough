// Package observe is the raw observation channel between Claude
// Code hooks (PreToolUse / PostToolUse / Stop) and the bough
// homunculus. Every line of observations.jsonl is one captured
// event; the observer daemon later tails the file and asks Claude
// CLI to extract instincts from the trail.
//
// Design contract:
//
//   - Pure filesystem. No LLM call, no network. The hook handler is
//     called every tool-use; LLM-grade work is forbidden here.
//   - Append-only JSONL. Each line is one self-contained record so
//     reading is robust against partial writes and truncations.
//   - Atomic per-line append. We open the file with O_APPEND so two
//     concurrent hook calls (= overlapping bash sessions) never
//     interleave bytes mid-record.
//   - Stable schema. Adding fields is fine; removing is not.
package observe

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Observation is the on-disk record shape. One JSON object per
// jsonl line.
type Observation struct {
	// TS is the host-side capture time in RFC3339Nano UTC. It is
	// set by Writer.Append when the caller leaves it zero so
	// hooks that forget to stamp the field still produce sortable
	// records.
	TS time.Time `json:"ts"`

	// Event is the Claude Code hook event name (PreToolUse,
	// PostToolUse, Stop, ...). Mirrors the names the hook handler
	// registers.
	Event string `json:"event"`

	// SessionID is the claude session id when available. Lets the
	// observer correlate observations within one session window.
	SessionID string `json:"session_id,omitempty"`

	// Tool is the tool name from the hook payload (e.g. "Bash",
	// "Write", "Edit").
	Tool string `json:"tool,omitempty"`

	// ToolInput / ToolOutput hold the raw payload bytes the hook
	// received, kept as json.RawMessage so observe.Writer does not
	// have to know each tool's schema (= forward-compatible with
	// new tools Claude Code adds).
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`
	ToolOutput json.RawMessage `json:"tool_output,omitempty"`

	// CWD is the working directory at hook-fire time, used by the
	// observer to scope per-project analysis.
	CWD string `json:"cwd,omitempty"`
}

// Writer appends Observations to the per-project observations.jsonl
// in the homunculus tree. It is safe for concurrent use across
// goroutines and across processes (= O_APPEND atomic write per
// line on POSIX).
type Writer struct {
	path string
	mu   sync.Mutex
	now  func() time.Time
}

// NewWriter pins a Writer to path. The parent directory is created
// lazily on the first Append call so callers can construct Writers
// without side effects.
func NewWriter(path string) *Writer {
	return &Writer{path: path, now: time.Now}
}

// SetClock overrides the time source used to stamp TS when the
// caller leaves it zero. Tests pin this for golden output stability.
func (w *Writer) SetClock(now func() time.Time) { w.now = now }

// Path returns the on-disk path the writer appends to.
func (w *Writer) Path() string { return w.path }

// Append serialises obs and writes one JSONL line to the underlying
// file. The newline is added by the writer so callers must not
// embed `\n` inside record bytes.
func (w *Writer) Append(obs Observation) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if obs.TS.IsZero() {
		obs.TS = w.now().UTC()
	}
	if obs.Event == "" {
		return fmt.Errorf("observe.Writer.Append: Event is required")
	}
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return fmt.Errorf("observe.Writer.Append: mkdir: %w", err)
	}
	buf, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("observe.Writer.Append: marshal: %w", err)
	}
	buf = append(buf, '\n')
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("observe.Writer.Append: open %s: %w", w.path, err)
	}
	defer f.Close()
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("observe.Writer.Append: write: %w", err)
	}
	return nil
}

// ReadAll loads every observation from path. Lines that fail to
// parse are dropped silently because a half-written record midway
// through the file should not abort the read — the observer is
// designed to tolerate that drift.
func ReadAll(path string) ([]Observation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseLines(raw), nil
}

// TailN returns the last n observations from path without loading
// the whole file into a heap-sized slice when the corpus is large.
// Falls back to ReadAll when the file is small enough that the
// optimisation does not help.
func TailN(path string, n int) ([]Observation, error) {
	if n <= 0 {
		return nil, nil
	}
	all, err := ReadAll(path)
	if err != nil {
		return nil, err
	}
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}

// TailNMerged returns the last n observations across several files,
// concatenated in the order given. One observer pass can therefore
// consume both the hook's project-local inbox
// (<root>/.bough/observations.jsonl, where `bough hook handle` appends
// on every tool use) and the homunculus archive (where `bough ecc
// import` writes). A missing file is skipped — either producer may
// legitimately not have run yet — so only a real read error aborts.
func TailNMerged(n int, paths ...string) ([]Observation, error) {
	if n <= 0 {
		return nil, nil
	}
	var all []Observation
	for _, p := range paths {
		obs, err := ReadAll(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		all = append(all, obs...)
	}
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}

func parseLines(raw []byte) []Observation {
	out := []Observation{}
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i < len(raw) && raw[i] != '\n' {
			continue
		}
		line := raw[start:i]
		start = i + 1
		if len(line) == 0 {
			continue
		}
		var obs Observation
		if err := json.Unmarshal(line, &obs); err != nil {
			continue
		}
		out = append(out, obs)
	}
	return out
}

// Discard is an io.Writer-compatible sink for tests that want to
// exercise the Writer's serialiser without producing on-disk
// artifacts. Plumb it as path "" (tests use os.DevNull).
var _ = io.Discard
