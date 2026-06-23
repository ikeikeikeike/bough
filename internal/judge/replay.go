package judge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

// ReplayJudgeClient replays prerecorded JudgeVerdicts from a fixture
// directory. The directory layout matches `.evolve/judgements/`:
//
//	<root>/<cache_key>.json   = AuditRecord JSON (see internal/evolve/audit.go)
//
// Cache misses return ErrReplayMiss so test harnesses can decide
// whether to fail (= strict reproducibility) or fall through to a
// HeuristicJudgeClient (= permissive replay).
//
// The replay client is the cornerstone of the v0.7.1 golden corpus
// test: the corpus records ClaudeJudgeClient verdicts once, commits
// them, and every subsequent CI run replays the cassette so the
// pipeline stays byte-stable without re-billing the operator.
type ReplayJudgeClient struct {
	root   string
	strict bool
}

// ErrReplayMiss is returned when no fixture matches the request's
// cache key. Callers in strict mode propagate it; permissive mode
// callers chain into a fallback JudgeClient.
var ErrReplayMiss = errors.New("replay miss: no fixture for cache key")

// NewReplayJudgeClient returns a ReplayJudgeClient rooted at the
// given directory. When strict is true the client returns
// ErrReplayMiss verbatim on cache miss; when false the caller can
// distinguish a miss from a parse error via errors.Is.
func NewReplayJudgeClient(root string, strict bool) *ReplayJudgeClient {
	return &ReplayJudgeClient{root: root, strict: strict}
}

// Name returns "replay" — the canonical backend name used in CLI
// flags and audit records.
func (r *ReplayJudgeClient) Name() string { return "replay" }

// Judge looks up the JudgeVerdict for req's cache key under root.
// The on-disk schema is the same AuditRecord internal/evolve/audit.go
// writes, so the same file can serve both audit and replay roles.
func (r *ReplayJudgeClient) Judge(_ context.Context, req api.JudgeRequest) (api.JudgeVerdict, error) {
	key := cacheKey(req)
	path := filepath.Join(r.root, key+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return api.JudgeVerdict{}, fmt.Errorf("%w: %s", ErrReplayMiss, key)
		}
		return api.JudgeVerdict{}, fmt.Errorf("read fixture %s: %w", path, err)
	}
	var rec struct {
		Verdict api.JudgeVerdict `json:"verdict"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return api.JudgeVerdict{}, fmt.Errorf("parse fixture %s: %w", path, err)
	}
	return rec.Verdict, nil
}

// cacheKey mirrors internal/evolve/cache.go so the replay client
// can be unit-tested without importing the evolve package and
// creating a cycle. Keep the two implementations in sync; the
// golden corpus test cross-checks them.
func cacheKey(req api.JudgeRequest) string {
	h := sha256.New()
	h.Write([]byte(req.PromptVersion))
	h.Write([]byte{0x00})
	h.Write([]byte(req.ModelID))
	h.Write([]byte{0x00})
	for _, id := range req.ClusterMemberIDs {
		h.Write([]byte(id))
		h.Write([]byte{0x1F})
	}
	h.Write([]byte{0x00})
	for _, hash := range req.ClusterMemberHashes {
		h.Write([]byte(hash))
		h.Write([]byte{0x1F})
	}
	h.Write([]byte{0x00})
	h.Write([]byte(req.NearestPriorLabel))
	h.Write([]byte{0x00})
	h.Write([]byte(req.NearestPriorDesc))
	return hex.EncodeToString(h.Sum(nil))
}
