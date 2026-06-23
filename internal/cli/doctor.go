package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/ikeikeikeike/bough/internal/homunculus"
	"github.com/ikeikeikeike/bough/internal/observe"
	"github.com/ikeikeikeike/bough/internal/provider/claudecli"
)

// newDoctorCmd wires the top-level `bough doctor` alias for
// `bough hook doctor`. Round 5 review insisted the transparency
// check (= "what is bough actually running on my behalf, and how
// much is it costing me") be reachable without remembering the
// `hook` namespace — the doctor is the operator's first stop when
// the automation surface starts to feel surprising.
//
// Both spellings render the exact same report. The v0.9 continuous-
// learning posture (= Claude CLI on PATH, Anthropic env scrub,
// LLM limiter defaults, homunculus root) is appended after the
// v0.7 hook + observer block.
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report bough's hook wiring + observer + cost posture (alias for `bough hook doctor`)",
		RunE: func(c *cobra.Command, _ []string) error {
			return runDoctor(c)
		},
	}
}

// renderContinuousLearningPosture appends the v0.9 surface to the
// doctor output. The order is:
//
//  1. Continuous-learning header
//  2. claude CLI presence (= bough observer can only call out when
//     `claude` is on PATH)
//  3. Anthropic API env scrub (= WARN when ANTHROPIC_API_KEY etc.
//     are exported in the operator's shell, even though bough
//     spawns the subprocess with them stripped — exported keys
//     still affect the operator's interactive Claude Code session)
//  4. Self-DoS cap defaults (= reminds the operator what bough will
//     refuse to do on their behalf)
//  5. Homunculus root (= where the corpus lives on disk)
func renderContinuousLearningPosture(w io.Writer) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Continuous learning (v0.9):")

	if bin, err := exec.LookPath("claude"); err == nil {
		fmt.Fprintf(w, "  claude CLI       ✓ %s\n", bin)
	} else {
		fmt.Fprintln(w, "  claude CLI       ✗ not on PATH — `bough observer run-once` will refuse to spawn until you install Claude Code")
	}

	apiVars := observe.DetectAnthropicAPIVars(os.Environ())
	if len(apiVars) == 0 {
		fmt.Fprintln(w, "  Anthropic env    ✓ no API key vars exported (subscription auth path is clean)")
	} else {
		fmt.Fprintln(w, "  Anthropic env    ⚠ exported API key vars detected — bough strips these from the subprocess env, but the operator's interactive `claude` session may still flip to API billing:")
		for _, v := range apiVars {
			fmt.Fprintf(w, "                     • %s\n", v)
		}
		fmt.Fprintln(w, "                     run `unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN` to clear")
	}

	fmt.Fprintf(w, "  Self-DoS caps    %d calls/session, %d calls/hour, %d failure breaker, %s cooldown\n",
		claudecli.DefaultMaxCallsPerSession,
		claudecli.DefaultMaxCallsPerHour,
		claudecli.DefaultCircuitBreakerN,
		claudecli.DefaultCircuitCooldown,
	)

	layout := homunculus.NewLayout()
	if _, err := os.Stat(layout.Root); err == nil {
		fmt.Fprintf(w, "  homunculus root  ✓ %s\n", layout.Root)
	} else {
		fmt.Fprintf(w, "  homunculus root  · %s (will be created on first `bough observer run-once`)\n", layout.Root)
	}
}
