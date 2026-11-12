# Commands


See also: [how it works](how-it-works.md), [README](../README.md).

| Command | What it does |
|---|---|
| `tix new "title" [--to status] [--project p] [--body-file f] [--parent T-N]` | creates a task (a child of T-N with `--parent`), by default in `idea`, with the body of file `f` (`-` is stdin); `--to open` needs criteria in the body, `--to blocked` and `--to closed` are refused |
| `tix list [--project p] [--status s] [--kind k] [--grep text] [--like text|T-N] [--all] [--stale] [--json]` | lists tasks that are not closed; `--stale` keeps idea and open tasks without a move for 14 days, most idle first, with `| idle Nd`; `--status` filters by status name, `--kind` by kind (`idea`, `open`, `in_progress`, `blocked`, `closed`; another word is refused with the list); `--all`, `--status` or `--kind` adds closed ones; a status whose name is not its kind word prints as `review [in_progress]` in `list`, `ctx` and `show`; `--json` has `status` and `kind`, and so has `ctx --json` in each section; `--grep` matches the title, Abstract and criteria, ignoring case, excluding notes, comments and history; `--like` lists tasks of the project similar to the words or to the title of T-N (not T-N itself), most similar first: stems of 5 letters, epics and closed tasks excluded unless `--all`, a one-word query finds nothing; alias `ls` |
| `tix show T-12 [-v] [--json]` | prints the whole task with its `rev` (a child prints `Parent: T-N`, a missing parent `Parent: T-N (not found)`; a parent prints `Children: 3/7` and a line per child: status, number, title), then the notes from the history: the last 5, all with `-v` (see below) |
| `tix edit T-12 --base <rev>` | replaces the body from stdin; comments stay untouched |
| `tix comment T-12 "text"` | adds a comment |
| `tix note T-12 "text"` | logs a step in the history; up to 300 characters, longer is refused with code 5: put the text in a file and the path in the note; the board shows the latest note |
| `tix rm T-12` | deletes a task |
| `tix ctx [--project p] [--json]` | brief of the project: in_progress, blocked, open, the line `stale 14d+: N (ids)` (up to 10 ids) for idea and open tasks without a move for 14 days, next task; the line `path: /abs/path` when the project has a path in `[paths]`; outside a project in_progress and blocked, the rest as counts |
| `tix take --release` | without a number: releases every live claim of the actor in one commit (in_progress goes to the head of open), prints `ok T-2 T-3 released` or `ok no claims`; foreign and expired claims stay; `--hook` is the SessionEnd form: actor from the hook input, silent |
| `tix take T-12 [--steal\|--release]` | claims a task for 3 hours, takes it over, or releases it back to open; take and steal for a project with a path also print `project: NAME /abs/path docs: AGENTS.md CLAUDE.md` (docs: the files that exist in the path root) |
| `tix move T-12 <status> [--first\|--after T-9] [-m text] [--force]` | moves a task; checks on entering `open`, `blocked`, `closed`; `-m` is for `blocked` and `closed` |
| `tix set T-12 field=value...` | sets `title`, `project`, `waiting`, `tags`, `external`, `abstract`, `parent`; `parent=T-N` sets or changes the parent, `parent=` clears it; refused: no such task, the task itself, a parent that has a parent, a task that has children, a parent of the kind closed |
| `tix check T-12 1 2` | ticks criteria by number; a task line (`- [ ] T-N ...`) of an epic is refused, it follows the status of T-N |
| `tix uncheck T-12 2 3` | clears the ticks by number; a task of the kind closed is refused (reopen it first), so is a task line of an epic |
| `tix done T-12 [-m text] [--force]` | closes a task and drops the claim; its line in every epic (tag `epic`) is ticked in the same commit, a move out of closed clears it; prints `epic T-M: all tasks closed, ready for acceptance` when that was the last line; closing an epic ticks the lines of closed tasks and refuses a line of an open or missing task, even with `--force` |
| `tix projects` | lists the catalog: project, path or `-`, number of tasks not closed; a project that only tasks mention gets `not in catalog` |
| `tix projects add NAME [PATH]` | registers a project in `[paths]` of config.toml; PATH is an absolute existing folder, else code 6; without PATH the project has no folder; repeating the same call prints ok; no `rm`, edit config.toml by hand |
| `tix status` | lists the statuses in board order: name, kind, number of tasks |
| `tix status add NAME --kind KIND` | adds a status after the last one of its kind; it never becomes the default of the kind; refused (code 5): the name exists, is malformed (`[a-z0-9_]+`, up to 20) or is a kind word of another kind, or the kind is unknown |
| `tix status rename OLD NEW` | rewrites `status:` in the tasks of OLD, then renames it in config.toml, one commit; a run that was interrupted is finished by the same command; same name rules as `add` |
| `tix status rm NAME` | removes a status with no tasks and not the last of its kind; the refusal names the task count and the hints `tix status rename`, `tix move`; `rename` and `rm` also refuse while a task file does not parse and name the files |
| catalog check | when `[paths]` is not empty, `tix new` (`--project X` or the project of the current folder), `tix set T-N project=X` and `n` in the TUI refuse a name outside the catalog and not `personal` (case ignored): code 5, the known projects and `tix projects add X PATH`; the same in the TUI status line; an empty catalog has no check; old tasks with an unknown project are read and edited, only a change of `project` to an unknown name is refused |
| `tix inbox --hook` | delivers new comments to an agent; runs from a hook, not by hand |

An id works with or without the prefix: `12` means `T-12`. Every command takes
`--as <name>` for the actor and `-v` to print the commit hash. Without `--as`
the name comes from `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID` or `TIX_ACTOR`.
When both agent variables are set (one agent started inside another), a write
asks for `--as`.
Text that starts with a dash goes after `--`.

A body line `After: T-1, T-2` (case-insensitive, outside code blocks) names the tasks this one waits for.
While one of them is not closed, `tix ctx` does not offer the task as `next`, `tix list` adds
`| after T-1` and `tix show` prints a line `after T-1` before the file. A number with no task counts as
closed. `tix take` works as usual.

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
the title, the latest note with its age, the criteria count and the claim. The board refreshes when a file in
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
| `p` | project list: letters narrow it (substring, any case), arrows select, `enter` sets the filter, `esc` keeps the old one; `all` is first, projects of tasks and of the catalog follow with their open counts |
| `f` | search: letters are the query (substring of the title, Abstract and criteria, any case, same as `tix list --grep`); rows show number, title and an excerpt; `tab` switches the board project and all projects, `ctrl+t` shows closed tasks, arrows select, `enter` opens the card, `esc` returns to the board as it was |
| `J`, `K` | move the card down or up in its column |
| `delete` | delete the task, asks `y/n` |
| `?`, `q` | help, quit |

In a card, `j` and `k` step through the history, `enter` unfolds an entry or
toggles a criterion, `h` and `l` open the previous or next task of the column,
`e` and `c` work as on the board, and `esc` goes back. The title and the fields
(tags, project, external, waiting, abstract) are changed with `tix set`.

The mouse does the same. Click a card to select it and double click to open
it. Drag a card to another column to move it. Click `+` in a column name for a
new task. The wheel scrolls. The hint line and the card actions line are
buttons.

A drop or a drag goes through the same checks as `tix move`. A refusal, for
example moving a task without criteria to `open`, shows in the status line.


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
`~/tasks/.tix`. Small per-user state (delivery marks) lives in
the user cache folder under `tix/`. `tix` makes no network calls.
