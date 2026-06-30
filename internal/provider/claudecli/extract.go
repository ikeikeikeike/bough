package claudecli

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ExtractResultJSON pulls the model's structured answer out of the
// `claude --print --output-format json` output.
//
// That stdout is the CLI's *result envelope*, not the model's answer.
// Depending on the CLI version it is either:
//
//   - a single object: {"type":"result","result":"<text>", …}, or
//   - a JSON array of stream events, the one with "type":"result"
//     carrying the "result" text.
//
// The `result` text is the model's reply, usually wrapped in a
// ```json … ``` fence. ExtractResultJSON returns the inner JSON bytes,
// ready to unmarshal into a verdict / instinct struct.
//
// If raw is already a bare JSON object that is plainly the answer (no
// "result" wrapper — e.g. a FakeExec test, or a future --output-format
// text), it is returned unchanged. This is the seam the GATE 5 judge
// was missing: before it existed the raw envelope was unmarshalled
// straight into the verdict struct, leaving every field zero so
// ValidateVerdict failed and every cluster fell back to DOUBT.
func ExtractResultJSON(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, ErrEmptyOutput
	}
	switch trimmed[0] {
	case '[':
		text, err := resultTextFromEvents(trimmed)
		if err != nil {
			return nil, err
		}
		return stripCodeFence(text), nil
	case '{':
		obj := map[string]json.RawMessage{}
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return nil, fmt.Errorf("claudecli.ExtractResultJSON: object: %w", err)
		}
		rawResult, ok := obj["result"]
		if !ok {
			// No envelope wrapper: this already looks like the answer.
			return trimmed, nil
		}
		return stripCodeFence(unquoteResult(rawResult)), nil
	default:
		// Plain text (no envelope): the model reply verbatim.
		return stripCodeFence(trimmed), nil
	}
}

// resultTextFromEvents finds the "type":"result" element of the event
// array and returns its "result" field as bytes. (An "assistant" event
// carries its text under "message", never a top-level "result", so there
// is no assistant-level fallback to attempt — a stream with no terminal
// result element is a genuine error.)
func resultTextFromEvents(arr []byte) ([]byte, error) {
	var events []map[string]json.RawMessage
	if err := json.Unmarshal(arr, &events); err != nil {
		return nil, fmt.Errorf("claudecli.ExtractResultJSON: event array: %w", err)
	}
	for _, ev := range events {
		typ := ""
		if t, ok := ev["type"]; ok {
			_ = json.Unmarshal(t, &typ)
		}
		if typ == "result" {
			if r, ok := ev["result"]; ok {
				return unquoteResult(r), nil
			}
		}
	}
	return nil, fmt.Errorf("claudecli.ExtractResultJSON: no result element in %d events", len(events))
}

// unquoteResult turns a JSON string value into its bytes; if the value
// is itself a JSON object/array (a CLI variant that puts structured
// output directly in `result`), it is returned as-is.
func unquoteResult(rawResult json.RawMessage) []byte {
	var s string
	if err := json.Unmarshal(rawResult, &s); err == nil {
		return []byte(s)
	}
	return rawResult
}

// stripCodeFence removes a leading ```json (or ```) fence line and a
// trailing ``` from a model reply, plus surrounding whitespace, so what
// remains is the bare JSON payload.
func stripCodeFence(b []byte) []byte {
	s := bytes.TrimSpace(b)
	if !bytes.HasPrefix(s, []byte("```")) {
		return s
	}
	if nl := bytes.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:] // drop the opening ```json line
	} else {
		s = bytes.TrimPrefix(s, []byte("```"))
	}
	if i := bytes.LastIndex(s, []byte("```")); i >= 0 {
		s = s[:i] // drop the closing fence
	}
	return bytes.TrimSpace(s)
}
