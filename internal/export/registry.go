package export

import (
	"github.com/ikeikeikeike/bough/internal/capability"
)

// DefaultRegistry wires the three v0.6 builtin emitters (agent-
// skill / claude-skill / mcp) into a fresh capability.Registry.
// CLI bootstrap calls this so `bough capability compile` works out
// of the box with the round 4 priority A2 + A6 default target set.
//
// Plugin authors can wrap the returned registry and Register
// additional emitters before handing it to a capability.Compiler
// — the registry is mutable so swapping in a SkillX adapter from
// examples/ does not require forking the bootstrap path.
func DefaultRegistry() *capability.Registry {
	r := capability.NewRegistry()
	r.Register(AgentSkillEmitter{})
	r.Register(ClaudeSkillEmitter{})
	r.Register(MCPEmitter{})
	return r
}
