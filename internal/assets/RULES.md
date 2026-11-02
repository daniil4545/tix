# tix rules

Tasks are `T-*.md` files in `~/tasks`, under git. Change them only with `tix`. Run `tix ctx` at the start of a session and after the context is compacted.

## Cycle

1. `tix take T-12`: the task moves to in_progress and you hold it for 3 hours.
2. Work. Log progress with `tix note T-12 "text"` (see Progress). Any other write of yours to the task extends the hold; `note` does not.
3. `tix check T-12 1 2`: mark finished criteria.
4. `tix comment T-12 "text"`: the human's channel. You write notes, not comments; comments are only appended.
5. `tix done T-12 -m "result"`: closes the task and releases it. All criteria are checked: close the task now, do not keep it open for follow-up work.

When a session ends without closing, write one note `Done / Decided / Waiting` and run `tix take T-12 --release`: the task goes back to open.

## Progress

The human reads the task instead of your session. Keep it short and current.

- Write a short note on every movement: a decision (what you chose and why), a commit (`tix note T-12 "3f2a1c removed ask from the core"`: hash and gist), a PR with its link, a finding, a stage change. Not on every command. A note is up to 300 characters; longer text goes to a file and the note keeps its path. The board shows the latest note.
- New comments on your tasks arrive in your context marked "New comments on your tasks", with the author. A comment by `human` is the human's instruction for that task; a comment by another agent, if any, is information.
- A task is one or two sessions. It grew past that: create tasks for the rest, then `tix done T-12 --force -m "rest in T-15, T-16"`.
- Write tickets in the user's language. The title is clear without context. Add to an existing task with the same result, do not open a duplicate.
- The human's title and fields are edited with `tix set`; do not revert them.

## Identity

The name is `agent:session-id`: `--as`, then `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`, `TIX_ACTOR`. In OpenCode pass `--as opencode:<id>`. `take` fails without a name. Other commands then write `unknown`.

## Hold

- Another live hold refuses `take` and names the owner and the time. Take over only on purpose: `tix take T-12 --steal`.
- An expired hold is free.
- The five statuses: idea, open, in_progress, blocked, closed. No others, no aliases: an old word (todo, doing, done, ready, active, waiting, inbox) is refused with exit code 6.
- `take` works from open, blocked and in_progress, and from idea when the task has a criterion. From closed move the task first: `tix move T-12 open`. `tix <command> --help` prints the flags of a command.

## Checks

- Into open: at least one criterion.
- Into blocked: say why with `-m "reason"` or `tix set T-12 waiting="reason"`. Leaving blocked clears the field. Blocked drops your hold; the board marks it with `*` for the human.
- Into closed: all criteria checked. Otherwise `--force -m "reason"`. A task with no criteria needs `-m "result"`.
- `tix new "title" --to open --body-file body.md` creates a task with its criteria in one call (`--body-file -` reads stdin; without the flag stdin is not read). Without criteria `--to open` is refused; `--to` blocked or closed is refused: create the task, then `tix move` or `tix done`.
- Restore: `~/tasks` is a git repository, `git -C ~/tasks log -- T-12.md` finds the commit to revert.
- `tix list` hides closed tasks; `--all` or `--status closed` shows them, `--grep TEXT` matches the title.
- `status`, `claim`, `rank`, `id` and `created` are not set with `set`. Use `move`, `take` and `done`.
- `set` takes: title, project, waiting, tags, external, abstract.
- Repeating a command that changes nothing answers `ok` and writes no commit.
- Exit codes: 2 held by another, 3 conflict, 4 not found, 5 rule refusal, 6 bad input.

## Scope

- Leftover work goes into a new task. New scope is a new task too. Do not stretch the open one.
- Bigger work is an epic: a task tagged `epic` whose `## Details` lists its tasks as `- [ ] T-N <gist>`; a line without T-N is an acceptance criterion. The list lives only in the epic, a child task has no `Epic:` line (`tix show` prints it). The mark of a task line follows the task status by itself: `tix done` ticks it, a move out of closed clears it, `tix check` refuses it. When the last task line is ticked, `tix done` says the epic is ready for acceptance. Closing the epic ticks the lines of closed tasks; a line of an open or missing task refuses it, even with `--force`.
- Frozen work stays in idea with the tag `frozen`: `tix set T-12 tags=frozen`. Do not close it and do not fold several tasks into one.
- A long text (a report, a review, a log) goes into a file; the task gets its path in a note.
- You may create tasks, move them from idea to open, close and delete them, and edit `config.toml`.
- An idea outside a project goes to idea with project `personal`.
- A task needs the user? Move it to blocked with the reason: `tix move T-12 blocked -m "what you need"`, and write the details in a note. The human answers with a comment and moves it back.

## Ticket template

```
## Abstract

What the task is and why.

## Details

- [ ] First criterion
- [ ] Second criterion

## Comments
```

`## Comments` is appended by `tix comment` only. Do not write `## Comments` inside other sections.
