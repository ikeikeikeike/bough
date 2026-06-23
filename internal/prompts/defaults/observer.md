IMPORTANT: You are running in non-interactive --print mode under bough's observer daemon. You MUST return a single JSON object that conforms to the supplied schema. Do NOT ask for permission, do NOT ask for confirmation, do NOT output summaries instead of JSON. The Go host will read your JSON and write the instinct files itself; you do not have the Write tool.

Read the supplied observation slice (= the tail of bough's per-project observations.jsonl) and identify recurring patterns for the project "{{.ProjectName}}" (= id {{.ProjectID}}):

- user corrections of your own tool use
- error → resolution sequences that repeat
- repeated workflows (= same Bash → same Edit → same Read order)
- explicit tool preferences (= "always use ripgrep" / "never grep")
- file-shape conventions the operator enforces by hand

For every cleanly-supported pattern with **3 or more occurrences** in the slice, emit one instinct entry. The output schema requires:

- `id`: kebab-case slug, lowercase ASCII letters / digits / hyphen. The host will refuse `BadID`, `with space`, `trailing-`, `double--dash`. It will also reject ids already present in this project's instincts/ tree if you echo them verbatim with no new evidence — use a fresh suffix or skip if it is the same pattern.
- `trigger`: one-line predicate. Start with `when …`. Narrow, specific.
- `confidence`: 0.30 — 0.85. Calibrate by frequency: 3-5 occurrences = 0.50, 6-10 = 0.70, 11+ = 0.85.
- `domain`: one of {`code-style`, `testing`, `git`, `debugging`, `workflow`, `file-patterns`}.
- `scope`: `project` by default. Use `global` only when the pattern is plainly cross-project (= e.g. "always validate user input"). Examples of global: language-level coding hygiene. Examples of project: framework-specific conventions, repo-specific filenames, this team's git workflow.
- `action`: one clear sentence. Imperative voice. Under 500 characters. Never include code snippets — describe what to do, not how to type it.
- `evidence`: at least one item, at most five. Each entry under 200 characters, no code snippets, no secrets. Cite the session ID + count + pattern summary in plain English.

Hard rules:

- Be conservative. If a pattern has fewer than 3 cleanly-matching occurrences, omit it. Returning fewer instincts is always better than returning weak ones.
- No code in `action` or `evidence`. The Go host will refuse entries containing fenced blocks or obvious code patterns.
- No secrets. Strip API keys, paths under `/secrets/`, `*_TOKEN`, `*_PASSWORD`. The host re-checks and refuses.
- One JSON document, no surrounding prose, no markdown fences.
- At most 5 instincts per call. The host hard-caps the JSON list at 5 and will reject longer arrays.

Project context (the host fills these in before sending):

- Project name: {{.ProjectName}}
- Project id  : {{.ProjectID}}
- Session id  : {{.SessionID}}
- Window      : last {{.WindowSize}} observation records, captured between {{.WindowStart}} and {{.WindowEnd}}.

Existing instincts for this project (kept here so you do not re-mint near-duplicates):

{{.ExistingIDs}}

Observation slice (JSONL, one record per line; truncated to the window above):

{{.Observations}}

Return a JSON object matching this schema:

```json
{
  "type": "object",
  "required": ["instincts"],
  "properties": {
    "instincts": {
      "type": "array",
      "maxItems": 5,
      "items": {
        "type": "object",
        "required": ["id", "trigger", "confidence", "domain", "scope", "action", "evidence"],
        "properties": {
          "id":         {"type": "string", "pattern": "^[a-z0-9]+(-[a-z0-9]+)*$"},
          "trigger":    {"type": "string", "maxLength": 200},
          "confidence": {"type": "number", "minimum": 0.30, "maximum": 0.85},
          "domain":     {"type": "string", "enum": ["code-style", "testing", "git", "debugging", "workflow", "file-patterns"]},
          "scope":      {"type": "string", "enum": ["project", "global"]},
          "action":     {"type": "string", "maxLength": 500},
          "evidence":   {"type": "array", "minItems": 1, "maxItems": 5, "items": {"type": "string", "maxLength": 200}}
        }
      }
    }
  }
}
```

If no qualifying patterns exist in this slice, return `{"instincts": []}` — an empty array is the correct answer when the operator has not yet generated 3-occurrence patterns.
