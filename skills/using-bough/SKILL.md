---
name: using-bough
description: How to drive bough from a Claude Code session — creating/tearing down per-worktree isolated dev environments (isolated MySQL/Redis/Elasticsearch per branch) and inspecting the continuous-learning instinct corpus. Use when the user wants an isolated environment for a branch, asks about bough worktrees/ports/engines, or wants to see or evolve learned instincts.
---

# Using bough from Claude Code

`bough` is a CLI that bootstraps a per-worktree isolated dev environment
(deterministic ports + its own MySQL / Redis / Elasticsearch / … engines +
a rendered `.env.local` in every sub-repo) declared in a monorepo's
`.bough.yaml`, and runs a continuous-learning loop (observe → instinct →
evolve) over Claude Code session events.

This plugin ships the commands, **not the binary**. Every command below shells
out to `bough` on `PATH`.

## Preflight: is the binary installed?

Before the first bough command in a session, confirm the binary is reachable:

```bash
command -v bough
```

If it is missing, do not guess — tell the user to install it first (GitHub
release tarball, `nix profile install github:threecorp/bough`, or `go install`;
see the bough README) and stop. The plugin's markdown commands cannot function
without it.

## When to reach for which command

| The user wants to… | Use |
|---|---|
| spin up an isolated env for a branch | `/bough:create <name>` |
| tear one down (keeps the branch) | `/bough:remove <name>` |
| see what worktrees/ports exist | `/bough:list`, `/bough:status` |
| check a worktree for drift | `/bough:verify <name>` |
| confirm hooks/observer/cost posture | `/bough:doctor` |
| see learned instincts | `/bough:instinct-status`, `/bough:instinct-list` |
| share instincts across projects | `/bough:instinct-promote` |
| turn instincts into skills/commands | `/bough:evolve` |
| validate a `.bough.yaml` | `/bough:config-validate` |

The primary way to create a worktree is still `claude --worktree <name>`, whose
`WorktreeCreate` hook this plugin also wires; `/bough:create` is for cutting one
from inside an already-running session.

## Hook wiring

Installing this plugin auto-wires bough's hook dispatcher for every event
(observe / inject / session-end / preserve / worktree create+remove). If the
user also ran `bough hook install`, those `settings.json` entries and the
plugin's hooks double-fire — run `bough hook uninstall` to keep only one. LLM
instinct minting stays opt-in (`bough observer start`); the plugin does not
enable it.
