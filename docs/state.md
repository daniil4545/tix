# State

Project tasks are kept in the tracker (`~/tasks`, project `tracker`): `tix list`, the board is `tix` without arguments.
`~/bin/tk` and `~/bin/tix` both link to `bin/tix` of this repository.
Public repository `daniil4545/tix` is a snapshot: `scripts/snapshot.sh <dir>`, copy into a clone of the public
repository, one commit on top, push. Never push from this repository (it has no remote).

## In progress

- Trial run of v0.1.1 in normal work (user, from 2026-10-21): agents now open a tix task by default.

## Next

1. T-39: philosophy of tools (essay and agent rule), a separate session during the trial.
2. T-44: an `ask` left by a closed session; jumping to its author fails. Decide what happens to the question.
3. After the MVP: T-27 (clicks on the question line, needs a modifier probe in Warp), T-5, T-7.

## Open

- Branch `z2-rank-cache` (`be562f9`) is kept for T-5: rank cache without tests and review.
- Codex cannot write `~/tasks` from the `workspace-write` sandbox, `writable_roots` keeps `.git` closed (T-76).
- A Codex started from a Claude shell inherits `CLAUDE_CODE_SESSION_ID` and acts as the parent (T-80).
- Known race: the reply `a` may erase a new `ask` of the agent in a window of milliseconds; conditional write - v0.2.
- The model in the `Model:` trailer is only for Claude Code; `/model` in the middle of a session and subagents - v0.2.
- `make e2e-agents` and live Codex calls spend subscription limits: only with the user's consent.
- The docker image `golang:1.27` is kept for checking on Linux.

## Done

- 2026-10-22. T-20 closed: comment delivery checked live. Claude: a human comment arrived on the next
  PostToolUse. Codex exec 0.163: SessionStart, UserPromptSubmit and Stop hooks run, the model quoted the
  comment. Why Codex never wrote: the sandbox (T-76). Found: nested agent identity (T-80), lost note (T-77).
- 2026-10-22. Retro of agent use (566 store commits, 323 calls) and T-71 in `main` (`5a5b33f`): `done`
  clears `ask`, `take --release` puts an active task at the head of ready, `new --to` takes a role,
  `<command> --help`, the full hint for `take` without criteria, release is not folded in the history;
  RULES.md and the skill: frozen means inbox with tag `frozen`. Store: the frozen backlog of T-48 split back
  into tasks. Checked: `make check`, `test-connect.sh`, a thorough review (no must-fix), `connect.sh apply`.

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
- 2026-10-21. T-38 and T-32, release v0.1.1 (public snapshot `0541ca8`). tix is the default tracker in the
  global layer (`~/.claude` `4b711c0`: CLAUDE.md, pipeline.md (the tix tracker section), skills spec, finish, grill,
  commit, templates) and in the tracker and tarn profiles; `docs/backlog.md` moved into T-41 and T-42. The core
  refreshes an untouched store `RULES.md` (live store refreshed). Checked: `make check`, `test-connect.sh`, CI.
