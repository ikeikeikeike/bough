// _smoke-docker-elasticsearch exercises the elasticsearch plugin's
// Docker backend end-to-end without going through the bough host
// orchestrator. ReadyTimeout is generous (300s) to cover the JVM
// cold-start on a busy laptop. See cmd/_smoke-docker-mysql for the
// rationale on the underscore prefix and the smoketool package split.
package main

import (
	"log"
	"time"

	"github.com/ikeikeikeike/bough/internal/smoketool"
	esplugin "github.com/ikeikeikeike/bough/plugins/engine/elasticsearch"
)

func main() {
	if err := smoketool.Run(esplugin.New(), smoketool.Config{
		Engine:          "elasticsearch",
		Port:            43504,
		ReadyTimeoutSec: 300,
		DownTimeoutSec:  60,
		ReadyPause:      2 * time.Second,
	}); err != nil {
		log.Fatalf("smoke: %v", err)
	}
}
