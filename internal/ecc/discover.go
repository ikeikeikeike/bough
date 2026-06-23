package ecc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultRoot is the canonical ECC corpus path on macOS / Linux.
// Operators on a different layout pass --ecc-root explicitly.
const DefaultRoot = "~/.local/share/ecc-homunculus"

// ExpandRoot resolves leading ~ in the corpus path to the current
// user's home directory. Returns the input unchanged when no ~ is
// present.
func ExpandRoot(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p, fmt.Errorf("ExpandRoot: %w", err)
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}

// Discover walks an ECC corpus rooted at root and returns the
// parsed Corpus. Per-file parse errors are accumulated in
// Corpus.Errors so one malformed file does not abort the walk.
//
// Filter selects which artifact kinds to load. Pass nil to load
// every kind; pass {KindInstinct} to load only instincts; etc.
func Discover(root string, filter []ArtifactKind) (*Corpus, error) {
	root, err := ExpandRoot(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("ecc.Discover: stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("ecc.Discover: %s is not a directory", root)
	}
	want := wantSet(filter)
	corp := &Corpus{Root: root, Projects: map[string]Project{}}

	// projects.json (= id → project metadata)
	pjPath := filepath.Join(root, "projects.json")
	if raw, err := os.ReadFile(pjPath); err == nil {
		var pjs map[string]Project
		if jerr := json.Unmarshal(raw, &pjs); jerr == nil {
			for id, pj := range pjs {
				if pj.ID == "" {
					pj.ID = id
				}
				corp.Projects[id] = pj
			}
		} else {
			corp.Errors = append(corp.Errors, fmt.Sprintf("parse projects.json: %v", jerr))
		}
	}

	if want[KindInstinct] {
		corp.Instincts = append(corp.Instincts, scanInstincts(corp, root)...)
	}
	if want[KindSkill] {
		corp.Skills = append(corp.Skills, scanSkills(corp, root)...)
	}
	if want[KindAgent] {
		corp.Agents = append(corp.Agents, scanAgents(corp, root)...)
	}
	if want[KindCommand] {
		corp.Commands = append(corp.Commands, scanCommands(corp, root)...)
	}
	return corp, nil
}

func wantSet(filter []ArtifactKind) map[ArtifactKind]bool {
	if len(filter) == 0 {
		return map[ArtifactKind]bool{
			KindInstinct: true, KindSkill: true,
			KindAgent: true, KindCommand: true,
		}
	}
	want := map[ArtifactKind]bool{}
	for _, k := range filter {
		want[k] = true
	}
	return want
}

// catalogFilenames are ECC bookkeeping files (= ALL-CAPS plain
// markdown) that share the instincts/ directory but never carry
// frontmatter. We skip them silently so the soft-errors list stays
// clean of pathologically-expected misses.
var catalogFilenames = map[string]struct{}{
	"INSTINCTS.md": {},
	"MEMORY.md":    {},
	"README.md":    {},
}

func scanInstincts(corp *Corpus, root string) []Instinct {
	out := []Instinct{}
	// Walk every */instincts/* path so global + per-project entries
	// are both picked up (= ECC stores them in parallel trees).
	_ = filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return nil // swallow directory iteration errors; record in Errors below.
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		if !strings.Contains(path, "/instincts/") {
			return nil
		}
		if strings.Contains(path, "/evolved/") {
			return nil // evolved/* are skills/agents/commands, not instincts
		}
		if _, isCatalog := catalogFilenames[filepath.Base(path)]; isCatalog {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			corp.Errors = append(corp.Errors, fmt.Sprintf("read %s: %v", path, rerr))
			return nil
		}
		inst, perr := ParseInstinct(string(raw), path)
		if perr != nil {
			// Files without frontmatter are common for early-format
			// instincts that ECC has not yet rewritten; demote those
			// to a silent skip rather than a soft error so the count
			// reflects only "we tried and failed", not "we expected
			// to fail".
			if strings.Contains(perr.Error(), "no YAML frontmatter") {
				return nil
			}
			corp.Errors = append(corp.Errors, fmt.Sprintf("%s: %v", path, perr))
			return nil
		}
		out = append(out, inst)
		return nil
	})
	return out
}

func scanSkills(corp *Corpus, root string) []Skill {
	out := []Skill{}
	_ = filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		if !strings.Contains(path, "/evolved/skills/") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			corp.Errors = append(corp.Errors, fmt.Sprintf("read %s: %v", path, rerr))
			return nil
		}
		s, perr := ParseSkill(string(raw), path)
		if perr != nil {
			corp.Errors = append(corp.Errors, fmt.Sprintf("%s: %v", path, perr))
			return nil
		}
		out = append(out, s)
		return nil
	})
	return out
}

func scanAgents(corp *Corpus, root string) []Agent {
	out := []Agent{}
	_ = filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		if !strings.Contains(path, "/evolved/agents/") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			corp.Errors = append(corp.Errors, fmt.Sprintf("read %s: %v", path, rerr))
			return nil
		}
		a, perr := ParseAgent(string(raw), path)
		if perr != nil {
			corp.Errors = append(corp.Errors, fmt.Sprintf("%s: %v", path, perr))
			return nil
		}
		out = append(out, a)
		return nil
	})
	return out
}

func scanCommands(corp *Corpus, root string) []Command {
	out := []Command{}
	_ = filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		if !strings.Contains(path, "/evolved/commands/") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			corp.Errors = append(corp.Errors, fmt.Sprintf("read %s: %v", path, rerr))
			return nil
		}
		c, perr := ParseCommand(string(raw), path)
		if perr != nil {
			corp.Errors = append(corp.Errors, fmt.Sprintf("%s: %v", path, perr))
			return nil
		}
		out = append(out, c)
		return nil
	})
	return out
}
