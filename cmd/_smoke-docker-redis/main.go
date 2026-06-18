// _smoke-docker-redis exercises the redis plugin's Docker backend
// end-to-end without going through the bough host orchestrator. See
// cmd/_smoke-docker-mysql for the rationale.
package main

import (
	"log"
	"time"

	"github.com/ikeikeikeike/bough/internal/smoketool"
	redisplugin "github.com/ikeikeikeike/bough/plugins/engine/redis"
)

func main() {
	if err := smoketool.Run(redisplugin.New(), smoketool.Config{
		Engine:          "redis",
		Port:            43503,
		ReadyTimeoutSec: 60,
		DownTimeoutSec:  5,
		ReadyPause:      2 * time.Second,
	}); err != nil {
		log.Fatalf("smoke: %v", err)
	}
}
