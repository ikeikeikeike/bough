---
description: Create a bough per-worktree isolated dev environment (spins up its DB/engines + writes each sub-repo's .env.local).
argument-hint: <worktree-name>
allowed-tools: Bash
---

Create a bough worktree named `$ARGUMENTS`.

If `$ARGUMENTS` is empty, ask the user for the worktree name (e.g. `F-Feature`) and stop.

bough's `create` reads the same WorktreeCreate payload the `claude --worktree`
hook sends, so feed it on stdin from the current monorepo checkout:

```bash
printf '{"name":"%s","cwd":"%s"}' "$ARGUMENTS" "$PWD" | bough create --stdin-json
```

Run it, then:

- Report the worktree root path bough printed on stdout.
- Note that its engines (mysql / redis / elasticsearch / …) are now running and
  each sub-repo's `.env.local` has been rendered with the allocated ports.

If the command fails with "command not found: bough", stop and tell the user the
`bough` binary must be installed on PATH first — point them at the install
section of the bough README (the plugin ships the commands, not the binary).
