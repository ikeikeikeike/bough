// _smoke-docker-mysql exercises the mysql plugin's Docker backend
// end-to-end without going through the bough host orchestrator. Use
// it to validate Up → ReadyCheck → Down → Cleanup against a real
// Docker daemon during plugin work. Underscore prefix on the cmd
// dir keeps it out of `go build ./...` and the GoReleaser archive.
package main

import (
	"log"
	"time"

	"github.com/ikeikeikeike/bough/internal/smoketool"
	mysqlplugin "github.com/ikeikeikeike/bough/plugins/engine/mysql"
)

func main() {
	if err := smoketool.Run(mysqlplugin.New(), smoketool.Config{
		Engine:          "mysql",
		Port:            43501,
		InitialDB:       "demo",
		ReadyTimeoutSec: 180,
		DownTimeoutSec:  30,
		ReadyPause:      2 * time.Second,
	}); err != nil {
		log.Fatalf("smoke: %v", err)
	}
}
