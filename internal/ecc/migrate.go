package ecc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/ikeikeikeike/bough/pkg/schema"
)

// MigrationResult aggregates everything one ECC → bough migration
// produces. The CLI surfaces these counts; the audit log persists
// the full lists.
type MigrationResult struct {
	InstinctCandidates []schema.InstinctCandidate
	SkillArtifacts     []schema.CapabilityArtifact
	AgentArtifacts     []schema.CapabilityArtifact
	CommandArtifacts   []schema.CapabilityArtifact
	SkippedInstincts   int
	SkippedSkills      int
	SkippedAgents      int
	SkippedCommands    int
	UnknownProjectRefs []string
}

// MigrateOptions tunes the projection. NowFn is the clock the
// migrator stamps into CreatedAt; tests pin it for byte-stable
// output.
type MigrateOptions struct {
	NowFn        func() time.Time
	DefaultScope schema.ScopeLevel
	MonorepoName string
}

// eccPayload is the JSON blob bough stashes inside CapabilityArtifact.Payload
// to preserve the ECC body text + metadata that does not fit the
// canonical CapabilityArtifact fields.
type eccPayload struct {
	Body                string   `json:"body"`
	SourcePath          string   `json:"source_path"`
	Tools               []string `json:"tools,omitempty"`
	Model               string   `json:"model,omitempty"`
	EvolvedFromInstinct string   `json:"evolved_from_instinct,omitempty"`
	Confidence          float64  `json:"confidence,omitempty"`
	Notes               string   `json:"notes,omitempty"`
}

// Migrate projects every parsed ECC entry in the Corpus onto the
// corresponding bough canonical type. Returns a MigrationResult
// the caller can hand to a memory backend Store loop or to the
// internal/bootstrap.Apply writer.
//
// Confidence floor: ECC's "Confidence: 0%" / missing confidence
// values are clamped to 0.1 so they still surface as DOUBT-tier
// candidates in bough's audit log rather than being silently
// filtered.
func Migrate(c *Corpus, opts MigrateOptions) MigrationResult {
	if opts.NowFn == nil {
		opts.NowFn = time.Now
	}
	if opts.DefaultScope == "" {
		opts.DefaultScope = schema.ScopeRepo
	}
	now := opts.NowFn().UTC()
	res := MigrationResult{}

	for _, in := range c.Instincts {
		cand, ok := instinctToCandidate(in, opts, now)
		if !ok {
			res.SkippedInstincts++
			continue
		}
		res.InstinctCandidates = append(res.InstinctCandidates, cand)
	}
	for _, s := range c.Skills {
		art, ok := skillToArtifact(s, opts, now)
		if !ok {
			res.SkippedSkills++
			continue
		}
		res.SkillArtifacts = append(res.SkillArtifacts, art)
	}
	for _, a := range c.Agents {
		art, ok := agentToArtifact(a, opts, now)
		if !ok {
			res.SkippedAgents++
			continue
		}
		res.AgentArtifacts = append(res.AgentArtifacts, art)
	}
	for _, cmd := range c.Commands {
		art, ok := commandToArtifact(cmd, opts, now)
		if !ok {
			res.SkippedCommands++
			continue
		}
		res.CommandArtifacts = append(res.CommandArtifacts, art)
	}
	return res
}

func instinctToCandidate(in Instinct, opts MigrateOptions, now time.Time) (schema.InstinctCandidate, bool) {
	if strings.TrimSpace(in.ID) == "" {
		return schema.InstinctCandidate{}, false
	}
	rule := preferAction(in)
	if rule == "" {
		return schema.InstinctCandidate{}, false
	}
	conf := in.Confidence
	if conf < 0.1 {
		conf = 0.1
	}
	scope := scopeFromECC(in.Scope, opts.DefaultScope)
	cand := schema.InstinctCandidate{
		ID:         "ecc_" + in.ID,
		Rule:       rule,
		Why:        in.BodyRationale,
		HowToApply: in.Trigger,
		Domain:     domainList(in.Domain),
		Scope: schema.Scope{
			Level:    scope,
			RepoName: pickRepoName(in, opts),
		},
		Source:       schema.TraceSource(coalesce(in.Source, "ecc-import")),
		Confidence:   conf,
		State:        schema.InstinctStateCandidate,
		SourceTraces: []string{in.SourcePath},
		CreatedAt:    now,
		DedupeKey:    dedupeKey(rule, scope, in.ID),
	}
	return cand, true
}

func skillToArtifact(s Skill, opts MigrateOptions, now time.Time) (schema.CapabilityArtifact, bool) {
	if strings.TrimSpace(s.Name) == "" {
		return schema.CapabilityArtifact{}, false
	}
	payload, _ := json.Marshal(eccPayload{Body: s.Body, SourcePath: s.SourcePath})
	return schema.CapabilityArtifact{
		ID:              "ecc_skill_" + s.Name,
		Kind:            schema.ArtifactKindSkill,
		Name:            s.Name,
		Description:     s.Description,
		SourceInstincts: s.EvolvedFrom,
		Scope:           schema.Scope{Level: schema.ScopeRepo, RepoName: opts.MonorepoName},
		CreatedAt:       now,
		Payload:         payload,
		Provenance:      schema.Provenance{InstinctIDs: s.EvolvedFrom, GeneratedBy: "ecc-import@bough-v0.7.2"},
	}, true
}

func agentToArtifact(a Agent, opts MigrateOptions, now time.Time) (schema.CapabilityArtifact, bool) {
	if strings.TrimSpace(a.Name) == "" {
		return schema.CapabilityArtifact{}, false
	}
	payload, _ := json.Marshal(eccPayload{
		Body:       a.Body,
		SourcePath: a.SourcePath,
		Tools:      a.Tools,
		Model:      a.Model,
	})
	return schema.CapabilityArtifact{
		ID:          "ecc_agent_" + a.Name,
		Kind:        schema.ArtifactKindAgent,
		Name:        a.Name,
		Description: "ECC-imported agent (model=" + a.Model + ")",
		Scope:       schema.Scope{Level: schema.ScopeRepo, RepoName: opts.MonorepoName},
		CreatedAt:   now,
		Payload:     payload,
		Provenance:  schema.Provenance{GeneratedBy: "ecc-import@bough-v0.7.2"},
	}, true
}

func commandToArtifact(c Command, opts MigrateOptions, now time.Time) (schema.CapabilityArtifact, bool) {
	if strings.TrimSpace(c.Name) == "" {
		return schema.CapabilityArtifact{}, false
	}
	payload, _ := json.Marshal(eccPayload{
		Body:                c.Body,
		SourcePath:          c.SourcePath,
		EvolvedFromInstinct: c.EvolvedFromInstinct,
		Confidence:          c.Confidence,
	})
	parents := []string{}
	if c.EvolvedFromInstinct != "" {
		parents = append(parents, c.EvolvedFromInstinct)
	}
	return schema.CapabilityArtifact{
		ID:              "ecc_command_" + c.Name,
		Kind:            schema.ArtifactKindCommand,
		Name:            c.Name,
		Confidence:      c.Confidence,
		SourceInstincts: parents,
		Scope:           schema.Scope{Level: schema.ScopeRepo, RepoName: opts.MonorepoName},
		CreatedAt:       now,
		Payload:         payload,
		Provenance:      schema.Provenance{InstinctIDs: parents, GeneratedBy: "ecc-import@bough-v0.7.2"},
	}, true
}

func preferAction(in Instinct) string {
	if strings.TrimSpace(in.BodyAction) != "" {
		return in.BodyAction
	}
	if strings.TrimSpace(in.BodyTitle) != "" {
		return in.BodyTitle
	}
	return strings.TrimSpace(in.Body)
}

func scopeFromECC(s string, def schema.ScopeLevel) schema.ScopeLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "project":
		return schema.ScopeRepo
	case "personal", "global":
		return schema.ScopeGlobal
	case "inherited":
		return schema.ScopeGlobal
	case "":
		return def
	default:
		return def
	}
}

func pickRepoName(in Instinct, opts MigrateOptions) string {
	if in.ProjectName != "" {
		return in.ProjectName
	}
	return opts.MonorepoName
}

func domainList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	out := []string{}
	for _, d := range strings.Split(s, ",") {
		if dd := strings.TrimSpace(d); dd != "" {
			out = append(out, dd)
		}
	}
	return out
}

func coalesce(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func dedupeKey(rule string, scope schema.ScopeLevel, eccID string) string {
	norm := strings.ToLower(strings.TrimSpace(rule))
	h := sha256.New()
	h.Write([]byte(norm))
	h.Write([]byte{0x00})
	h.Write([]byte(string(scope)))
	h.Write([]byte{0x00})
	h.Write([]byte(eccID))
	return hex.EncodeToString(h.Sum(nil))
}
