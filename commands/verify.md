---
description: Compare a worktree's registry vs .env.local vs declared ranges and report any drift.
argument-hint: <worktree-name>
allowed-tools: Bash(bough:*)
---

Run `bough verify $ARGUMENTS` to check the given worktree for drift between the
port registry, the rendered `.env.local` files, and the declared `.bough.yaml`
ranges.

If `$ARGUMENTS` is empty, run `bough list` first and ask which worktree to
verify. Report whether it is consistent, and if `bough verify` exits non-zero,
explain exactly which value drifted.
