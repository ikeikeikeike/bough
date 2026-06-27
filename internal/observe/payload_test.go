package observe

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestObservationUnmarshal_NestedPayload is the regression test for the
// hook-writer / observer-reader schema mismatch. `bough hook handle`
// stores the Claude Code event verbatim under "payload" (tool_name,
// tool_input nested), but the observer's flat Observation struct only
// bound ts+event — so every extraction pass saw hollow {ts,event}
// records and Claude correctly minted zero instincts.
func TestObservationUnmarshal_NestedPayload(t *testing.T) {
	// a real-shape Claude Code PostToolUse envelope as written by
	// `bough hook handle` (payload nested, Claude Code field names).
	rec := `{"ts":"2026-06-27T09:19:44.081761Z","event":"PostToolUse","payload":{"session_id":"sess-1","cwd":"/repo","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"make test"},"tool_response":{"stdout":"ok"}}}`

	var o Observation
	if err := json.Unmarshal([]byte(rec), &o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if o.Event != "PostToolUse" {
		t.Errorf("Event = %q, want PostToolUse", o.Event)
	}
	if o.Tool != "Bash" {
		t.Errorf("Tool = %q, want Bash (payload.tool_name must map across)", o.Tool)
	}
	if o.CWD != "/repo" {
		t.Errorf("CWD = %q, want /repo", o.CWD)
	}
	if o.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", o.SessionID)
	}
	if !strings.Contains(string(o.ToolInput), "make test") {
		t.Errorf("ToolInput = %q, want it to carry the command", o.ToolInput)
	}
	if !strings.Contains(string(o.ToolOutput), "ok") {
		t.Errorf("ToolOutput = %q, want it to carry tool_response", o.ToolOutput)
	}

	// The re-marshal buildObserverData feeds to Claude must now carry
	// the tool name — otherwise the rendered prompt is still hollow.
	line, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !strings.Contains(string(line), `"tool":"Bash"`) {
		t.Errorf("re-marshaled observation %s missing tool name", line)
	}
}

// TestObservationUnmarshal_FlatLegacy proves the flat schema that
// observe.Writer.Append and `bough ecc import` emit still binds when
// there is no "payload" key — the fix must not regress the path that
// produced threecorp's working corpus.
func TestObservationUnmarshal_FlatLegacy(t *testing.T) {
	rec := `{"ts":"2026-06-27T09:19:44Z","event":"PostToolUse","tool":"Edit","cwd":"/repo","tool_input":{"file_path":"x.go"}}`
	var o Observation
	if err := json.Unmarshal([]byte(rec), &o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if o.Tool != "Edit" {
		t.Errorf("Tool = %q, want Edit (flat top-level must still bind)", o.Tool)
	}
	if o.CWD != "/repo" {
		t.Errorf("CWD = %q, want /repo", o.CWD)
	}
	if !strings.Contains(string(o.ToolInput), "x.go") {
		t.Errorf("ToolInput = %q, want the flat top-level value", o.ToolInput)
	}
}

// TestObservationUnmarshal_FlatWinsOverPayload guards the precedence
// rule: when a record carries both a flat field and the same field
// nested under payload, the flat value is kept (only empties backfill).
func TestObservationUnmarshal_FlatWinsOverPayload(t *testing.T) {
	rec := `{"ts":"2026-06-27T09:19:44Z","event":"PostToolUse","tool":"Edit","payload":{"tool_name":"Bash"}}`
	var o Observation
	if err := json.Unmarshal([]byte(rec), &o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if o.Tool != "Edit" {
		t.Errorf("Tool = %q, want Edit (flat must win over payload backfill)", o.Tool)
	}
}

// TestObservationUnmarshal_UserPrompt maps payload.prompt for the
// UserPromptSubmit event so the observer can mine "user corrections".
func TestObservationUnmarshal_UserPrompt(t *testing.T) {
	rec := `{"ts":"2026-06-27T09:17:01Z","event":"UserPromptSubmit","payload":{"session_id":"s","cwd":"/r","prompt":"always run make format first"}}`
	var o Observation
	if err := json.Unmarshal([]byte(rec), &o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if o.Prompt != "always run make format first" {
		t.Errorf("Prompt = %q, want the user text", o.Prompt)
	}
}

// TestObservationUnmarshal_OddPayload tolerates a non-object payload
// (spec drift) without dropping the envelope-level fields.
func TestObservationUnmarshal_OddPayload(t *testing.T) {
	for _, rec := range []string{
		`{"ts":"2026-06-27T09:17:01Z","event":"Stop","payload":null}`,
		`{"ts":"2026-06-27T09:17:01Z","event":"Stop","payload":"just-a-string"}`,
		`{"ts":"2026-06-27T09:17:01Z","event":"Stop"}`,
	} {
		var o Observation
		if err := json.Unmarshal([]byte(rec), &o); err != nil {
			t.Fatalf("unmarshal %q: %v", rec, err)
		}
		if o.Event != "Stop" {
			t.Errorf("rec %q: Event = %q, want Stop", rec, o.Event)
		}
	}
}
