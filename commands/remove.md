---
description: Tear down a bough per-worktree environment (stops its engines, drops datadirs, removes the worktree; keeps the branch).
argument-hint: <worktree-name-or-path>
allowed-tools: Bash
---

Tear down the bough worktree identified by `$ARGUMENTS` (a worktree name or an
absolute worktree path).

If `$ARGUMENTS` is empty, run `bough list` first, show the registered worktrees,
and ask the user which one to remove.

`remove` reads the WorktreeRemove payload on stdin. Prefer the explicit path
when the argument looks like a path, otherwise pass it as the name:

```bash
# when $ARGUMENTS is an absolute path:
printf '{"worktree_path":"%s"}' "$ARGUMENTS" | bough remove --stdin-json
# when $ARGUMENTS is a bare name:
printf '{"name":"%s","cwd":"%s"}' "$ARGUMENTS" "$PWD" | bough remove --stdin-json
```

After it completes, confirm the engines were stopped and the worktree removed,
and remind the user the git branch is retained by design.
