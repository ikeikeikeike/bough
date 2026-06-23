package ecc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// splitFrontmatter separates the YAML frontmatter from the body
// markdown. ECC convention is `---` on its own line as the open +
// close delimiter. Files without frontmatter (= some commands) are
// returned with an empty front string and the full body.
func splitFrontmatter(raw string) (front, body string) {
	if !strings.HasPrefix(raw, "---\n") && !strings.HasPrefix(raw, "---\r\n") {
		return "", raw
	}
	rest := strings.TrimPrefix(raw, "---\n")
	rest = strings.TrimPrefix(rest, "---\r\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", raw
	}
	front = rest[:idx]
	body = strings.TrimLeft(rest[idx+len("\n---"):], "\r\n-")
	return front, body
}

// parseConfidence accepts "0.8" / "80%" / "80" / empty and returns
// a [0.0, 1.0] float. Empty or unparsable values return 0.
func parseConfidence(s string) float64 {
	s = strings.TrimSpace(strings.TrimRight(s, "%"))
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if f > 1.0 {
		f /= 100.0
	}
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return f
}

// extractSection returns the body paragraph under a `## <name>`
// heading until the next `##` or end of file. Whitespace at the
// boundaries is trimmed; the body content is preserved otherwise.
func extractSection(body, name string) string {
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	want := "## " + name
	collecting := false
	out := strings.Builder{}
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, want) {
			collecting = true
			continue
		}
		if collecting {
			if strings.HasPrefix(trimmed, "## ") {
				break
			}
			out.WriteString(line)
			out.WriteString("\n")
		}
	}
	return strings.TrimSpace(out.String())
}

// extractTitle returns the first `# Title` line as a plain string.
// Returns the empty string when no level-1 heading is found.
func extractTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return ""
}

// parseInstinctFrontmatter unmarshals the YAML keys ECC uses for
// instinct files. Unknown keys are silently ignored so a future
// ECC schema bump does not break bough's reader.
type instinctFrontmatter struct {
	ID          string  `yaml:"id"`
	Trigger     string  `yaml:"trigger"`
	Confidence  string  `yaml:"confidence"` // string so "80%" parses
	Domain      string  `yaml:"domain"`
	Source      string  `yaml:"source"`
	Scope       string  `yaml:"scope"`
	ProjectID   string  `yaml:"project_id"`
	ProjectName string  `yaml:"project_name"`
}

// ParseInstinct parses an ECC instinct file's frontmatter + body
// and returns the Instinct struct. Path is recorded into
// SourcePath so the audit log can trace where each row came from.
func ParseInstinct(raw, path string) (Instinct, error) {
	front, body := splitFrontmatter(raw)
	if front == "" {
		return Instinct{}, errors.New("ParseInstinct: no YAML frontmatter")
	}
	var fm instinctFrontmatter
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return Instinct{}, fmt.Errorf("ParseInstinct: %w", err)
	}
	if fm.ID == "" {
		return Instinct{}, errors.New("ParseInstinct: id missing in frontmatter")
	}
	return Instinct{
		ID:            fm.ID,
		Trigger:       fm.Trigger,
		Confidence:    parseConfidence(fm.Confidence),
		Domain:        fm.Domain,
		Source:        fm.Source,
		Scope:         fm.Scope,
		ProjectID:     fm.ProjectID,
		ProjectName:   fm.ProjectName,
		Body:          body,
		BodyTitle:     extractTitle(body),
		BodyAction:    extractSection(body, "Action"),
		BodyEvidence:  extractSection(body, "Evidence"),
		BodyRationale: extractSection(body, "Rationale"),
		SourcePath:    path,
	}, nil
}

// skillFrontmatter is the YAML shape ECC's evolved/skills/* files
// carry. evolved_from arrives as a YAML list — yaml.Unmarshal
// fills it as a string slice.
type skillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	EvolvedFrom []string `yaml:"evolved_from"`
}

// ParseSkill parses an ECC skill file. Skills carry frontmatter
// with name + description + evolved_from; the body is the rendered
// guidance content the host displays.
func ParseSkill(raw, path string) (Skill, error) {
	front, body := splitFrontmatter(raw)
	if front == "" {
		return Skill{}, errors.New("ParseSkill: no YAML frontmatter")
	}
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return Skill{}, fmt.Errorf("ParseSkill: %w", err)
	}
	if fm.Name == "" {
		return Skill{}, errors.New("ParseSkill: name missing in frontmatter")
	}
	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		EvolvedFrom: fm.EvolvedFrom,
		Body:        body,
		SourcePath:  path,
	}, nil
}

// agentFrontmatter mirrors the Claude Code agent-definition shape
// ECC writes. tools is a comma-separated string in YAML; this
// reader normalises it to a clean []string.
type agentFrontmatter struct {
	Model string `yaml:"model"`
	Tools string `yaml:"tools"`
}

// ParseAgent parses an ECC agent file.
func ParseAgent(raw, path string) (Agent, error) {
	front, body := splitFrontmatter(raw)
	if front == "" {
		return Agent{}, errors.New("ParseAgent: no YAML frontmatter")
	}
	var fm agentFrontmatter
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return Agent{}, fmt.Errorf("ParseAgent: %w", err)
	}
	tools := []string{}
	for _, t := range strings.Split(fm.Tools, ",") {
		if tt := strings.TrimSpace(t); tt != "" {
			tools = append(tools, tt)
		}
	}
	return Agent{
		Name:       extractTitle(body),
		Model:      strings.TrimSpace(fm.Model),
		Tools:      tools,
		Body:       body,
		SourcePath: path,
	}, nil
}

// ParseCommand parses an ECC evolved/commands/<slug>.md file.
// Commands do not carry YAML frontmatter; metadata is pulled from
// "Evolved from instinct: <id>" and "Confidence: <N>%" lines in
// the body.
func ParseCommand(raw, path string) (Command, error) {
	c := Command{
		Body:       raw,
		Name:       extractTitle(raw),
		SourcePath: path,
	}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Evolved from instinct:") {
			c.EvolvedFromInstinct = strings.TrimSpace(strings.TrimPrefix(line, "Evolved from instinct:"))
			continue
		}
		if strings.HasPrefix(line, "Confidence:") {
			c.Confidence = parseConfidence(strings.TrimSpace(strings.TrimPrefix(line, "Confidence:")))
			continue
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return c, fmt.Errorf("ParseCommand: %w", err)
	}
	if c.Name == "" {
		return c, errors.New("ParseCommand: no title heading found")
	}
	return c, nil
}
