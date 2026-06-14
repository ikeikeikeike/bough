# bough

> Per-worktree isolation orchestrator for monorepos.

`bough` is a single-binary CLI that bootstraps per-worktree isolated
environments for `claude --worktree`-style workflows: a deterministically-
allocated port triplet (database / api / gateway) per branch, an
auto-generated `.env.local` in each sub-repo, and a worktree-local
database instance brought up via services-flake.

The "what to isolate" is fully declarative — pick which repositories
appear under `.worktrees/<name>/` and which database engine (MySQL,
Postgres, …) gets spawned per worktree via a single
`.worktree-isolation.yaml` in the monorepo root. Database engines are
loaded as gRPC plugins (Hashicorp go-plugin), so extending to a new
engine never requires editing the host binary.

## Status

Alpha (v0.x). MVP scope: MySQL plugin only. Postgres / Redis /
Elasticsearch plugins are tracked under the "future plugins" milestone.

## Install

```bash
# Nix flake (primary distribution channel)
nix run github:ikeikeikeike/bough -- create --stdin-json

# Or pin a release via nix profile
nix profile install github:ikeikeikeike/bough

# Homebrew (planned — once the brew tap is published)
brew tap ikeikeikeike/tap
brew install bough

# Go-native (host + plugin separately)
go install github.com/ikeikeikeike/bough/cmd/bough@latest
go install github.com/ikeikeikeike/bough/cmd/bough-plugin-mysql@latest
```

## Quick start

Drop a `.worktree-isolation.yaml` at the monorepo root that declares
which sub-repos hang off `.worktrees/<name>/` and which database
engines start per worktree:

```yaml
schema_version: 1

monorepo_root: "."

repositories:
  - name: demo-api
    branch_strategy: develop
    direnv: true
    env_local:
      DEMO_API_DSN: "root:@tcp(127.0.0.1:{{ .Mysql.Port }})/demo?parseTime=true"
      DEMO_API_URI: "grpc://0.0.0.0:{{ .Ports.api }}"

  - name: demo-dbmigration
    branch_strategy: develop
    direnv: true
    role: db-provider
    env_local:
      DEMO_DBM_PORT: "{{ .Mysql.Port }}"
    post_create:
      - "nix develop -c make migrate"

databases:
  - kind: mysql
    version: "8.4"
    port_range: [42000, 44999]
    socket_dir: "/tmp"
    initial_databases: ["demo"]

ports:
  api:    { range: [45000, 47999] }

registry:
  path: ".worktree-ports.json"
  backup_dir: "~/.claude/backups"

teardown:
  remove_branch: true
  remove_datadir: true
  graceful_timeout_sec: 10
```

Then wire it into Claude Code's WorktreeCreate / WorktreeRemove hooks
in `.claude/settings.json`:

```json
{
  "hooks": {
    "WorktreeCreate": [
      {"hooks": [{"type": "command", "command": "bough create --stdin-json"}]}
    ],
    "WorktreeRemove": [
      {"hooks": [{"type": "command", "command": "bough remove --stdin-json"}]}
    ]
  }
}
```

After that, `claude --worktree F-FeatureName` deterministically:

1. Allocates a port triplet (db / api / gateway / …) for the branch
2. Materialises every declared sub-repo via `git worktree add`
3. Spawns the configured database engine via the matching
   `bough-plugin-<kind>` gRPC plugin
4. Polls for readiness and renders each `.env.local` template
5. Runs any per-repo `post_create` hooks (migrations, seed-force, etc.)

`bough remove` (or the WorktreeRemove hook) reverses all of the above:
graceful plugin Down → lsof PID kill fallback → `git worktree remove`
per sub-repo → registry cleanup → datadir teardown.

## CLI surface

```
bough create [--config PATH] [--name NAME] [--stdin-json] [--cwd PATH]
bough remove [--config PATH] [--name NAME | --path PATH] [--stdin-json]
bough verify <worktree-name>            # registry vs declared ranges vs .env.local
bough status [--json]                   # registry + lsof TCP listen probe
bough list                              # registry table (kinds dynamic)
bough backfill                          # register pre-existing .worktrees/*
bough config validate [PATH]            # strict YAML schema check
bough plugins list                      # glob $PATH for bough-plugin-*
```

## Repository layout

```
bough/
├── cmd/
│   ├── bough/                       host CLI entrypoint
│   └── bough-plugin-mysql/          MySQL plugin entrypoint
├── internal/
│   ├── cli/                         cobra subcommands
│   ├── config/                      YAML schema (validator/v10)
│   ├── allocator/                   crc32 + linear-probing port allocator
│   ├── registry/                    .worktree-ports.json atomic R/W
│   ├── gitwt/                       `git worktree` wrapper
│   ├── envwriter/                   text/template + Sprig .env.local generator
│   ├── hooks/                       post_create / pre_remove hook runner
│   ├── mcp/                         ~/.claude.json projects upsert
│   └── pluginhost/                  go-plugin discovery + lifecycle
├── plugins/
│   └── db/
│       ├── api/                     gRPC contract + Go interface
│       └── mysql/                   MySQL provider impl + embedded services-flake
├── tests/
│   └── integration/                 real-mysqld E2E (build tag: integration)
├── flake.nix                        devShells.ci / devShells.default
├── .goreleaser.yaml                 cross-compile + GitHub Release
└── .github/workflows/               ci.yml + release.yml
```

## Contributing

Bug reports and pull requests welcome — please run `make test`,
`make lint`, and `make build` locally before opening a PR.

## License

MIT. See `LICENSE` for the full text.
