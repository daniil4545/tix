# State

Project tasks are kept in the tracker (`~/tasks`, project `tracker`): `tix list`, the board is `tix` without arguments.
`~/bin/tk` and `~/bin/tix` both link to `bin/tix` of this repository.
Public repository `daniil4545/tix` is a snapshot: `scripts/snapshot.sh <dir>`, copy into a clone of the public
repository, one commit on top, push. Never push from this repository (it has no remote).

## In progress

- Pause for testing (owner, 2026-10-22): 0.3.0 in normal work, no new features until the next retro.

## Next

1. Retro after a few days of 0.3.0: board colors, ask kinds, agents keeping the task while asking.
2. Later: T-27 (clicks on the question line, needs a modifier probe in Warp), T-5, T-7; `project` in the card `f` menu.

## Open

- Branch `z2-rank-cache` (`be562f9`) is kept for T-5: rank cache without tests and review.
- vhs 0.12.1 skipped the `Screenshot` of `docs/board.tape`; the picture came from the last GIF frame (see the tape header).
- The grey card relies on the 3-hour claim: a closed window greys out only when the claim expires.
- Known race: the reply `a` may erase a new `ask` of the agent in a window of milliseconds; conditional write - later.
- The model in the `Model:` trailer is only for Claude Code; `/model` in the middle of a session and subagents - later.
- `make e2e-agents` and live Codex calls spend subscription limits: only with the user's consent.
- The docker image `golang:1.27` is kept for checking on Linux.

## Done

- 2026-10-22. Release 0.3.0 (`main` tag `v0.3.0`, public snapshot `2d0db33`): ask kinds by prefix (T-44), board
  color scheme in `internal/tui/view/scheme.go` (T-85: live claim colored, the rest grey, distinct project
  colors), `new` keeps the entry check and the board falls back to the inbox (T-82), lower-case projects and
  `set project=` (T-73), spec 2.3 (T-83), a flaky TUI test harness fixed. Checked: `make check`,
  `test-connect.sh`, two reviews and an Opus final review (no must-fix), CI and release green, the
  darwin_arm64 archive passes `SHA256SUMS` and prints `0.3.0`.
- 2026-10-22. Release 0.2.0 (`main` tag `v0.2.0`, public snapshot `ca67353`): T-71 and T-77, T-80. T-76 closed
  as not a defect: the allow rule takes a single `tix` out of the sandbox (live `codex exec` note committed);
  Codex is rarely used. Checked: `make check`, review and final review (no must-fix), CI and release green,
  the darwin_arm64 archive passes `SHA256SUMS` and prints `0.2.0`.
- 2026-10-22. T-20 closed: comment delivery checked live. Claude: a human comment arrived on the next
  PostToolUse. Codex exec 0.163: SessionStart, UserPromptSubmit and Stop hooks run, the model quoted the
  comment. Found: nested agent identity (T-80), lost note (T-77).
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
