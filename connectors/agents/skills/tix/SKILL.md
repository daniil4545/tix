---
name: tix
description: Use when the user mentions tasks, tickets, ideas, "what next" or "write this down", or when you start, progress or finish agreed work in any project, even one with no tasks yet. Drives the tix task tracker (tasks live in ~/tasks).
---

# tix task tracker

Run `tix ctx` first (a hook may have run it). It lists `in_progress`, `blocked`, `open` tasks and the next one. The five statuses: idea, open, in_progress, blocked, closed.

## Work cycle

0. Agreed work with no task gets one: `tix new "title" --to open --body-file body.md`, criteria are `- [ ]` lines under `## Details` of the body file (`-` reads stdin). Trivial edits need none.
1. `tix take T-12` - claim a task (3 hours); an idea with criteria is taken too.
2. `tix note T-12 "decided X because Y"` - log a decision, question, finding or stage change; not every command.
3. `tix check T-12 1 2` - tick done criteria by number.
4. `tix set T-12 summary="..."` - short result.
5. `tix done T-12 -m "result"` - close once all criteria are checked.

Remaining or new scope is a new task: `tix new`. Frozen work stays in idea with tag `frozen`, never done. Need the human: `tix move T-12 blocked -m "what you need"`.

## Rules

- Run `tix` as a single command. No `;`, `&&`, `|`, `cd` (Codex sandbox).
- Find a task with `tix list --grep TEXT` (title and summary; closed tasks need `--all`), read it with `tix show T-12`; do not grep `~/tasks`.
- Outside a project (home session in `~/tasks`), an idea is `tix new "title"`: it goes to `idea`, project `personal`.
- Errors name the fix; follow it. Do not edit files in `~/tasks` by hand.
- Details: `~/tasks/RULES.md`.
