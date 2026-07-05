//go:build darwin || linux

// Package composeutil is the shared helper behind the engine plugins'
// `backend: compose` path. It lets bough ADOPT a project's existing
// docker-compose file as the runtime for an engine: bough keeps owning
// per-worktree port allocation + lifecycle, while the compose file stays
// the source of truth for image, volumes, healthcheck, and env.
//
// The two hard problems it solves:
//
//   - Port override without editing the user's file. Compose's own
//     `-f base -f override` merge CONCATENATES the `ports` list rather than
//     replacing it, so an override can't retarget a hardcoded `3306:3306`.
//     Render() instead reads the file, rewrites the one service's published
//     port to `127.0.0.1:<boughPort>:<target>`, and writes a derived file
//     bough runs on its own — everything else copied verbatim.
//
//   - Per-worktree datadir isolation. A compose named volume (`dbstore`)
//     is global to its compose project, so two worktrees running the same
//     file would SHARE a datadir. Every op is scoped to a per-worktree
//     project name (`bough-<kind>-<port>`), which namespaces both the
//     containers and the named volumes — verified in the PoC: two projects
//     get `bough-<a>_dbstore` vs `bough-<b>_dbstore`, fully isolated.
//
// Up shells out to the `docker compose` CLI (v2) so bough does not
// reimplement compose's orchestration. Teardown (Down / RemoveVolumes /
// Running) uses the Docker SDK filtered by the compose project label, so
// it needs neither the derived file nor the CLI and is robust to either
// being absent at cleanup time.
package composeutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"gopkg.in/yaml.v3"

	"github.com/ikeikeikeike/bough/pkg/dockerutil"
)

// composeProjectLabel is the standard label docker compose v2 stamps on
// every container / network / volume it creates, keyed by project name.
const composeProjectLabel = "com.docker.compose.project"

// Project returns the deterministic per-worktree compose project name for
// an engine on its allocated host port. The port is worktree-unique (the
// bough allocator guarantees it), so the project name is too — which is
// exactly what namespaces the named volumes apart between worktrees.
func Project(kind string, port int) string {
	return fmt.Sprintf("bough-%s-%d", kind, port)
}

// Render reads the user's compose file, rewrites `service`'s published
// ports to a single `127.0.0.1:<hostPort>:<targetPort>` mapping (bind
// 127.0.0.1 only — the per-worktree engine is dev-only, never 0.0.0.0),
// and writes the derived file to dst. Every other key (image, volumes,
// healthcheck, environment, command) is copied verbatim, so the compose
// file stays the source of truth for everything except the host port.
func Render(composeFile, service, targetPort string, hostPort int, dst string) error {
	raw, err := os.ReadFile(composeFile)
	if err != nil {
		return fmt.Errorf("compose: read %s: %w", composeFile, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("compose: parse %s: %w", composeFile, err)
	}
	servicesAny, ok := doc["services"]
	if !ok {
		return fmt.Errorf("compose: %s has no `services:` block", composeFile)
	}
	services, ok := servicesAny.(map[string]any)
	if !ok {
		return fmt.Errorf("compose: %s `services:` is not a mapping", composeFile)
	}
	svcAny, ok := services[service]
	if !ok {
		return fmt.Errorf("compose: %s has no service %q", composeFile, service)
	}
	svc, ok := svcAny.(map[string]any)
	if !ok {
		return fmt.Errorf("compose: service %q is not a mapping", service)
	}
	svc["ports"] = []any{fmt.Sprintf("127.0.0.1:%d:%s", hostPort, targetPort)}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("compose: re-marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("compose: mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		return fmt.Errorf("compose: write %s: %w", dst, err)
	}
	return nil
}

// Up runs `docker compose -p <project> -f <derived> up -d <service>
// --wait`. `--wait` blocks until the service is healthy per the compose
// file's own healthcheck — which is the correct readiness signal (a naive
// host-TCP dial would go green during the mysql:8.4 temporary-server
// phase). `-p` sets the project name that isolates the named volumes.
func Up(ctx context.Context, derivedFile, project, service string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 600 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", "compose",
		"-p", project, "-f", derivedFile, "up", "-d", service, "--wait")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compose: up %s (project %s): %w: %s", service, project, err, out)
	}
	return nil
}

// Running reports whether any container labeled for `project` exists (the
// cheap backend-detection probe ReadyCheck / Down use — the compose
// analogue of dockerutil.IsBackendRunning's name probe). A stopped
// container still counts as "this project is the compose backend"; the
// caller distinguishes liveness separately.
func Running(ctx context.Context, project string) bool {
	cli, err := dockerutil.NewClient()
	if err != nil {
		return false
	}
	defer func() { _ = cli.Close() }()
	args := filters.NewArgs()
	args.Add("label", composeProjectLabel+"="+project)
	list, err := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return false
	}
	return len(list) > 0
}

// Down stops and removes the project's containers (label-scoped via the
// SDK, so no compose file is needed at teardown). Networks compose
// created for the project are best-effort pruned afterwards. Volumes are
// left intact — that is RemoveVolumes' job, mirroring the docker
// backend's Down-stops / Cleanup-deletes split.
func Down(ctx context.Context, project string) error {
	cli, err := dockerutil.NewClient()
	if err != nil {
		return err
	}
	defer func() { _ = cli.Close() }()

	args := filters.NewArgs()
	args.Add("label", composeProjectLabel+"="+project)
	list, err := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return fmt.Errorf("compose: list containers for %s: %w", project, err)
	}
	for _, c := range list {
		timeoutSec := 30
		_ = cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeoutSec})
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("compose: remove container %s: %w", c.ID, err)
		}
	}
	// Best-effort: prune the project's default network so a re-up doesn't
	// warn about a dangling network. A leftover network is harmless, so
	// failures here are ignored.
	nargs := filters.NewArgs()
	nargs.Add("label", composeProjectLabel+"="+project)
	if nets, nerr := cli.NetworkList(ctx, network.ListOptions{Filters: nargs}); nerr == nil {
		for _, n := range nets {
			_ = cli.NetworkRemove(ctx, n.ID)
		}
	}
	return nil
}

// RemoveVolumes deletes the project's named volumes — the compose
// analogue of deleting a datadir. Call after Down (a volume in use by a
// live container cannot be removed).
func RemoveVolumes(ctx context.Context, project string) error {
	cli, err := dockerutil.NewClient()
	if err != nil {
		return err
	}
	defer func() { _ = cli.Close() }()
	args := filters.NewArgs()
	args.Add("label", composeProjectLabel+"="+project)
	vols, err := cli.VolumeList(ctx, volume.ListOptions{Filters: args})
	if err != nil {
		return fmt.Errorf("compose: list volumes for %s: %w", project, err)
	}
	for _, v := range vols.Volumes {
		if err := cli.VolumeRemove(ctx, v.Name, true); err != nil {
			return fmt.Errorf("compose: remove volume %s: %w", v.Name, err)
		}
	}
	return nil
}
