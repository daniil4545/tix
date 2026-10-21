# tix

Memory for agents across sessions and projects (Claude Code, Codex, OpenCode): tasks are md files in `~/tasks` under git, and every edit leads back to the agent session that made it. Agents work through the `tix` CLI, the human works through the TUI.

## Stage

MVP. The write core, the seven base `tix` commands (`new`, `show`, `edit`, `comment`, `note`, `rm`, `list`) and the agent cycle (`ctx`, `take`, `move`, `set`, `check`, `done`, `backup`) work; S2 acceptance is under way. The TUI and the agent connectors come in S3-S4.

## Quick start

Requirements: Go 1.27.2, `golangci-lint` 2.x, git.

1. Build: `make build` (binary `bin/tix`).
2. Set the store: `export TIX_HOME=$HOME/tasks` (without the variable `~/tasks` is used).
3. Create a task: `bin/tix new "first task"`.
4. Check: `bin/tix list` prints `<column> T-1 first task | <summary>`.

Version: `tix --version` prints `dev`; a build with a version: `VERSION=1.2.3 make build`.

Check: `make check`. Speed: `make bench`.

## Moving from the name tk

The project used to be called `tk`. The service folder of the store `.tracker` is renamed to `.tix` the first time the store is opened, and the `.tracker/` entries in the store `.gitignore` are replaced. `scripts/connect.sh apply` removes the old hooks, skill and Codex rule and installs `tix`. The `TRACKER_*` variables are no longer read: set `TIX_*`. The `tk` cache is not carried over; delivery marks and focus are created again.

## Agent cycle

The agent works by `RULES.md` (created in `~/tasks` on the first write). The agent name comes from `--as`, then from `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`, `TIX_ACTOR`.

| Command | What it does |
|---|---|
| `tix ctx [--project p] [--json]` | active, waiting and ready tasks of the project; no git |
| `tix take T-12 [--steal\|--release]` | claims the task for 3 hours, steals the claim or releases it |
| `tix move T-12 <column> [--first\|--after T-9] [-m text] [--force]` | moves the task; entry checks for `ready`, `waiting`, `closed` |
| `tix set T-12 field=value...` | changes `title`, `waiting`, `due`, `tags`, `ask`, `external`, `abstract`, `summary` |
| `tix check T-12 1 2` | checks criteria |
| `tix done T-12 [-m result] [--force]` | closes the task and releases the claim |
| `tix backup` | commits external edits and pushes `HEAD` to `origin` |

Exit codes: 0 success; 2 busy; 3 conflict; 4 no task; 5 rule; 6 input; 1 other.

## Configuration

All variables are optional.

| Variable | Required | What it sets |
|---|---|---|
| `TIX_HOME` | no | the task store, `~/tasks` by default |
| `TIX_LOCK_TIMEOUT` | no | wait for the write lock, 5 s by default |
| `TIX_GIT` | no | path to git, bypassing the search and the cache |

## Documents

| Question | Where the answer is |
|---|---|
| how the code is built and where to change it | `docs/architecture.md` |
| what we build and why it was decided so | decisions `docs/specs/decisions.md`, spec `docs/spec.md` |
| sync with external trackers (after the MVP) | `docs/specs/integrations.md` |
| what it looks like | mockup `docs/mockup/ui.html` |
| where the project is now and what is next | `docs/state.md`, `docs/backlog.md` |
| recon results | `docs/research/` |
| git and Codex behavior | `docs/api/git.md`, `docs/api/codex.md` |
| spec of the current task, temporary | `docs/plans/` |
| rules for the agent | `AGENTS.md` |
