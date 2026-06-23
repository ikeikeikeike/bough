// Package ecc reads the upstream affaan-m/everything-claude-code
// homunculus corpus and projects it onto bough's canonical schemas.
//
// ECC's on-disk layout (sampled 2026-06-23 against the threecorp fork):
//
//	~/.local/share/ecc-homunculus/
//	├── projects.json                     # id → {name, root, remote}
//	├── projects/<project_id>/
//	│   ├── instincts/personal/<slug>.md  # YAML frontmatter + body
//	│   ├── instincts/inherited/<slug>.md
//	│   ├── evolved/agents/<slug>.md
//	│   ├── evolved/commands/<slug>.md
//	│   ├── evolved/skills/<slug>.md
//	│   └── observations.jsonl
//	├── instincts/personal/<slug>.md      # global personal
//	├── instincts/inherited/<slug>.md     # global inherited
//	└── evolved/{agents,commands,skills}/<slug>.md
//
// The ECC artifact kind is decoded from the parent directory name;
// frontmatter shape and body convention are kind-specific. See the
// Parse* functions for the exact tolerances each kind accepts.
//
// Bough mapping:
//
//	ECC instinct → schema.InstinctCandidate (state=candidate)
//	ECC skill    → schema.CapabilityArtifact (kind=skill)
//	ECC agent    → schema.CapabilityArtifact (kind=agent)
//	ECC command  → schema.CapabilityArtifact (kind=command)
//
// v0.7.2 reads but does not write back to the ECC corpus. The
// ingest goes one direction (= ECC → bough memory backend +
// .claude/ tree); ECC remains the canonical source until the
// operator deletes it.
package ecc

import "time"

// ArtifactKind enumerates the four ECC artifact families bough
// recognises. The string values mirror the canonical directory
// names so audit logs and CLI flags stay greppable.
type ArtifactKind string

const (
	KindInstinct ArtifactKind = "instinct"
	KindSkill    ArtifactKind = "skill"
	KindAgent    ArtifactKind = "agent"
	KindCommand  ArtifactKind = "command"
)

// Project mirrors one entry in projects.json.
type Project struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Root       string    `json:"root"`
	Remote     string    `json:"remote,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	LastSeen   time.Time `json:"last_seen,omitempty"`
}

// Instinct is the parsed shape of an ECC instinct file. Confidence
// is stored as float64 even when the on-disk value is "70%" — the
// parser normalises both forms to [0.0, 1.0].
type Instinct struct {
	ID          string
	Trigger     string
	Confidence  float64
	Domain      string
	Source      string
	Scope       string  // "project" | "personal" | "inherited"
	ProjectID   string
	ProjectName string
	Body        string  // raw markdown after frontmatter
	BodyTitle   string  // first # heading
	BodyAction  string  // ## Action paragraph
	BodyEvidence string // ## Evidence paragraph
	BodyRationale string// ## Rationale paragraph
	SourcePath  string  // absolute path the file was loaded from
}

// Skill is the parsed shape of an ECC evolved/skills/<slug>.md
// file. EvolvedFrom carries the source instinct ids so the bough
// CapabilityArtifact.Provenance can chain back.
type Skill struct {
	Name        string
	Description string
	EvolvedFrom []string
	Body        string
	SourcePath  string
}

// Agent is the parsed shape of an ECC evolved/agents/<slug>.md
// file. Tools is the comma-separated list from the frontmatter
// (= the same tokens Claude Code's agent definition accepts).
type Agent struct {
	Name        string
	Model       string
	Tools       []string
	Body        string
	SourcePath  string
}

// Command is the parsed shape of an ECC evolved/commands/<slug>.md
// file. ECC commands do not carry frontmatter; the parser pulls
// metadata from the body's "Evolved from instinct: <id>" +
// "Confidence: N%" lines.
type Command struct {
	Name             string
	EvolvedFromInstinct string
	Confidence       float64
	Body             string
	SourcePath       string
}

// Corpus is the in-memory result of walking an ECC root. The
// embedding loop populates this; downstream migrators read it.
type Corpus struct {
	Root      string
	Projects  map[string]Project
	Instincts []Instinct
	Skills    []Skill
	Agents    []Agent
	Commands  []Command
	Errors    []string // soft errors (= one malformed file does not abort the walk)
}
