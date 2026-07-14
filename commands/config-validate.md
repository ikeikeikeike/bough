---
description: Validate a .bough.yaml against the bough schema.
argument-hint: "[path-to-.bough.yaml]"
allowed-tools: Bash(bough:*)
---

Run `bough config validate $ARGUMENTS` (defaults to the monorepo root's
`.bough.yaml` when no path is given) and report whether the config is valid. If
it fails, quote the exact schema error and point at the offending key.
