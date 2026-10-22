# tix rules

Tasks are `T-*.md` files in `~/tasks`, under git. Change them only with `tix`. Run `tix ctx` at the start of a session and after the context is compacted.

## Cycle

1. `tix take T-12`: the task moves to the active column and you hold it for 3 hours.
2. Work. Log progress with `tix note T-12 "text"` (see Progress). Any other write of yours to the task extends the hold; `note` does not.
3. `tix check T-12 1 2`: mark finished criteria.
4. `tix set T-12 summary="what is next"`: the first line shows on the board. Replace the summary, do not append to it.
5. `tix comment T-12 "text"`: comments are only appended.
6. `tix done T-12 -m "result"`: closes the task, releases it and clears `ask`. All criteria are checked: close the task now, do not keep it open for follow-up work.

When a session ends without closing, write the comment `Done / Decided / Waiting` and run `tix take T-12 --release`: the task goes back to the ready column.

## Progress

The human reads the task instead of your session. Keep it short and current.

- Write a note on a decision (what you chose and why), a question, a finding and a stage change: spec, code, review, done. Not on every command.
- Whoever answers a question in `ask`, you or the human, record it: `tix comment T-12 "Answer: ..."`, then `tix set T-12 ask=`. An orchestrator that answers for the human writes the same comment.
- New comments on your tasks arrive in your context marked "New comments on your tasks", with the author. A comment by `human` is the human's instruction for that task; a comment by another agent is information.
- A task should fit one session. It grew past that: create tasks for the rest, then `tix done T-12 --force -m "rest in T-15, T-16"`.

## Identity

The name is `agent:session-id`: `--as`, then `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`, `TIX_ACTOR`. In OpenCode pass `--as opencode:<id>`. `take` fails without a name. Other commands then write `unknown`.

## Hold

- Another live hold refuses `take` and names the owner and the time. Take over only on purpose: `tix take T-12 --steal`.
- An expired hold is free.
- `take` works from ready, waiting and active columns. From any other column move the task first: `tix move T-12 ready`. `tix <command> --help` prints the flags of a command. `move` takes a column name or a role; `tix ctx` prints which column has which role.

## Checks

- Into ready: at least one criterion.
- Into waiting: say why with `-m "reason"` or `tix set T-12 waiting="reason"`. Leaving waiting clears the field.
- Into closed: all criteria checked. Otherwise `--force -m "reason"`. A task with no criteria needs `-m "result"`.
- A column without a role is not yours.
- `status`, `claim`, `rank`, `id` and `created` are not set with `set`. Use `move`, `take` and `done`.
- `set` takes: title, waiting, due, tags, ask, external, abstract, summary.
- Repeating a command that changes nothing answers `ok` and writes no commit.
- Exit codes: 2 held by another, 3 conflict, 4 not found, 5 rule refusal, 6 bad input.

## Scope

- Leftover work goes into a new task. New scope is a new task too. Do not stretch the open one.
- Frozen work stays in the inbox column with the tag `frozen`: `tix set T-12 tags=frozen`. Do not close it and do not fold several tasks into one.
- A long text (a report, a review, a log) goes into a file; the task gets its path in a comment.
- You may create tasks, move them from inbox to ready, close and delete them, and edit `config.toml`.
- An idea outside a project goes to the inbox column with project `personal`.
- A task needs the user? Put the question in `ask`: `tix set T-12 ask="what you need"`. The board marks it.

## Ticket template

```
## Abstract

What the task is and why.

## Summary

What is next, in the first line.

## Details

- [ ] First criterion
- [ ] Second criterion

## Comments
```

`## Comments` is appended by `tix comment` only. Do not write `## Comments` inside other sections.
