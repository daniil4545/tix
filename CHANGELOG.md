# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.8.0] - 2026-11-03

### Upgrade from 0.7.1

- Reinstall the agent skill: `scripts/connect.sh apply --force`. It adds the SessionEnd hook and the new rules (`projects`, `list --like`); `RULES.md` you did not edit is replaced on the first write.
- Fill the project catalog: `tix projects add NAME PATH` for each project. Once `[paths]` in `config.toml` is not empty, `tix new` (with `--project X` or the project of the current folder), `tix set T-N project=X` and `n` in the TUI refuse a name outside the catalog and not `personal` (code 5, the known projects and the add command). An empty catalog keeps the old behaviour; old tasks in unknown projects stay editable. The TUI reads the catalog at start: restart it after `tix projects add`.
- Older binaries ignore `[paths]`.

### Added

- `tix projects` lists the catalog (project, path, open tasks; `not in catalog` for a project only tasks mention); `tix projects add NAME [PATH]` registers one. There is no `rm`: edit `config.toml`.
- `tix list --like TEXT|T-N` lists tasks of the project that share word stems with the text or with T-N, most similar first (epics and closed tasks excluded, a one-word query finds nothing). To merge a duplicate by hand: move its criteria into the older task with `tix edit`, then `tix done T-N --force -m "duplicate of T-M"`.
- `tix take` prints `project: NAME /path docs: AGENTS.md CLAUDE.md` and `tix ctx` prints `path: /path` for a project with a path.
- A body line `After: T-1, T-2` keeps a task out of `next` in `tix ctx` until those tasks close; `list` shows `| after T-1`, `show` the open numbers. `take` still works.
- `tix take --release` without a number drops every live claim of the session in one commit (`ok no claims` if none). The Claude connector runs it from a SessionEnd hook, so a closed window frees its cards.
- TUI: `p` opens a project list with a substring filter and open counts; `f` searches title, Abstract and criteria (same match as `list --grep`), `tab` switches project and all, `ctrl+t` shows closed tasks.

### Changed

- `make bench` target for `show` is 20 ms (was 15): the 1000-task bench measured 19 ms on 0.7.1 and 0.8.0 alike.

- `p` in the TUI no longer cycles projects. The board hint line is over 100 characters; on a narrow screen `q quit` is cut.
- `project.Detect` prefers the longest registered path that contains the folder, then the git lookup.
- Agents merge their task PRs through `merge-gate` after one review; the release tag stays with the owner.

## [0.7.1] - 2026-11-02

### Upgrade from 0.7.0

- An untouched `RULES.md` in the store is replaced by the new text on the first write; one you edited yourself stays.
- Reinstall the agent skill (`scripts/connect.sh apply --force`) or add the `stale 14d+` line to your own copy: it tells the agent to offer the review and never close or delete by itself.
- `tix ctx` now runs one `git log` over the last 14 days: about 17 ms on the 1000-task bench (target 25 ms, was 15) and longer on a store with thousands of recent commits.

### Added

- `tix ctx` prints `stale 14d+: N (T-1, ...)` (up to 10 ids, most idle first) for idea and open tasks without a move for 14 days; `--json` has `stale`. A move is a `new`, `note`, `comment`, `move`, `take`, `steal`, `done`, `edit` or `check` commit; `set`, `release`, external edits and mass commits of tix do not count.
- `tix list --stale` keeps those tasks with `| idle Nd`; `--json` has `idle_days`.

### Changed

- `tix list --grep` matches the title, the `## Abstract` section and the criteria, ignoring case. Notes, comments and history are not searched.
- CI runs `make ci-check` on Linux and macOS for every PR into `main` and every push to it; `main` is the only long branch.

### Fixed

- Closing an epic checks the task status behind a ticked line too: a line ticked by hand for an open or missing task refuses the close, `--force` included.
- A move that renumbers ranks no longer drops the epic line edit when the epic is one of the renumbered neighbours.

## [0.7.0] - 2026-11-02

See the [release notes](https://github.com/daniil4545/tix/releases/tag/v0.7.0).

## [0.6.0] - 2026-10-30

See the [release notes](https://github.com/daniil4545/tix/releases/tag/v0.6.0).

## [0.5.0] - 2026-10-26

See the [release notes](https://github.com/daniil4545/tix/releases/tag/v0.5.0).

## [0.4.0] - 2026-10-26

See the [release notes](https://github.com/daniil4545/tix/releases/tag/v0.4.0).

## [0.3.0] - 2026-10-22

See the [release notes](https://github.com/daniil4545/tix/releases/tag/v0.3.0).

## [0.2.0] - 2026-10-22

See the [release notes](https://github.com/daniil4545/tix/releases/tag/v0.2.0).

## [0.1.1] - 2026-10-21

See the [release notes](https://github.com/daniil4545/tix/releases/tag/v0.1.1).

## [0.1.0] - 2026-10-21

See the [release notes](https://github.com/daniil4545/tix/releases/tag/v0.1.0).

[Unreleased]: https://github.com/daniil4545/tix/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/daniil4545/tix/compare/v0.7.1...v0.8.0
[0.7.1]: https://github.com/daniil4545/tix/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/daniil4545/tix/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/daniil4545/tix/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/daniil4545/tix/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/daniil4545/tix/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/daniil4545/tix/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/daniil4545/tix/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/daniil4545/tix/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/daniil4545/tix/releases/tag/v0.1.0
