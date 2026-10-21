# State

Project tasks are kept in the tracker (`~/tasks`, project `tracker`): `tix list`, the board is `tix` without arguments.
`~/bin/tk` and `~/bin/tix` both link to `bin/tix` of this repository.
Public repository `daniil4545/tix` is a snapshot: `scripts/snapshot.sh <dir>`, copy into a clone of the public
repository, one commit on top, push. Never push from this repository (it has no remote).

## In progress

- Trial run of v0.1.0 for 2-3 days in normal work (user, from 2026-10-21). Then T-38.
- T-20 "A reply from the task reaches the agent": in `main`, hooks installed. Step "Acceptance": a live check
  on a new session (this one did not pick up the hooks), approval of the hooks in Codex `/hooks`.

## Next

1. T-38: embed tix deeper into the work process, after the trial run. The user does not open the board now:
   agents creating and closing their own tasks is enough. Start from what the trial showed.
2. T-32: the user decides who owns `~/tasks/RULES.md` (the core writes it only at creation; the live file
   still says `tk`).
3. After the MVP: T-27 (clicks on the question line, needs a modifier probe in Warp), T-5, T-7.

## Open

- Branch `z5-publish` (tree `../tracker-z5`, `d635570`) is superseded by `main`; deleting it is the user's call.
- Codex: approve the new `tix` hooks in `/hooks`.
- Known race: the reply `a` may erase a new `ask` of the agent in a window of milliseconds; conditional write - v0.2.
- The model in the `Model:` trailer is only for Claude Code; `/model` in the middle of a session and subagents - v0.2.
- `make e2e-agents` and live Codex calls spend subscription limits: only with the user's consent.
- The docker image `golang:1.27` is kept for checking on Linux.

## Done

- 2026-10-20. Recon, the MVP spec, S1-S4: core, CLI, connectors, TUI; install on the Mac; tasks in the tracker.
- 2026-10-21. The MVP boundaries were rebuilt (D65), philosophy: the task is the boundary "discussed / doing".
  In `main` (`0c07aea`): board with a hint line, pulse and ordering by due date; mouse; the human main path
  without the CLI (claim, new task, reply, fields, criteria, delete, help, jump to the Warp session on `g`);
  the `Model:` trailer; comment delivery to the agent `inbox --hook`. Each zone passed code review and
  skeptic, `make check` is green. The name `tix` (D67), rename
  in `z6-tix`. Rule: public projects are fully in English (global `CLAUDE.md`).
- 2026-10-21. Release v0.1.0 (`main` tag `v0.1.0`, public snapshot `c002c40`). T-24: migration `.tracker` to
  `.tix` reviewed and hardened, live store migrated, `connect.sh apply` done. T-23: `move` takes a role, `ctx`
  prints columns. T-12: repository in English, plans and research of closed tasks removed. T-3: README,
  demo and board picture, CI on macOS and Linux, release with 4 archives. CI found a board defect: an edit
  before the watcher start was not shown (fixed, test under CPU load). Checked: `make check`, `test-connect.sh`,
  CI green, downloaded archive passes `SHA256SUMS` and prints `0.1.0`.
