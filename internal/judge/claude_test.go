package judge

import (
	"context"
	"errors"
	"testing"

	api "github.com/ikeikeikeike/bough/plugins/capability/api"
)

func TestClaudeJudge_ReturnsNotWired(t *testing.T) {
	c := NewClaudeJudgeClient("claude-opus-4-7")
	_, err := c.Judge(context.Background(), api.JudgeRequest{})
	if !errors.Is(err, ErrClaudeNotWired) {
		t.Errorf("Judge() error = %v, want errors.Is(ErrClaudeNotWired)", err)
	}
}

func TestClaudeJudge_Name(t *testing.T) {
	c := NewClaudeJudgeClient("claude-opus-4-7")
	if got := c.Name(); got != "claude" {
		t.Errorf("Name() = %q, want %q", got, "claude")
	}
}
