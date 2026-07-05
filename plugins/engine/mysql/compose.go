//go:build darwin || linux

// Compose backend for the MySQL plugin. Selected by `backend: compose` in
// .bough.yaml. Instead of bringing up its own mysqld (nix or docker SDK),
// bough adopts the project's existing docker-compose service: it allocates
// the per-worktree host port as usual, then renders a derived compose file
// that retargets that one service's published port and runs it under a
// per-worktree compose project name (which isolates the named-volume
// datadir). The compose file stays the source of truth for image, volume,
// healthcheck, and env. See pkg/composeutil for the mechanics.
//
// Required extras: compose.file (path to the compose file, resolved by the
// host against the worktree root), compose.service (the service to bring
// up, e.g. "db"), compose.target_port (the container-internal port, e.g.
// "3306").
package mysql

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"time"

	api "github.com/ikeikeikeike/bough/plugins/engine/api"

	"github.com/ikeikeikeike/bough/pkg/composeutil"
)

const composeEngineKind = "mysql"

// usingComposeBackend detects a compose-backed engine on `port` by probing
// for a container labeled with this engine's per-worktree compose project.
// ReadyCheck / Down only receive the port (no Extras), so — like
// usingDockerBackend's name probe — detection has to be probe-based.
func usingComposeBackend(ctx context.Context, port int) bool {
	if port <= 0 {
		return false
	}
	return composeutil.Running(ctx, composeutil.Project(composeEngineKind, port))
}

// composeDerivedPath puts the rendered compose file beside the datadir
// under <worktree>/.local, keyed by kind so sibling engines don't collide.
func composeDerivedPath(req *api.UpReq) string {
	return filepath.Join(filepath.Dir(req.Datadir), "bough-compose-"+composeEngineKind+".yml")
}

func (p *Provider) composeUp(ctx context.Context, req *api.UpReq) error {
	port := api.PickMainPort(req.Ports)
	if port <= 0 {
		return fmt.Errorf("mysql compose: invalid port %d (Ports=%v)", port, req.Ports)
	}
	file := req.Extras["compose.file"]
	service := req.Extras["compose.service"]
	target := req.Extras["compose.target_port"]
	if file == "" || service == "" || target == "" {
		return fmt.Errorf("mysql compose: backend=compose requires extras "+
			"compose.file / compose.service / compose.target_port (got file=%q service=%q target=%q)",
			file, service, target)
	}
	derived := composeDerivedPath(req)
	if err := composeutil.Render(file, service, target, port, derived); err != nil {
		return err
	}
	// up --wait blocks on the compose file's own healthcheck — the correct
	// readiness signal (a naive host-TCP dial would go green during the
	// mysql:8.4 temporary-server phase).
	return composeutil.Up(ctx, derived, composeutil.Project(composeEngineKind, port), service, 0)
}

func (p *Provider) composeReadyCheck(ctx context.Context, port, timeoutSec int) (bool, error) {
	if timeoutSec <= 0 {
		timeoutSec = 600
	}
	project := composeutil.Project(composeEngineKind, port)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}
		if composeutil.Running(ctx, project) {
			if conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
				_ = conn.Close()
				return true, nil
			}
		}
		time.Sleep(dockerReadyPollMS * time.Millisecond)
	}
	return false, fmt.Errorf("mysql compose: not ready on port %d within %ds", port, timeoutSec)
}

func (p *Provider) composeDown(ctx context.Context, req *api.DownReq) error {
	return composeutil.Down(ctx, composeutil.Project(composeEngineKind, firstListenPort(req.Ports)))
}

// composeCleanupVolumes removes the per-worktree compose project's named
// volumes (the compose datadir). It is a no-op for the nix / docker
// backends — they create no such compose project — so Cleanup can call it
// unconditionally without a backend flag it does not receive.
func composeCleanupVolumes(ctx context.Context, ports []int) {
	if port := firstListenPort(ports); port > 0 {
		_ = composeutil.RemoveVolumes(ctx, composeutil.Project(composeEngineKind, port))
	}
}
