package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHookHandle_AppendsObservation drives the `bough hook handle`
// dispatcher end-to-end: feed a fixture JSON on stdin, check the
// .bough/observations.jsonl file gets one canonical JSONL line
// stamped with the event name and the raw payload.
func TestHookHandle_AppendsObservation(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, ".bough", "observations.jsonl")
	cmd := newHookHandleCmd()
	cmd.SetArgs([]string{
		"--event", "PreToolUse",
		"--out", outPath,
	})
	cmd.SetIn(strings.NewReader(`{"hook_event_name":"PreToolUse","fixture":"smoke"}`))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read observations: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("observation line missing trailing newline")
	}
	var rec struct {
		TS      string          `json:"ts"`
		Event   string          `json:"event"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &rec); err != nil {
		t.Fatalf("parse observation: %v", err)
	}
	if rec.Event != "PreToolUse" {
		t.Errorf("event: got %q want %q", rec.Event, "PreToolUse")
	}
	if rec.TS == "" {
		t.Errorf("ts: expected RFC3339Nano timestamp")
	}
	if !strings.Contains(string(rec.Payload), `"fixture":"smoke"`) {
		t.Errorf("payload: stdin not round-tripped: %s", rec.Payload)
	}
}

// TestHookHandle_RejectsBadPayload ensures a malformed JSON payload
// surfaces as a hook failure rather than silently appending
// garbage to the observation log.
func TestHookHandle_RejectsBadPayload(t *testing.T) {
	dir := t.TempDir()
	cmd := newHookHandleCmd()
	cmd.SetArgs([]string{
		"--event", "PreToolUse",
		"--out", filepath.Join(dir, ".bough", "observations.jsonl"),
	})
	cmd.SetIn(strings.NewReader("not-valid-json"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected execute to error on malformed JSON")
	}
}

// TestHookHandle_RequiresEvent ensures a missing --event flag is
// caught early. Claude Code's settings.json wiring always supplies
// the flag, but a hand-invocation should fail fast.
func TestHookHandle_RequiresEvent(t *testing.T) {
	cmd := newHookHandleCmd()
	cmd.SetArgs([]string{})
	cmd.SetIn(strings.NewReader(`{}`))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected execute to error when --event is missing")
	}
}
