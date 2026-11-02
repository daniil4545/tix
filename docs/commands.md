# Commands


See also: [how it works](how-it-works.md), [README](../README.md).

| Command | What it does |
|---|---|
| `tix new "title" [--to status] [--project p] [--body-file f]` | creates a task, by default in `idea`, with the body of file `f` (`-` is stdin); `--to open` needs criteria in the body, `--to blocked` and `--to closed` are refused |
| `tix list [--project p] [--status s] [--grep text] [--all] [--json]` | lists tasks that are not closed; `--all` or `--status closed` adds closed ones, `--grep` matches the title; alias `ls` |
| `tix show T-12 [-v] [--json]` | prints the whole task with its `rev`, then the notes from the history: the last 5, all with `-v` (see below) |
| `tix edit T-12 --base <rev>` | replaces the body from stdin; comments stay untouched |
| `tix comment T-12 "text"` | adds a comment |
| `tix note T-12 "text"` | logs a step in the history; up to 300 characters, longer is refused with code 5: put the text in a file and the path in the note; the board shows the latest note |
| `tix rm T-12` | deletes a task |
| `tix ctx [--project p] [--json]` | brief of the project: in_progress, blocked, open, next task; outside a project in_progress and blocked, the rest as counts |
| `tix take T-12 [--steal\|--release]` | claims a task for 3 hours, takes it over, or releases it back to open |
| `tix move T-12 <status> [--first\|--after T-9] [-m text] [--force]` | moves a task; checks on entering `open`, `blocked`, `closed`; `-m` is for `blocked` and `closed` |
| `tix set T-12 field=value...` | sets `title`, `project`, `waiting`, `tags`, `external`, `abstract` |
| `tix check T-12 1 2` | ticks criteria by number; a task line (`- [ ] T-N ...`) of an epic is refused, it follows the status of T-N |
| `tix done T-12 [-m text] [--force]` | closes a task and drops the claim; its line in every epic (tag `epic`) is ticked in the same commit, a move out of closed clears it; prints `epic T-M: all tasks closed, ready for acceptance` when that was the last line; closing an epic ticks the lines of closed tasks and refuses a line of an open or missing task, even with `--force` |
| `tix inbox --hook` | delivers new comments to an agent; runs from a hook, not by hand |

An id works with or without the prefix: `12` means `T-12`. Every command takes
`--as <name>` for the actor and `-v` to print the commit hash. Without `--as`
the name comes from `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID` or `TIX_ACTOR`.
When both agent variables are set (one agent started inside another), a write
asks for `--as`.
Text that starts with a dash goes after `--`.

`tix show` ends with the notes of the task, read from the store history (`tix note`
writes only a commit, not the file). Without notes the section is absent. Format:

```
## Notes (last 5 of 7, -v shows all)
- 2026-10-24 14:03 claude:abc: text of the note
```

The default is the last 5 notes, oldest first; `-v` prints all (`Notes (7)`).
Notes survive renames and moves. The history read keeps the newest 500 notes per
task, and `--json` output has no notes. The section is output only: `tix edit`
refuses a body that contains its heading, so cut it before pasting a task back.

Exit codes: 0 success, 1 other error, 2 busy, 3 conflict, 4 no such task,
5 rule refused, 6 bad input.

Five fixed statuses, in board order. An old word (`todo`, `doing`, `done`, `ready`, `active`,
`waiting`, `inbox`) is refused with exit code 6 and a hint such as "maybe open?".

| Status | Rule on entering |
|---|---|
| `idea` | none; a new task starts here |
| `open` | at least one criterion |
| `in_progress` | none; `take` moves a task here |
| `blocked` | needs the `waiting` field, `-m "reason"` sets it; the board marks the card with `*` |
| `closed` | all criteria ticked, or `--force -m "result"` |

A store that still has `[[column]]` in `~/tasks/.tix/config.toml` is migrated when it is opened: the
statuses of all tasks are renamed by the role of their column and `[[column]]` is removed, in one commit.
The same file has a `[projects]` table to rename a project folder.

## Board

`tix` without arguments opens the board in a terminal. Columns show cards with
the title, the project and a one-line pulse. The board refreshes when a file in
`~/tasks` changes, so an agent's work appears live. Color shows who is on a
task: the title is green while an agent holds a live claim, and the whole card
is grey when nobody does. Each project
label has its own muted color.

| Keys | Action |
|---|---|
| `h j k l`, arrows | select a column and a card |
| `space` | grab the card; arrows move it, `space` or `enter` drops it, `esc` cancels |
| `enter` | open the card |
| `n` | new task in the selected column (in `idea` if that status needs criteria): asks for a title, then opens `$EDITOR` |
| `e`, `c` | edit the body in `$VISUAL` or `$EDITOR`, add a comment |
| `p` | cycle the project filter |
| `J`, `K` | move the card down or up in its column |
| `delete` | delete the task, asks `y/n` |
| `?`, `q` | help, quit |

In a card, `j` and `k` step through the history, `enter` unfolds an entry or
toggles a criterion, `h` and `l` open the previous or next task of the column,
`f` edits one field (title, tags, waiting, external) and
`esc` goes back.

The mouse does the same. Click a card to select it and double click to open
it. Drag a card to another column to move it. Click `+` in a column name for a
new task. The wheel scrolls. The hint line and the card actions line are
buttons.

A drop or a drag goes through the same checks as `tix move`. A refusal, for
example moving a task without criteria to `todo`, shows in the status line.


## Configuration

All variables are optional.

| Variable | What it sets |
|---|---|
| `TIX_HOME` | task folder, default `~/tasks` |
| `TIX_LOCK_TIMEOUT` | wait for the write lock, a Go duration, default `5s` |
| `TIX_GIT` | path to git, skips the search and the cache |
| `TIX_ACTOR` | actor name when no agent session id is set |
| `TIX_MODEL` | model name for the `Model:` trailer |
| `VISUAL`, `EDITOR` | editor for the board, `vi` if both are empty |

`tix` writes only inside the task folder. Service files live in
`~/tasks/.tix`. Small per-user state (delivery marks, tab addresses) lives in
the user cache folder under `tix/`. `tix` makes no network calls.
