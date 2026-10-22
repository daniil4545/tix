# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `ask` takes a kind prefix: `decide:` (the default), `review:` or `access:`.
  The board star is yellow, blue or red by kind. `tix set` refuses an unknown
  lowercase prefix with code 5.
- `tix set T-12 project=<name>` changes the project of a task.

### Changed

- The star of an `ask` turns grey when the task has no live claim, and `g` on
  it says that the session ended.
- `tix new --to` a column with an entry check (ready, waiting, closed) is
  refused with code 5 before any write: create the task, add criteria, then
  `tix move`.
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

[Unreleased]: https://github.com/daniil4545/tix/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/daniil4545/tix/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/daniil4545/tix/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/daniil4545/tix/releases/tag/v0.1.0
