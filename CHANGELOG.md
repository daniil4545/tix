# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `tix new "title" --body-file body.md` creates a task with its body in one
  call; `--body-file -` reads stdin. With criteria in the body, `--to ready`
  lands in ready at once. Without the flag stdin is never read, so a shell loop
  keeps its lines and an open stdin does not hang `new`.
- `tix list --grep TEXT` matches the title and the `## Summary` section,
  case-insensitive. `tix ls` is an alias of `tix list`.
- Epics: a task whose body starts with `Epic: T-M` ticks its `- [ ] T-N` line
  in that epic when `tix done` closes it, in a separate commit. When every line
  is ticked, `done` prints `epic T-M ready for acceptance`; the epic stays open.
  A missing epic or line never fails `done`.

### Changed

- `tix list` and `tix list --json` no longer print tasks of the closed column.
  `--all` brings them back; `--status <closed column>` shows that column.
- `tix take` of an inbox task with at least one criterion moves it straight
  into work with a claim; an inbox task without criteria is refused as before.
- `tix ctx` outside a project lists active and waiting tasks in full and prints
  only a count for every other column. `ctx` of one project is unchanged.
- The refusal of `tix new --to ready` without criteria names `--body-file`.

## [0.5.0] - 2026-10-26

### Added

- `tix version` prints the same version as `tix --version` and exits with 0.
- A mistyped command gets the closest known command: `tix lsit` prints
  `unknown command lsit; did you mean list?`. With no command within two edits
  the message keeps the usage line. The exit code is unchanged.
- The task card works by mouse: a click on a criterion or a history entry
  selects it, a click on the `[ ]` / `[x]` box toggles the criterion, and the
  wheel scrolls the list without moving the selection.

### Changed

- The card list scrolls only when the cursor crosses the top or bottom edge,
  in both directions, like a board column.
- `take` and `move` keep a cache of the column order in `.git/tix-rank`: an
  unchanged task file is not parsed again. A missing, stale or broken cache
  falls back to reading the files.

## [0.4.0] - 2026-10-26

### Added

- `tix show` prints the `tix note` entries of a task after the file, read from
  the store history: the last 5 by default, all with `-v`. Notes written before
  this version show up as well, no migration. `--json` output is unchanged.

### Changed

- `tix edit` refuses a body that contains the `## Notes (...)` heading printed
  by `show`, so pasting `show` output back does not store the notes in the file.

## [0.3.0] - 2026-10-22

### Added

- `ask` takes a kind prefix: `decide:` (the default), `review:` or `access:`.
  The board star is yellow, blue or red by kind. `tix set` refuses an unknown
  lowercase prefix with code 5.
- `tix set T-12 project=<name>` changes the project of a task.

### Changed

- Board colors show life: the title is green with a live claim, a card without
  one is grey and has no star, and `g` on its `ask` says that the session
  ended. The star appears only with a live claim and an `ask`.
- A due date in the past is red, and each project label has its own color.
- `tix new --to` a column with an entry check (ready, waiting, closed) is
  refused with code 5 before any write: create the task, add criteria, then
  `tix move`. On the board, `n` in such a column creates the task in the inbox
  column and keeps the title.
- Project names are stored in lower case; the project filter ignores case.

## [0.2.0] - 2026-10-22

### Changed

- `tix take --release` moves a task out of the active column back to the head
  of the ready column, so the active column shows only work in progress. It
  also drops an expired claim of another agent.
- `tix done` clears `ask`: a closed task no longer shows the yellow star.
- `tix new --to` takes a role as well as a column name, as `tix move` does.
- `tix take` on a task without criteria names the whole path: add criteria,
  move, take.
- `RULES.md` and the agent skill: frozen work stays in the inbox with the tag
  `frozen`, a task is closed once all criteria are checked, a long report goes
  into a file.
- When both `CLAUDE_CODE_SESSION_ID` and `CODEX_THREAD_ID` are set, a write
  refuses with code 6 and names both `--as` forms instead of guessing which
  agent calls. The comment hook takes the name from its input.

### Fixed

- `tix note` reports an error when its commit fails: the note lived only in the
  commit and was lost behind an `ok`.

### Added

- `tix <command> --help` prints the usage line of the command.

## [0.1.1] - 2026-10-21

### Changed

- The agent skill applies to agreed work in any project, not only in a project
  that already has tasks, and tells the agent to create a task when none covers
  the work.

### Fixed

- `RULES.md` in an existing store now gets the current text when it is an
  untouched copy from an earlier version. A copy you edited is kept as it is.

## [0.1.0] - 2026-10-21

First public release.

### Added

- Task store in `~/tasks`: one markdown file per task, one git repository for all
  projects, `TIX_HOME` to move it. The project is the folder name above `.git`,
  and `[projects]` in `.tix/config.toml` renames it.
- Core write path with a lock, a temporary file, a rename and one git commit per
  edit. Every commit carries an `Actor:` line with the full id of the agent
  session, or `human` for the board. An edit made outside `tix`, for example in
  Obsidian, is committed as an external edit by the next operation.
- `Model:` trailer in a commit, taken from `TIX_MODEL`. The Claude Code
  `SessionStart` hook sets it for the session. `Code: <project>@<sha>` trailer
  with the project `HEAD`.
- A task file that does not parse is never overwritten. Stale git lock files
  are removed, a fresh one makes `tix` answer `git is busy`.
- Fifteen commands: `ctx`, `inbox`, `take`, `move`, `set`, `check`, `done`,
  `backup`, `new`, `list`, `show`, `edit`, `comment`, `note`, `rm`.
- `tix --version`.
- `tix ctx`: a short brief of the active, waiting and ready tasks of the current
  project, with the next task to take and a `columns:` line that names the role
  of each column. Under 2000 characters.
- `move` accepts a column name or a column role, for example `tix move 12 ready`.
- Claim on `take` for three hours, with `--steal` and `--release`. Short ids:
  `12` means `T-12`.
- `edit` with `--base <rev>` and a three-way merge, so a stale edit does not
  lose a newer one.
- Exit codes by kind of refusal: 2 busy, 3 conflict, 4 no such task, 5 rule,
  6 input, 1 other.
- Columns with roles in `.tix/config.toml`, checks on entering `ready`,
  `waiting` and `closed`, and the `ask` field for a task that needs a human.
- Terminal board: `tix` without arguments opens it. Task card with history,
  `$VISUAL` or `$EDITOR` for the body, new task, answer to an agent question,
  field edit, delete, project filter, order by due date, live refresh.
- Board mouse: select and open a card, drag a card to another column, `+` in a
  column name for a new task, wheel scrolling, clickable hints and card actions.
- Board human path: grab a card with `space`, move it with arrows, drop it with
  `space`; reorder with `J` and `K`; `?` shows the help.
- Jump from a card to the agent's terminal tab with `g` (macOS and Warp).
- Delivery of comments to agents: `tix inbox --hook` adds new comments on the
  tasks an agent holds to its context, once. A `human` comment also returns an
  agent that has stopped to work.
- `RULES.md` for agents, embedded in the binary and written to `~/tasks` on the
  first write.
- `scripts/connect.sh plan|apply|undo` for Claude Code and Codex: a skill, hooks
  for `SessionStart` (`tix ctx --hook` for Claude Code, `tix ctx` for Codex) and
  for `PostToolUse`, `UserPromptSubmit` and `Stop` (`tix inbox --hook`), and a
  permission or rule that allows `tix`. Backups `*.tix-bak-*` live only while
  `apply` runs. The manifest is `~/tasks/.tix/connect-state.json`.
- Release archives for macOS and Linux on amd64 and arm64, with `SHA256SUMS.txt`.

[Unreleased]: https://github.com/daniil4545/tix/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/daniil4545/tix/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/daniil4545/tix/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/daniil4545/tix/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/daniil4545/tix/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/daniil4545/tix/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/daniil4545/tix/releases/tag/v0.1.0
