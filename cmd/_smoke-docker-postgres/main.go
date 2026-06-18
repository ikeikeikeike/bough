// _smoke-docker-postgres exercises the postgres plugin's Docker
// backend end-to-end without going through the bough host
// orchestrator. See cmd/_smoke-docker-mysql for the rationale.
package main

import (
	"log"
	"time"

	"github.com/ikeikeikeike/bough/internal/smoketool"
	pgplugin "github.com/ikeikeikeike/bough/plugins/engine/postgres"
)

func main() {
	if err := smoketool.Run(pgplugin.New(), smoketool.Config{
		Engine:          "postgres",
		Port:            43502,
		InitialDB:       "demo",
		ReadyTimeoutSec: 180,
		DownTimeoutSec:  15,
		ReadyPause:      2 * time.Second,
	}); err != nil {
		log.Fatalf("smoke: %v", err)
	}
}
