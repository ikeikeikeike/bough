---
description: Promote cross-project instincts (present in ≥2 projects, avg confidence ≥0.8) into the global corpus.
argument-hint: "[--dry-run]"
allowed-tools: Bash(bough:*)
---

Run `bough instinct promote $ARGUMENTS` to promote instincts that recur across
projects (present in ≥2 projects with average confidence ≥0.8) into the global
corpus that the UserPromptSubmit inject reads.

Default to a preview first: if the user has not asked to write, run
`bough instinct promote --dry-run` and show what would be promoted before doing
it for real. Report which instincts were promoted (or would be).
