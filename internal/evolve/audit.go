package evolve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// AuditRecord is what gets serialised to
// .evolve/judgements/<cache_key>.json. The schema doubles as the
// ReplayJudgeClient fixture format: any audit record can be
// hand-edited into a replay corpus without translation.
//
// RawResponse is the verbatim string the LLM returned (= empty for
// HeuristicJudgeClient runs). ParsedAt is when the host parsed the
// verdict, in case the LLM's TimestampUTC is unreliable.
type AuditRecord struct {
	CacheKey     string           `json:"cache_key"`
	Request      api.JudgeRequest `json:"request"`
	Verdict      api.JudgeVerdict `json:"verdict"`
	RawResponse  string           `json:"raw_response,omitempty"`
	JudgeName    string           `json:"judge_name"`
	ParsedAt     string           `json:"parsed_at"`
	PromptPath   string           `json:"prompt_path,omitempty"`
}

// AuditDir wraps the on-disk .evolve/ directory. The struct is
// safe for concurrent use across goroutines; writes are serialised
// behind a mutex so two judge calls completing simultaneously do
// not corrupt the JSON file.
type AuditDir struct {
	root string
	mu   sync.Mutex
}

// NewAuditDir returns an AuditDir rooted at the given path. The
// directory tree is lazily created on the first WriteRecord call.
func NewAuditDir(root string) *AuditDir {
	return &AuditDir{root: root}
}

// JudgementsDir is the on-disk directory AuditDir reads / writes
// audit records under. ReplayJudgeClient reads from the same
// directory.
func (a *AuditDir) JudgementsDir() string { return filepath.Join(a.root, "judgements") }

// PromptsDir is the on-disk directory snapshot prompt templates
// land in. The PromptVersion key in JudgeRequest is the filename
// stem (= "v3-2026-06-23" → "v3-2026-06-23.txt").
func (a *AuditDir) PromptsDir() string { return filepath.Join(a.root, "prompts") }

// WriteRecord persists an AuditRecord. The file path is
// `<root>/judgements/<cache_key>.json`. Write is atomic (= write to
// .tmp then rename) so a half-flushed record never appears on disk.
//
// Idempotent: writing the same record twice produces the same
// content. Callers can safely retry after a transient error.
func (a *AuditDir) WriteRecord(rec AuditRecord) error {
	if rec.CacheKey == "" {
		return errors.New("audit.WriteRecord: empty CacheKey")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	dir := a.JudgementsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("audit.WriteRecord: mkdir %s: %w", dir, err)
	}
	target := filepath.Join(dir, rec.CacheKey+".json")
	tmp := target + ".tmp"
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("audit.WriteRecord: marshal: %w", err)
	}
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("audit.WriteRecord: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("audit.WriteRecord: rename %s → %s: %w", tmp, target, err)
	}
	return nil
}

// ReadRecord loads an AuditRecord by cache key. Returns os.ErrNotExist
// when the record is missing — callers distinguish via errors.Is.
func (a *AuditDir) ReadRecord(cacheKey string) (AuditRecord, error) {
	path := filepath.Join(a.JudgementsDir(), cacheKey+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return AuditRecord{}, err
	}
	var rec AuditRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return AuditRecord{}, fmt.Errorf("audit.ReadRecord: parse %s: %w", path, err)
	}
	return rec, nil
}

// CachedJudge wraps a JudgeClient with read-through audit caching.
// On Judge():
//
//  1. compute cache_key = CacheKey(req)
//  2. if .evolve/judgements/<cache_key>.json exists → return it
//  3. else delegate to inner.Judge(), persist the audit record,
//     return the verdict.
//
// JudgeRequest.Temperature is overwritten to 0 and MaxOutputTokens
// to 1024 inside Judge() so callers cannot bypass the determinism
// invariant.
type CachedJudge struct {
	inner api.JudgeClient
	audit *AuditDir
	now   func() time.Time
}

// NewCachedJudge wires an audit-backed cache around any JudgeClient.
// inner must not be nil; audit must be non-nil to persist records.
// If audit is nil the wrapper is a pass-through.
func NewCachedJudge(inner api.JudgeClient, audit *AuditDir) *CachedJudge {
	return &CachedJudge{inner: inner, audit: audit, now: time.Now}
}

// SetClock pins the clock used for ParsedAt in audit records.
// Tests pin this for golden diff stability.
func (c *CachedJudge) SetClock(now func() time.Time) {
	c.now = now
}

// Name returns the inner judge's name so audit records record the
// real backend, not the wrapper.
func (c *CachedJudge) Name() string {
	return c.inner.Name()
}

// Judge implements the read-through cache documented on CachedJudge.
func (c *CachedJudge) Judge(ctx context.Context, req api.JudgeRequest) (api.JudgeVerdict, error) {
	req.Temperature = 0
	if req.MaxOutputTokens == 0 {
		req.MaxOutputTokens = 1024
	}
	key := CacheKey(req)

	if c.audit != nil {
		if rec, err := c.audit.ReadRecord(key); err == nil {
			return rec.Verdict, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return api.JudgeVerdict{}, fmt.Errorf("audit cache read: %w", err)
		}
	}

	verdict, err := c.inner.Judge(ctx, req)
	if err != nil {
		return verdict, err
	}
	if err := ValidateVerdict(verdict); err != nil {
		return verdict, fmt.Errorf("judge %s returned invalid verdict: %w", c.inner.Name(), err)
	}
	if c.audit != nil {
		rec := AuditRecord{
			CacheKey:  key,
			Request:   req,
			Verdict:   verdict,
			JudgeName: c.inner.Name(),
			ParsedAt:  c.now().UTC().Format(time.RFC3339Nano),
		}
		if err := c.audit.WriteRecord(rec); err != nil {
			// Audit failure must not break the pipeline; surface
			// to the operator via stderr in the CLI layer and
			// keep the verdict.
			return verdict, fmt.Errorf("audit write (verdict preserved): %w", err)
		}
	}
	return verdict, nil
}
