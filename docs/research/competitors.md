# Analogs: wedow/ticket, its forks and beans

Date: 2026-10-20. Complements `landscape.md`, section 2: these projects did not appear in the first recon.
Sources: README, GitHub API and the code of the repositories (analyzed by agents down to file and line). Stars and
dates are as of 2026-10-20. Basis for the comparison in the README (S5).

## Who is who

| Project | Stars | Language | Alive | Storage | Write | git | Claim | Session start | TUI | MCP |
|---|---|---|---|---|---|---|---|---|---|---|
| wedow/ticket | 912 | bash | frozen since 2026-03, 42 issues and PRs open | `.tickets/` in the repo | sed, no lock | does not commit | no | a line in CLAUDE.md | no | no |
| hmans/beans | 952 | Go | silent since 2026-04 | `.beans/` in the repo | ETag, no lock | does not commit | no | hooks SessionStart and PreCompact, Claude plugin | yes | no, GraphQL |
| EnderRealm/ticket | 0 | Go | active, v8.8.0 | central | flock | autocommit every 5 s | no | no | yes | yes |
| lawrips/tkt | 3 | Go | push 2026-08, no releases | central | no lock, no rename | daemon every 30 s | no | `agent-setup` writes to CLAUDE.md | yes | 22 tools |
| msampathkumar/ticket | 2 | bash + Python | active, v0.5.2 | `.tickets/` in the repo | mkdir lock per task | does not commit | `start --claim`, no takeover | skill, no hook | web | no |
| Backlog.md (already in landscape) | 7078 | TS | active | in the repo | - | - | - | MCP | yes | yes |

The small forks of ticket (byronwall, namli, mjakl, skorokithakis) add a web view, `tk set`, a lock with
atomic write, a skill. Each has no more than 3 stars: demand is scattered, there is no leader among the forks.

## What confirms demand

- One store for many projects: ticket issues #15, #12, PR #29; the forks EnderRealm and lawrips moved to
  a central store.
- A task waits for a human: #39, PR #49, #50; the user josharian wrote `tk ask`/`tk answer` himself.
- A skill instead of a line in CLAUDE.md: #3, the author promised it on 2026-01-09 and did not deliver; the forks added a skill.
- Several agents in parallel: #33 (parallel Codex agents).
- Show HN ticket 2026-01-04: 84 points. People praise flat files and simplicity; they ask how to clean up
  old tasks and why store them in the repo.

## What nobody has

| Ours | Closest elsewhere |
|---|---|
| a commit per edit with `Actor:`: who, what, when | autocommit in batches every 5-30 s (EnderRealm, lawrips) |
| an edit that bypasses the core (Obsidian, by hand) is not lost: external edit | no |
| claim by session with the takeover `--steal` | `--claim` without takeover (msampathkumar) |
| `edit` on an old base with the merge `git merge-file` | ETag refusal (beans), web revision (lawrips) |
| `ctx` by start hook in Claude Code and Codex, skill in three agents | hooks only in beans and only for Claude |
| one Go binary with a speed measurement in the README | Go exists in the forks, nobody has measurements |

## What we take (user decisions of 2026-10-20)

- Short id `12` (the idea of ticket, by prefix) - D48, S2.
- A Claude Code plugin for installation (as in beans) - D49, S3.
- Install by `go install`, binaries, Homebrew tap, completions (requests in the ticket issues) - D50, S5.

## Candidates, decision not taken

| Idea | From | Where |
|---|---|---|
| hook PreCompact with `tk ctx`: context after the window is compacted | beans | S3 |
| own exit code per kind of refusal: busy, conflict, not found | msampathkumar (codes 3, 4) | S2 |
| `doctor`: a check of the installation and the connectors | lawrips | after the MVP, with `setup` |
| a README section with sample phrases to the agent and a warning about format changes before 1.0 | beans | S5 |
| a rule in `RULES.md`: the agent creates a new task after the human confirms | namli, skill | S2 |
| acceptance checks by command (`verify`) | EnderRealm | after the MVP |
| a CI check that the copies of the skill match | msampathkumar | S5 |

## Their presentation

ticket: no topics, no badges, no demo, no binaries. beans: banner, 5 badges, GoReleaser,
Homebrew tap, README order "goal, warning, features, install in 3 steps, agent
setup, examples". The forks have almost no releases with binaries (except EnderRealm).
