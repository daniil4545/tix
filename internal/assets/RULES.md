# tix rules

Tasks are `T-*.md` files in `~/tasks`, under git. Change them only with `tix`. Run `tix ctx` at the start of a session and after the context is compacted.

## Cycle

1. `tix take T-12`: the task moves to in_progress and you hold it for 3 hours.
2. Work. Log progress with `tix note T-12 "text"` (see Progress). Any other write of yours to the task extends the hold; `note` does not.
3. `tix check T-12 1 2`: mark finished criteria; `tix uncheck T-12 2` clears a mark.
4. `tix comment T-12 "text"`: the human's channel. You write notes, not comments; comments are only appended.
5. `tix done T-12 -m "result"`: closes the task and releases it. All criteria are checked: close the task now, do not keep it open for follow-up work.

When a session ends without closing, write one note `Done / Decided / Waiting` and run `tix take T-12 --release`: the task goes back to open. `tix take --release` without a number drops all your live holds at once; the SessionEnd hook of the Claude Code and Codex connectors runs it when the window closes (reinstall the skill with `scripts/connect.sh apply --force` to get it).

## Progress

The human reads the task instead of your session. Keep it short and current.

- Write a short note on every movement: a decision (what you chose and why), a commit (`tix note T-12 "3f2a1c removed ask from the core"`: hash and gist), a PR with its link, a finding, a stage change. Not on every command. A note is up to 300 characters; longer text goes to a file and the note keeps its path. The board shows the latest note.
- New comments on your tasks arrive in your context marked "New comments on your tasks", with the author. A comment by `human` is the human's instruction for that task; a comment by another agent, if any, is information.
- A task is one or two sessions. It grew past that: create tasks for the rest, then `tix done T-12 --force -m "rest in T-15, T-16"`.
- Write tickets in the user's language. The title is clear without context. Before `tix new`, run `tix list --like "words"` (or `--like T-12`): it lists similar tasks of the project. Add to an existing task with the same result, do not open a duplicate. To merge a duplicate by hand: put its criteria into the older task with `tix edit`, then `tix done T-13 --force -m "duplicate of T-12"`.
- The human's title and fields are edited with `tix set`; do not revert them.

## Identity

The name is `agent:session-id`: `--as`, then `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`, `TIX_ACTOR`. In OpenCode pass `--as opencode:<id>`. `take` fails without a name. Other commands then write `unknown`.

## Hold

- Another live hold refuses `take` and names the owner and the time. Take over only on purpose: `tix take T-12 --steal`.
- An expired hold is free.
- The five statuses: idea, open, in_progress, blocked, closed. No others, no aliases: an old word (todo, doing, done, ready, active, waiting, inbox) is refused with exit code 6.
- `take` prints `project: NAME /path docs: ...` for a project with a path; read those docs before the work, and `tix ctx` shows the same `path:`. `take` works from open, blocked and in_progress, and from idea when the task has a criterion. From closed move the task first: `tix move T-12 open`. `tix <command> --help` prints the flags of a command.

## Checks

- Into open: at least one criterion.
- Into blocked: say why with `-m "reason"` or `tix set T-12 waiting="reason"`. Leaving blocked clears the field. Blocked drops your hold; the board marks it with `*` for the human.
- Into closed: all criteria checked. Otherwise `--force -m "reason"`. A task with no criteria needs `-m "result"`.
- `tix new "title" --to open --body-file body.md` creates a task with its criteria in one call (`--body-file -` reads stdin; without the flag stdin is not read). Without criteria `--to open` is refused; `--to` blocked or closed is refused: create the task, then `tix move` or `tix done`.
- Restore: `~/tasks` is a git repository, `git -C ~/tasks log -- T-12.md` finds the commit to revert.
- `tix list` hides closed tasks; `--all` or `--status closed` shows them. `--grep TEXT` matches the title, Abstract and criteria, ignoring case; notes, comments and history are excluded.
- `status`, `claim`, `rank`, `id` and `created` are not set with `set`. Use `move`, `take` and `done`.
- `tix projects` lists the known projects with paths; `tix projects add NAME PATH` registers one. Use a name from that list in `tix new --project` and `tix set project=`; `tix new` without `--project` takes the project of the current folder, which must be in the list too. An unknown name is refused with code 5 once the catalog is not empty.
- `tix status` lists the statuses with their kind and task count; `tix status add NAME --kind KIND`, `tix status rename OLD NEW` and `tix status rm NAME` change the list. A status with tasks cannot be removed.
- A body line `After: T-1, T-2` means the task waits for those tasks: `tix ctx` does not offer it as next until they close (`take` still works). `tix list` and `tix show` mark it.
- `set` takes: title, project, waiting, tags, external, abstract.
- Repeating a command that changes nothing answers `ok` and writes no commit.
- Exit codes: 2 held by another, 3 conflict, 4 not found, 5 rule refusal, 6 bad input.

## Scope

- Leftover work goes into a new task. New scope is a new task too. Do not stretch the open one.
- A task can have a parent: `tix new <title> --parent T-N` or `tix set T-5 parent=T-N` (`parent=` clears it). The depth is one: a parent has no parent, a task with children has no parent, a closed parent takes no new child. `tix show` prints `Parent: T-N` or `Children: 3/7` with a line per child.
- Bigger work is an epic: a task tagged `epic` whose `## Details` lists its tasks as `- [ ] T-N <gist>`; a line without T-N is an acceptance criterion. The list lives only in the epic, a child task has no `Epic:` line (`tix show` prints it). The mark of a task line follows the task status by itself: `tix done` ticks it, a move out of closed clears it, `tix check` refuses it. When the last task line is ticked, `tix done` says the epic is ready for acceptance. Closing the epic ticks the lines of closed tasks; a line of an open or missing task refuses it, even with `--force`.
- `tix ctx` prints `stale 14d+: N (T-1, ...)` for idea and open tasks without a move for 14 days; `tix list --stale` shows them with the idle days. Seeing the line, offer the human one review list: close as outdated (`tix done T-12 --force -m "outdated"`), keep, or delete obvious junk (`tix rm`). Do not close or delete them yourself.
- Frozen work stays in idea with the tag `frozen`: `tix set T-12 tags=frozen`. Do not close it and do not fold several tasks into one.
- A long text (a report, a review, a log) goes into a file; the task gets its path in a note.
- You may create tasks, move them from idea to open, close and delete them, and edit `config.toml`.
- An idea outside a project goes to idea with project `personal`.
- A task needs the user? Move it to blocked with the reason: `tix move T-12 blocked -m "what you need"`, and write the details in a note. The human answers with a comment and moves it back. A blocked task is released, so the answer is not pushed to you: after `take`, read the task with `tix show`.

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
