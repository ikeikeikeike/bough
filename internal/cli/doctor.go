package cli

import (
	"github.com/spf13/cobra"
)

// newDoctorCmd wires the top-level `bough doctor` alias for
// `bough hook doctor`. Round 5 review insisted the transparency
// check (= "what is bough actually running on my behalf, and how
// much is it costing me") be reachable without remembering the
// `hook` namespace — the doctor is the operator's first stop when
// the automation surface starts to feel surprising.
//
// Both spellings render the exact same report. Future v0.7.x
// additions (= observer detail, cost meter histograms, MCP write
// rate-limit posture) land in hooks.DoctorReport so the alias and
// the namespaced command stay in lockstep.
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report bough's hook wiring + observer + cost posture (alias for `bough hook doctor`)",
		RunE: func(c *cobra.Command, _ []string) error {
			return runDoctor(c)
		},
	}
}
