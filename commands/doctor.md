---
description: Report bough's hook wiring, observer state, and cost posture (transparency check).
allowed-tools: Bash(bough:*)
---

Run `bough doctor` and summarize for the user:

- which hook events are wired, and whether they come from bough or a hand-edit;
- whether the `claude` CLI is on PATH (required for instinct minting);
- whether any Anthropic API-key vars are exported (which could flip billing);
- the self-DoS caps and the homunculus corpus root.

If the report warns that these hooks live in `settings.json` while the bough
Claude Code plugin is also installed, explain that the two double-fire and the
`settings.json` copy should be removed with `bough hook uninstall`.
