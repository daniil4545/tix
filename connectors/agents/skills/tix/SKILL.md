---
name: tix
description: Use when the user mentions tasks, tickets, ideas, "what next" or "write this down", or when you start, progress or finish agreed work in any project, even one with no tasks yet. Drives the tix task tracker (tasks live in ~/tasks).
---

# tix task tracker

Run `tix ctx` first (a hook may have run it). It lists `active`, `waiting`, `ready` tasks and the next one.

## Work cycle

0. Agreed work with no task gets one: `tix new "title"`; criteria are `- [ ]` lines under `## Details` in a body file, `tix edit T-12 --base <rev> < body.md` (`rev` from `tix show T-12 --json`); then `tix move T-12 ready`. Trivial edits need none.
1. `tix take T-12` - claim a task (3 hours).
2. `tix note T-12 "decided X because Y"` - log a decision, question, finding or stage change; not every command.
3. `tix check T-12 1 2` - tick done criteria by number.
4. `tix set T-12 summary="..."` - short result.
5. `tix done T-12 -m "result"` - close once all criteria are checked.

Remaining or new scope is a new task: `tix new`. Frozen work stays in inbox with tag `frozen`, never done. Blocked on the human: `tix move T-12 waiting -m "reason"`. A question: `tix set T-12 ask="..."`, prefix `review:` or `access:`.

## Rules

- Run `tix` as a single command. No `;`, `&&`, `|`, `cd` (Codex sandbox).
- Outside a project (home session in `~/tasks`), an idea is `tix new "title"`: it goes to `inbox`, project `personal`.
- Errors name the fix; follow it. Do not edit files in `~/tasks` by hand.
- Details: `~/tasks/RULES.md`.
