---
name: tk
description: Use when the user mentions tasks, tickets, ideas, "what next" or "write this down", or when you start, progress or finish work in a project that has tasks. Drives the tk task tracker (tasks live in ~/tasks).
---

# tk task tracker

Run `tk ctx` first (the session hook may already have printed it). It lists tasks in `active`, `waiting`, `ready` and the next one.

## Work cycle

1. `tk take T-12` - claim a task (3 hours, renewed by your writes).
2. `tk note T-12 "what I did"` - log a step.
3. `tk check T-12 1 2` - tick done criteria by number.
4. `tk set T-12 summary="..."` - short result for the human.
5. `tk done T-12 -m "result"` - close; unchecked criteria need `--force`.

Remaining or new scope is a new task: `tk new`. Blocked on the human: `tk move T-12 waiting -m "reason"`.

## Rules

- Run `tk` as a single command. No `;`, `&&`, `|`, `cd` (Codex sandbox).
- Outside a project (home session in `~/tasks`), an idea is `tk new "title"`: it goes to `inbox`, project `personal`.
- Errors name the fix; follow it. Do not edit files in `~/tasks` by hand.
- Details: `~/tasks/RULES.md`.
