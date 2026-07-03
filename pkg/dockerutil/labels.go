//go:build darwin || linux

package dockerutil

import "fmt"

// Label keys for the com.bough.* schema. Exported so external tooling
// (a future `bough doctor` / `bough ps`) can grep by symbol instead of
// magic strings.
const (
	LabelManaged  = "com.bough.managed"
	LabelEngine   = "com.bough.engine"
	LabelImage    = "com.bough.image"
	LabelHostPort = "com.bough.host-port"
)

// Labels returns the canonical com.bough.* label set used to tag every
// bough-managed container. The `engine` argument is the plugin-specific
// identifier (= "mysql" / "postgres" / "redis" / "elasticsearch") and is
// the only knob the per-plugin caller needs to provide; the rest are
// derived from the image reference + published host port.
//
// Currently write-only: every plugin discovers its container purely
// by exact name (bough-<engine>-<port> via LookupByName), never by
// listing on these labels. They exist so a future label-based
// discovery tool (`bough doctor` / `bough ps`) doesn't have to
// retrofit tagging onto every already-running container — schema
// stability matters for THAT reason, not because anything reads them
// back today.
func Labels(engine, imageRef string, port int) map[string]string {
	return map[string]string{
		LabelManaged:  "true",
		LabelEngine:   engine,
		LabelImage:    imageRef,
		LabelHostPort: fmt.Sprintf("%d", port),
	}
}
