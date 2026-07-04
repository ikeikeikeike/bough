package observe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWriter_AppendStampsTS(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(filepath.Join(dir, "observations.jsonl"))
	frozen := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	w.SetClock(func() time.Time { return frozen })

	if err := w.Append(Observation{Event: "PostToolUse", Tool: "Bash"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	rows, err := ReadAll(w.Path())
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if !rows[0].TS.Equal(frozen) {
		t.Errorf("TS = %s, want %s", rows[0].TS, frozen)
	}
	if rows[0].Tool != "Bash" {
		t.Errorf("Tool = %q", rows[0].Tool)
	}
}

func TestWriter_AppendRejectsMissingEvent(t *testing.T) {
	w := NewWriter(filepath.Join(t.TempDir(), "x.jsonl"))
	err := w.Append(Observation{Tool: "Bash"})
	if err == nil {
		t.Errorf("expected error for empty Event, got nil")
	}
}

func TestWriter_ConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(filepath.Join(dir, "concurrent.jsonl"))
	var wg sync.WaitGroup
	const N = 32
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]int{"i": i})
			_ = w.Append(Observation{Event: "PostToolUse", Tool: "Bash", ToolInput: payload})
		}(i)
	}
	wg.Wait()
	rows, err := ReadAll(w.Path())
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(rows) != N {
		t.Errorf("rows = %d, want %d (concurrent appends must not lose lines)", len(rows), N)
	}
}

func TestTailN(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(filepath.Join(dir, "x.jsonl"))
	for i := 0; i < 10; i++ {
		_ = w.Append(Observation{Event: "PostToolUse", Tool: "Bash"})
	}
	tail, err := TailN(w.Path(), 3)
	if err != nil {
		t.Fatalf("TailN: %v", err)
	}
	if len(tail) != 3 {
		t.Errorf("tail = %d, want 3", len(tail))
	}
}

func TestReadAll_TolerantOfPartialLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drift.jsonl")
	good := `{"ts":"2026-06-23T00:00:00Z","event":"PostToolUse","tool":"Bash"}`
	junk := `{"ts":"oops","event":` // truncated mid-record
	body := []byte(good + "\n" + junk + "\n" + good + "\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rows, err := ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2 (one junk line dropped silently)", len(rows))
	}
}

func TestSanitizeAnthropicEnv(t *testing.T) {
	env := []string{
		"HOME=/Users/me",
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-ant-XYZ",
		"ANTHROPIC_AUTH_TOKEN=tok-XYZ",
		"anthropic_base_url=https://api.anthropic.com", // lower-case test
		"BOUGH_HOMUNCULUS_DIR=/x",
	}
	out := SanitizeAnthropicEnv(env)
	for _, kv := range out {
		for _, drop := range AnthropicAPIEnvVars {
			if len(kv) > len(drop) && kv[:len(drop)] == drop {
				// substring check is safe because each name has '=' delimiter
				if kv[len(drop)] == '=' {
					t.Errorf("variable %q survived sanitisation", kv)
				}
			}
		}
	}
	// non-API vars must survive
	gotPath := false
	for _, kv := range out {
		if kv == "PATH=/usr/bin" {
			gotPath = true
		}
	}
	if !gotPath {
		t.Errorf("PATH was dropped; sanitisation is over-eager")
	}
}

// TestSanitizeAnthropicEnv_StripsBedrockVertexSwitches is the
// regression guard for the wave-4 review finding: AnthropicAPIEnvVars
// listed only the Bedrock/Vertex auxiliary endpoint/project-override
// vars (ANTHROPIC_BEDROCK_BASE_URL / ANTHROPIC_VERTEX_*), never the
// actual CLAUDE_CODE_USE_BEDROCK / CLAUDE_CODE_USE_VERTEX enable
// switches — so an operator's normal enterprise env (Bedrock/Vertex
// billing) passed straight through into the spawned `claude --print`
// subprocess, contradicting the README's "cannot silently flip to API
// billing" claim.
func TestSanitizeAnthropicEnv_StripsBedrockVertexSwitches(t *testing.T) {
	env := []string{
		"HOME=/Users/me",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"CLAUDE_CODE_USE_VERTEX=1",
	}
	out := SanitizeAnthropicEnv(env)
	for _, kv := range out {
		if kv == "CLAUDE_CODE_USE_BEDROCK=1" || kv == "CLAUDE_CODE_USE_VERTEX=1" {
			t.Errorf("Bedrock/Vertex enable switch survived sanitisation: %q", kv)
		}
	}
}

func TestDetectAnthropicAPIVars(t *testing.T) {
	env := []string{
		"HOME=/x",
		"ANTHROPIC_API_KEY=sk-ant-XYZ",
		"BOUGH_HOMUNCULUS_DIR=/x",
		"ANTHROPIC_BASE_URL=https://api.anthropic.com",
	}
	got := DetectAnthropicAPIVars(env)
	want := []string{"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}
