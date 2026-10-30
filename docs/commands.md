
See also: [how it works](how-it-works.md), [README](../README.md).

| Command | What it does |
|---|---|
| `tix new "title" [--to column] [--project p] [--body-file f]` | creates a task, by default in the `inbox` column, with the body of file `f` (`-` is stdin); `--to ready` needs criteria in the body, a column that needs a reason is refused |
| `tix list [--project p] [--status s] [--grep text] [--all] [--json]` | lists open tasks; `--all` or `--status` of the closed column adds closed ones, `--grep` matches title and summary; alias `ls` |
| `tix show T-12 [-v] [--json]` | prints the whole task with its `rev`, then the notes from the history: the last 5, all with `-v` (see below) |
| `tix edit T-12 --base <rev>` | replaces the body from stdin; comments stay untouched |
| `tix comment T-12 "text"` | adds a comment |
| `tix note T-12 "text"` | logs a step in the history |
| `tix rm T-12` | deletes a task |
| `tix ctx [--project p] [--json]` | brief of the project: active, waiting, ready, next task; outside a project active and waiting, the rest as counts |
| `tix take T-12 [--steal\|--release]` | claims a task for 3 hours, takes it over, or releases it back to ready |
| `tix move T-12 <column or role> [--first\|--after T-9] [-m text] [--force]` | moves a task; checks on entering `ready`, `waiting`, `closed` |
| `tix set T-12 field=value...` | sets `title`, `project`, `waiting`, `due`, `tags`, `ask`, `external`, `abstract`, `summary` |
| `tix check T-12 1 2` | ticks criteria by number |
| `tix done T-12 [-m text] [--force]` | closes a task, drops the claim and clears `ask`; a task whose body starts with `Epic: T-M` ticks its line in that epic |
| `tix backup` | commits edits made outside `tix` and pushes `HEAD` to `origin` |
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

Columns and their roles live in `~/tasks/.tix/config.toml`. The defaults:

| Column | Role | Rule on entering |
|---|---|---|
| `idea` | inbox | none |
| `todo` | ready | at least one criterion |
| `doing` | active | none |
| `blocked` | waiting | needs the `waiting` field, `-m "reason"` sets it |
| `done` | closed | all criteria ticked, or `--force -m "result"` |

The same file has a `[projects]` table to rename a project folder.

## Board

`tix` without arguments opens the board in a terminal. Columns show cards with
the title, the project and a one-line pulse. The board refreshes when a file in
`~/tasks` changes, so an agent's work appears live. Color shows who is on a
task: the title is green while an agent holds a live claim, and the whole card
is grey when nobody does. A `*` appears only when a live agent waits for you:
its `ask` field holds a question, yellow for a decision, blue for a review
(`ask="review: ..."`), red for access such as a key or a login
(`ask="access: ..."`). A card without a live claim has no star: the answer stays
in the task for the next agent. A due date in the past is red, and each project
label has its own muted color.

| Keys | Action |
|---|---|
| `h j k l`, arrows | select a column and a card |
| `space` | grab the card; arrows move it, `space` or `enter` drops it, `esc` cancels |
| `enter` | open the card |
| `n` | new task in the selected column (in the inbox column if that column needs criteria): asks for a title, then opens `$EDITOR` |
| `a` | answer the agent's question |
| `e`, `c` | edit the body in `$VISUAL` or `$EDITOR`, add a comment |
| `g` | go to the agent's session (macOS and Warp only) |
| `o`, `p` | order by due date or rank, cycle the project filter |
| `J`, `K` | move the card down or up in its column |
| `delete` | delete the task, asks `y/n` |
| `?`, `q` | help, quit |

In a card, `j` and `k` step through the history, `enter` unfolds an entry or
toggles a criterion, `h` and `l` open the previous or next task of the column,
`f` edits one field (title, due, tags, ask, waiting, summary, external) and
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
the user cache folder under `tix/`. The only network call is the `git push` of
`tix backup`.
