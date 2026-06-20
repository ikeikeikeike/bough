package instinct

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestNewEventWriter_RejectsRelativePath pins the v0.5.1 LOW #18 fix:
// callers must supply an absolute path so two worktrees (or a CI run
// + a dev shell) cannot race on a cwd-local events.jsonl.
func TestNewEventWriter_RejectsRelativePath(t *testing.T) {
	cases := []string{
		"events.jsonl",
		".bough/memory/events.jsonl",
		"./relative/events.jsonl",
	}
	for _, p := range cases {
		w, err := NewEventWriter(p)
		if err == nil {
			_ = w.Close()
			t.Errorf("NewEventWriter(%q): want error, got nil", p)
			continue
		}
		if !strings.Contains(err.Error(), "absolute") {
			t.Errorf("NewEventWriter(%q): error should mention absolute path, got %q", p, err.Error())
		}
	}
}

// TestNewEventWriter_RejectsEmptyPath asserts the empty-path branch
// returns a distinct, actionable error.
func TestNewEventWriter_RejectsEmptyPath(t *testing.T) {
	if _, err := NewEventWriter(""); err == nil {
		t.Fatalf("NewEventWriter(\"\"): want error, got nil")
	} else if !strings.Contains(err.Error(), "empty") {
		t.Errorf("NewEventWriter(\"\"): error should mention empty, got %q", err.Error())
	}
}

// TestNewEventWriter_AcceptsAbsolutePath confirms the happy path
// still works and that the writer creates a fresh file.
func TestNewEventWriter_AcceptsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "events.jsonl")
	w, err := NewEventWriter(path)
	if err != nil {
		t.Fatalf("NewEventWriter(absolute): %v", err)
	}
	defer func() { _ = w.Close() }()
	if err := w.Append(Event{Kind: "store", ID: "rule-1", Detail: "hello"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
}
