// Package smoketool drives one full Up → ReadyCheck → Down → Cleanup
// lifecycle against a bough engine plugin's Docker backend, with
// uniform timing + log output. It is the shared body the
// cmd/_smoke-docker-<kind>/ binaries call so each remains a ~15-line
// main() that only spells out its plugin and per-engine tunables.
//
// Underscore prefix on the binary names keeps them out of `go build
// ./...` and the GoReleaser archive — they exist for ad-hoc local
// validation during plugin work, not for shipping.
package smoketool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	api "github.com/ikeikeikeike/bough/plugins/engine/api"
)

// Config drives one smoke run.
type Config struct {
	// Engine names the plugin under test (e.g. "mysql"). Surfaces in
	// log labels and the container-name banner.
	Engine string

	// Port is the host port the plugin is asked to bind to. Pick one
	// outside any running worktree's allocation range to avoid
	// collisions with the daily-driver bough environment.
	Port int

	// InitialDB is the first database name to provision (passed as an
	// InitialResource of type "database"). Plugins that ignore
	// InitialResources (redis, elasticsearch) leave this empty.
	InitialDB string

	// ReadyTimeoutSec bounds the ReadyCheck poll loop. Elasticsearch
	// wants ~300 (JVM warmup); mysql ~180; redis ~60.
	ReadyTimeoutSec int

	// DownTimeoutSec is the graceful-shutdown budget for Down. ES and
	// mysql want 30-60 (translog/InnoDB flush); redis ~5.
	DownTimeoutSec int

	// ReadyPause is how long Run sleeps between a successful
	// ReadyCheck and the Down phase — gives the operator a window to
	// poke the running engine (`mysql -uroot -h127.0.0.1`,
	// `redis-cli ping`, …). Zero disables the pause.
	ReadyPause time.Duration
}

// Run drives the full lifecycle once and returns when Cleanup
// finishes. A ReadyCheck failure tries a best-effort Down + Cleanup
// before returning the error, so the caller never leaks a stranded
// container.
func Run(prov api.EngineProvider, cfg Config) error {
	if cfg.Engine == "" {
		return errors.New("smoketool: Config.Engine is required")
	}
	if cfg.Port <= 0 {
		return errors.New("smoketool: Config.Port is required")
	}

	datadir, err := os.MkdirTemp("", "bough-smoke-"+cfg.Engine+"-")
	if err != nil {
		return fmt.Errorf("tempdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(datadir) }()
	log.Printf("datadir: %s", datadir)

	ctx := context.Background()
	ports := []int{cfg.Port}
	upReq := &api.UpReq{
		Ports:            []api.PortSpec{{Role: "main", Port: cfg.Port}},
		Datadir:          datadir,
		InitialResources: initialResources(cfg.InitialDB),
		Extras:           map[string]string{"backend": "docker"},
	}
	downReq := &api.DownReq{Ports: ports, GracefulTimeoutSec: cfg.DownTimeoutSec}

	overall := time.Now()

	if err := phase("Up", func() error { return prov.Up(ctx, upReq) }); err != nil {
		return fmt.Errorf("up: %w", err)
	}

	readyLabel := fmt.Sprintf("ReadyCheck (timeout %ds)", cfg.ReadyTimeoutSec)
	if err := phase(readyLabel, func() error {
		ready, err := prov.ReadyCheck(ctx, ports, cfg.ReadyTimeoutSec)
		if err != nil {
			return err
		}
		if !ready {
			return fmt.Errorf("not-ready within %ds", cfg.ReadyTimeoutSec)
		}
		return nil
	}); err != nil {
		// Best-effort teardown — keeps a failed smoke from leaving a
		// container running on the operator's machine.
		log.Printf("ReadyCheck FAILED — tearing down before exit: %v", err)
		_ = prov.Down(ctx, downReq)
		_ = prov.Cleanup(ctx, datadir, ports)
		return err
	}

	log.Printf("*** %s bough-%s-%d UP+READY in %s ***",
		cfg.Engine, cfg.Engine, cfg.Port, time.Since(overall))

	if cfg.ReadyPause > 0 {
		time.Sleep(cfg.ReadyPause)
	}

	if err := phase("Down", func() error { return prov.Down(ctx, downReq) }); err != nil {
		return fmt.Errorf("down: %w", err)
	}
	if err := phase("Cleanup", func() error { return prov.Cleanup(ctx, datadir, ports) }); err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	log.Printf("*** TOTAL CYCLE: %s ***", time.Since(overall))
	return nil
}

// phase wraps fn with a uniform "=== <name> ===" / "<name>: <duration>"
// log banner so every smoke run looks the same when scrolled.
func phase(name string, fn func() error) error {
	log.Printf("=== %s ===", name)
	start := time.Now()
	if err := fn(); err != nil {
		return err
	}
	log.Printf("%s: %s", name, time.Since(start))
	return nil
}

func initialResources(dbName string) []api.ResourceSpec {
	if dbName == "" {
		return nil
	}
	return []api.ResourceSpec{{Type: "database", Name: dbName}}
}
