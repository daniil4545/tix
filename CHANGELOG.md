# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

<!-- An entry stays one line: the release notes are cut from this file as they are. -->
<!-- markdownlint-disable MD013 -->

## [0.9.0] - 2026-11-13

### Upgrade from 0.8.x

- Copy the store (`~/tasks`) before installing. The first write of 0.9.0 moves the old epics to `parent` in one commit `migrate: epic lines to parent` and writes `format = 2`. Tasks an epic lists get `parent: T-N` and their lines leave the epic; a line that names a missing task or another epic stays a criterion.
- 0.8 does not read `parent`. To go back: `git revert` the migration commit in the store and install the 0.8 binary. 0.9.0 migrates a reverted store again on its next write.
- After work on 0.9 the revert can stop on conflicts: in a task whose `status`, `claim` or `rank` changed next to the new `parent:` line, and in `config.toml` after `tix status` or `tix projects add`. Keep the other fields, drop the `parent:` line and the `format = 2` line. A `T-N: external edit` commit right before the migration commit means the migration was cut off: revert that commit too.
- Skills and rules that write `- [ ] T-N` epic lines (`spec`, `orchestrate`, `RULES.md`) must be updated first: after the migration such a line is an ordinary criterion. Scripts that read status names should read `kind`.

### Added

- Statuses have a kind and live in `config.toml` as `[[status]]` (`name`, `kind`) next to the top-level `format = 2`. A config with no `[[status]]` means the five defaults. Rules read the kind; text output prints `review [in_progress]` when the name differs from the kind; `--json` has `kind`.
- `tix status` lists statuses with task counts; `tix status add NAME --kind KIND`, `rename OLD NEW` and `rm NAME` (refused while the status has tasks or is the last of its kind).
- `tix list --kind KIND` filters by kind.
- `tix uncheck T-N N` clears the mark of a criterion.
- TUI: board columns come from the `[[status]]` list of `config.toml`; a parent card shows `EPIC 3/7` by its children.
- `parent: T-N` links a task to an epic: `tix new --parent`, `tix set T-N parent=T-M` (`parent=` clears), `Parent:` and `Children: 3/7` in `show`, `[epic 3/7]` in `list` and `ctx`, `parent`, `children` and `children_closed` in `list --json`. Depth is one; a closed parent takes no new children.

### Changed

- An epic is a task with children. Closing it is refused while a child is open (`--force` too); closing the last child prints `epic T-N: all tasks closed, ready for acceptance`; deleting a parent with children is refused. The tag `epic` means nothing.
- Text output for scripts: `show` prints `Parent:` and `Children:` instead of `Epic: T-M`; `[epic]` in `list` and `ctx` is `[epic n/m]`; a task with the tag `epic` and no children is an ordinary task. `check` ticks a `- [ ] T-N` line like any criterion and `done` no longer ticks lines in epics.

## [0.8.1] - 2026-11-03

### Added

- The Codex connector gets the `SessionEnd` hook too (`tix take --release --hook`, 3 s timeout): reinstall with `scripts/connect.sh apply --force` and confirm it in Codex `/hooks`. The lock timeout (5 s) is above the Codex hook ceiling (3 s): with a busy lock the claim stays until it expires.

### Fixed

- The known project list of the refusal (code 5) names `personal` once.

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

See the [release notes](https://github.com/daniil4545/tix/releases/tag/v0.7.1).

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

[0.9.0]: https://github.com/daniil4545/tix/compare/v0.8.1...v0.9.0
[0.8.1]: https://github.com/daniil4545/tix/compare/v0.8.0...v0.8.1
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
