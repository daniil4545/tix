# tix - context for AI agents

## Project

A personal terminal task tracker for humans and agents: tasks are md files in `~/tasks` under git,
agents work through the `tix` CLI, the human works through the TUI. The user decides. The behavior
canon is `docs/spec.md`, tasks are in tix (project `tracker`), task plans in `docs/plans/`.

## Principles

- The reference for speed, lightness and reliability (D44): a part that nothing breaks without is removed.
- The agent writes the code, acceptance is automatic (D33): done only with a green `make check`. `make bench` runs before a release and for the README, it does not block a task (D64).

## Stack

Go, one `tix` binary; Bubble Tea from S4. Versions are in `go.mod`.

## Structure

Dependencies: `cli` - `core`, `brief`, `project`, `actor`; `core` - `task`, `gitx`, `config`, `assets`, `project`, `actor`; `brief` - `task`, `config`, `actor`. The map and flows are in `docs/architecture.md`.

| Path | Role |
|---|---|
| `cmd/tix` | CLI entry point |
| `cmd/tixbench` | speed benchmark of `tix` for `make bench` |
| `internal/cli` | command parsing and reply printing |
| `internal/core` | the only write path into `~/tasks`: lock, rename, commit |
| `internal/task` | parsing and printing of the task file, no IO |
| `internal/config` | `.tix/config.toml`: columns and roles |
| `internal/actor` | write identity: sources, name check |
| `internal/brief` | building the `tix ctx` output, no IO |
| `internal/assets` | `RULES.md`, embedded in the binary |
| `internal/gitx` | all git calls, the only `os/exec` in `internal` |
| `internal/project` | project by working folder and project HEAD, no git |
| `e2e/` | end-to-end black-box tests |

## Commands

| Command | What it does |
|---|---|
| `make check` | format, vet, linter, tests, build; acceptance |
| `make test-fast` | fast tier (`-short`); `RUN=Name` - one test |
| `make bench` | builds `bin/tix`, speed benchmark on 1000 tasks, including `ctx`, `take`, `move` |
| `make build` | `go build ./...` and `bin/tix`; `VERSION=1.2.3` sets `tix --version` |
| `make fmt` | `gofmt -l -w .` |
| `make e2e-agents` | stub until S3 |

## Workflow

Project profile for the pipeline `~/.claude/docs/pipeline.md`:

| Parameter | Value |
|---|---|
| Autonomy | the agent decides technical questions within the spec; it approves the task plan itself if `spec-reviewer` raised no questions about functionality; a question about behavior, anything irreversible and spending subscription limits go to the user |
| Subagents | every task runs through the `orchestrate` skill: plan, `spec-reviewer`, up to 5 concurrent agents (workers and tester), thorough `code-reviewer`, `skeptic` by `regress` |
| Tracker | tix, project `tracker`; the plan of a heavy task is `docs/plans/<task>.md` |
| Subagent header | `~/.claude/templates/subagent-brief.md` |
| Findings file | `docs/findings.jsonl` |

- Branches: a task goes in the branch `z<N>-<name>`, the merge into `main` happens at acceptance with the user's confirmation.
- Hosting and CI: `origin` is the public repository `daniil4545/tix` (D68); CI runs there. Agents of the admitd farm send PRs to it.

## Critical invariants

- Writes to `~/tasks` go only through the core: lock, temp file, rename, commit; there are no half-written files.
- An edit made around the core is not lost: the next operation commits it as an external edit.
- The core does not overwrite a file that does not parse.
- Only `take` sets a claim; a live claim of another actor is not taken over without `--steal`.
