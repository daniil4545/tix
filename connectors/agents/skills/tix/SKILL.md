---
name: tix
description: Use when the user mentions tasks, tickets, ideas, "what next" or "write this down", or when you start, progress or finish agreed work in any project, even one with no tasks yet. Drives the tix task tracker (tasks live in ~/tasks).
---

# tix task tracker

Run `tix ctx` first (a hook may have run it). Full rules: `~/tasks/RULES.md`.

## Work cycle

0. Agreed work with no task: first `tix list --like "words"`, else `tix new "title" --to open --body-file body.md` (criteria are `- [ ]` lines under `## Details`).
1. `tix take T-12` claims the task for 3 hours; it prints the project path.
2. `tix note T-12 "text"` on every movement: decision, commit, PR (up to 300 characters).
3. `tix check T-12 1 2` ticks criteria.
4. `tix done T-12 -m "result"` closes the task.

## Rules

- Run `tix` as a single command: no `;`, `&&`, `|`, `cd`.
- Find tasks with `tix list --grep TEXT` or `--like TEXT`, read with `tix show T-12`; never grep or edit `~/tasks`.
- Projects: `tix projects`; an unknown name is refused.
- Need the human: `tix move T-12 blocked -m "what you need"`.
- A `stale 14d+` line: offer the human a review, never close or delete yourself.
- Errors name the fix; follow it.
