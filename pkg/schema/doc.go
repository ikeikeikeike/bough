// Package schema declared the canonical Go types for the v0.5-v0.8
// memory-orchestration subsystem (observer → redaction →
// poisoning_guard → coordinator → MemoryBackend plugin). That
// subsystem — and the plugins/{memory,instinct,capability,evaluator}
// packages whose api/types.go files these types mirrored — was
// superseded wholesale in v0.9.0. This package has no importers left
// in the current codebase; it stays only as a historical artifact of
// that design (the types themselves are still valid Go, just unused).
package schema
