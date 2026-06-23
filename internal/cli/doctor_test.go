package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRenderContinuousLearningPosture_LimitsLine(t *testing.T) {
	var buf bytes.Buffer
	renderContinuousLearningPosture(&buf)
	out := buf.String()
	for _, want := range []string{
		"Continuous learning (v0.9):",
		"claude CLI",
		"Anthropic env",
		"Self-DoS caps",
		"calls/session",
		"calls/hour",
		"homunculus root",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("posture output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderContinuousLearningPosture_WarnsOnExportedAPIKey(t *testing.T) {
	prev := os.Getenv("ANTHROPIC_API_KEY")
	defer func() {
		if prev == "" {
			_ = os.Unsetenv("ANTHROPIC_API_KEY")
		} else {
			_ = os.Setenv("ANTHROPIC_API_KEY", prev)
		}
	}()
	_ = os.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	var buf bytes.Buffer
	renderContinuousLearningPosture(&buf)
	out := buf.String()
	if !strings.Contains(out, "exported API key vars detected") {
		t.Errorf("doctor did not warn on exported ANTHROPIC_API_KEY:\n%s", out)
	}
	if !strings.Contains(out, "ANTHROPIC_API_KEY") {
		t.Errorf("doctor did not name the offending variable:\n%s", out)
	}
}

func TestRenderContinuousLearningPosture_CleanEnv(t *testing.T) {
	keys := []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL"}
	saved := map[string]string{}
	for _, k := range keys {
		saved[k] = os.Getenv(k)
		_ = os.Unsetenv(k)
	}
	defer func() {
		for k, v := range saved {
			if v == "" {
				continue
			}
			_ = os.Setenv(k, v)
		}
	}()

	var buf bytes.Buffer
	renderContinuousLearningPosture(&buf)
	out := buf.String()
	if !strings.Contains(out, "subscription auth path is clean") {
		t.Errorf("doctor did not say subscription path is clean:\n%s", out)
	}
}
