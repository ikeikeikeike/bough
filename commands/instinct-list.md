---
description: List the instincts in the bough corpus (id, trigger, confidence, domain).
allowed-tools: Bash(bough:*)
---

Run `bough instinct list` and present the instincts to the user as a readable
table (id, trigger, confidence, domain), highest confidence first. If the user
named a specific instinct, follow up with `bough instinct show <id>` to print it
verbatim.
