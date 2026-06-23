package judge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

func TestReplayJudge_Hit(t *testing.T) {
	dir := t.TempDir()
	req := api.JudgeRequest{
		PromptVersion:       "v3",
		ModelID:             "claude-opus-4-7",
		ClusterMemberIDs:    []string{"a", "b"},
		ClusterMemberHashes: []string{"h0", "h1"},
	}
	want := api.JudgeVerdict{
		Verdict:          api.VerdictPass,
		Confidence:       0.85,
		Reason:           "fixture verdict",
		RecommendedLabel: "io-lives-in-data-layer",
		TimestampUTC:     "2026-06-23T00:00:00Z",
	}
	record := struct {
		Verdict api.JudgeVerdict `json:"verdict"`
	}{Verdict: want}
	buf, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(dir, cacheKey(req)+".json"), buf, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	r := NewReplayJudgeClient(dir, true)
	got, err := r.Judge(context.Background(), req)
	if err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReplayJudge_MissReturnsSentinel(t *testing.T) {
	dir := t.TempDir()
	r := NewReplayJudgeClient(dir, true)
	req := api.JudgeRequest{
		PromptVersion:       "v3",
		ModelID:             "claude-opus-4-7",
		ClusterMemberIDs:    []string{"missing"},
		ClusterMemberHashes: []string{"h0"},
	}
	_, err := r.Judge(context.Background(), req)
	if !errors.Is(err, ErrReplayMiss) {
		t.Errorf("error = %v, want errors.Is(ErrReplayMiss)", err)
	}
}

func TestReplayJudge_BadJSON(t *testing.T) {
	dir := t.TempDir()
	req := api.JudgeRequest{
		PromptVersion: "v3",
		ModelID:       "m",
	}
	if err := os.WriteFile(filepath.Join(dir, cacheKey(req)+".json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	r := NewReplayJudgeClient(dir, true)
	_, err := r.Judge(context.Background(), req)
	if err == nil {
		t.Errorf("expected parse error, got nil")
	}
	if errors.Is(err, ErrReplayMiss) {
		t.Errorf("parse error should not match ErrReplayMiss: %v", err)
	}
}
