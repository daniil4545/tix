# State

Project tasks are kept in the tracker (`~/tasks`, project `tracker`): `tk list`, the board is `tk` without arguments.
After the merge of `z6-tix` the command is `tix`, `~/bin/tk` stays as a local link.

## In progress

- T-24 "Rename to tix": branch `z6-tix`, tree `../tracker-z6`, `b93f2a6`; `make check` and
  `scripts/test-connect.sh` are green. Step "Review": code review of the migration of `.tracker` to `.tix`, then
  merge with the user's confirmation and `connect.sh apply`.
- T-20 "A reply from the task reaches the agent": in `main`, hooks installed. Step "Acceptance": a live check
  on a new session (this one did not pick up the hooks), approval of the hooks in Codex `/hooks`.

## Next

1. T-24: review, merge `z6-tix`, `connect.sh apply`, check the board and the hooks under `tix`.
2. T-23 (`move` accepts a role name, `ctx` prints columns and roles) - in branch `z6-tix` before the merge or right after.
3. T-12: the repository in English (the rule of 2026-10-21 for public projects), agents by zones.
4. T-3: repository presentation (README with the manifest and scenarios, HTML pictures, demo), snapshot,
   CI, public `daniil4545/tix` with confirmation, tag `v0.1.0`. Branch `z5-publish` (`d635570`) is a draft.
5. After the MVP: T-27 (clicks on the question line, needs a modifier probe in Warp), T-5, T-7.

## Open

- The working repository has a remote `origin` at `github.com/daniil4545/ticket` (the repository does not exist
  on GitHub). By design the public repository is a separate snapshot with no shared remote: remove or
  replace the remote at release, do not push from the working one.
- Under load (parallel agents) one e2e test failed on 2026-10-21, could not be repeated: 3 e2e runs,
  2 full ones and `make check` are green. If it repeats - investigate.
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
