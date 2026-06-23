package claudecli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/prompts"
)

func mkTpl(body string) prompts.Template {
	return prompts.Template{
		Name:    "observer",
		Source:  "test",
		Body:    body,
		Version: "test12345678",
	}
}

func TestProvider_Generate_HappyPath(t *testing.T) {
	p := NewProvider()
	p.FakeExec = func(ctx context.Context, args []string, env []string, _ io.Reader, prompt string) ([]byte, error) {
		if !strings.Contains(prompt, "PROJECT-X") {
			t.Errorf("prompt did not render data: %q", prompt)
		}
		// ensure ANTHROPIC env was scrubbed
		for _, kv := range env {
			if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
				t.Errorf("env was not sanitised: %q", kv)
			}
		}
		// ensure required Claude CLI flags are present
		mustContain := []string{"--model", "--max-turns", "--output-format", "--permission-mode"}
		for _, want := range mustContain {
			found := false
			for _, a := range args {
				if a == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("args missing %q: %v", want, args)
			}
		}
		return []byte(`{"instincts":[{"id":"x","trigger":"t","confidence":0.7,"domain":"workflow","scope":"project","action":"a","evidence":["e"]}]}`), nil
	}
	tpl := mkTpl("name={{.Name}}")
	res, err := p.Generate(context.Background(), GenerateRequest{Template: tpl, Data: map[string]string{"Name": "PROJECT-X"}})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.PromptVersion != "test12345678" {
		t.Errorf("PromptVersion = %q", res.PromptVersion)
	}
	if res.Parsed["instincts"] == nil {
		t.Errorf("parsed payload missing instincts: %+v", res.Parsed)
	}
}

func TestProvider_Generate_RetriesOnEmptyOutput(t *testing.T) {
	p := NewProvider()
	calls := 0
	p.FakeExec = func(ctx context.Context, args []string, env []string, _ io.Reader, prompt string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, ErrEmptyOutput
		}
		return []byte(`{"instincts":[]}`), nil
	}
	_, err := p.Generate(context.Background(), GenerateRequest{Template: mkTpl("x")})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected exactly 1 retry, got calls=%d", calls)
	}
}

func TestProvider_Generate_SchemaViolationIsTerminal(t *testing.T) {
	p := NewProvider()
	p.FakeExec = func(ctx context.Context, args []string, env []string, _ io.Reader, prompt string) ([]byte, error) {
		return []byte(`not json at all`), nil
	}
	_, err := p.Generate(context.Background(), GenerateRequest{Template: mkTpl("x")})
	if !errors.Is(err, ErrSchemaViolation) {
		t.Errorf("err = %v, want ErrSchemaViolation", err)
	}
}

func TestProvider_Generate_SelfDoSCap(t *testing.T) {
	p := NewProvider()
	p.Limiter.MaxCallsPerSession = 2
	p.FakeExec = func(ctx context.Context, args []string, env []string, _ io.Reader, prompt string) ([]byte, error) {
		return []byte(`{"instincts":[]}`), nil
	}
	for i := 0; i < 2; i++ {
		if _, err := p.Generate(context.Background(), GenerateRequest{Template: mkTpl("x")}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	_, err := p.Generate(context.Background(), GenerateRequest{Template: mkTpl("x")})
	if !errors.Is(err, ErrSelfDoSLimit) {
		t.Errorf("3rd call err = %v, want ErrSelfDoSLimit", err)
	}
}

func TestProvider_Generate_CircuitBreaker(t *testing.T) {
	p := NewProvider()
	p.Limiter.CircuitBreakerN = 2
	calls := 0
	p.FakeExec = func(ctx context.Context, args []string, env []string, _ io.Reader, prompt string) ([]byte, error) {
		calls++
		// always fail with a transient-looking error so the retry path runs
		return nil, errors.New("connection reset by peer")
	}
	for i := 0; i < 2; i++ {
		_, err := p.Generate(context.Background(), GenerateRequest{Template: mkTpl("x")})
		if err == nil {
			t.Fatalf("call %d should have failed", i)
		}
	}
	_, err := p.Generate(context.Background(), GenerateRequest{Template: mkTpl("x")})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("3rd call err = %v, want ErrCircuitOpen", err)
	}
}

func TestLimiter_HourWindowRolls(t *testing.T) {
	l := NewLimiter()
	l.MaxCallsPerSession = 0 // disable session cap
	l.MaxCallsPerHour = 3

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	l.SetClock(func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if err := l.Acquire(); err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
	}
	if err := l.Acquire(); !errors.Is(err, ErrSelfDoSLimit) {
		t.Errorf("4th acquire err = %v, want ErrSelfDoSLimit", err)
	}
	// advance an hour + 1 sec → window rolls
	now = now.Add(time.Hour + time.Second)
	if err := l.Acquire(); err != nil {
		t.Errorf("post-rolled window err = %v, want nil", err)
	}
}

func TestLimiter_CircuitCooldown(t *testing.T) {
	l := NewLimiter()
	l.CircuitBreakerN = 1
	l.CircuitCooldown = 10 * time.Minute

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	l.SetClock(func() time.Time { return now })
	if err := l.Acquire(); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	l.RecordFailure()
	if err := l.Acquire(); !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("circuit-open Acquire err = %v, want ErrCircuitOpen", err)
	}
	// advance past cooldown
	now = now.Add(11 * time.Minute)
	if err := l.Acquire(); err != nil {
		t.Errorf("post-cooldown Acquire err = %v", err)
	}
}

func TestProvider_Generate_PromptVersionPropagates(t *testing.T) {
	p := NewProvider()
	p.FakeExec = func(ctx context.Context, args []string, env []string, _ io.Reader, prompt string) ([]byte, error) {
		return []byte(`{"x":1}`), nil
	}
	tpl := prompts.Template{Name: "observer", Body: "hello", Version: "deadbeef00ff"}
	res, err := p.Generate(context.Background(), GenerateRequest{Template: tpl})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.PromptVersion != "deadbeef00ff" {
		t.Errorf("PromptVersion = %q", res.PromptVersion)
	}
}
