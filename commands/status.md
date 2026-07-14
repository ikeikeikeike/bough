---
description: Show the bough registry alongside live lsof listen state for each allocated port.
allowed-tools: Bash(bough:*)
---

Run `bough status` and summarize the worktree/port table for the user. Call out
any port that is registered but not currently listening (a stopped engine) and
any that is listening but unregistered (a possible collision).
