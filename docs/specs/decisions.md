# User decisions: tix

Part of the global spec `docs/spec.md` (section 12), moved out because of size. A decision number does not change;
a new decision is appended at the end, a cancelled one is marked, not deleted.

Accepted on 2026-10-20:

- D1. Our own, not a fork: the analogs are tied to a repository, have no external trackers and no md editor in the TUI.
- D2. An assistant with a tracker: the agent manages tasks, the human looks.
- D3. One md file per task in `~/tasks`, its own git.
- D4. A commit per edit: history, execution time, rollback, backup.
- D5. Columns idea, todo, doing, blocked, done; they can be added and removed.
- D6. Priority is the order in the column.
- D7. The agent manages through the CLI; the rules are in `~/tasks/RULES.md`.
- D8. Assistant: the role in `RULES.md`, a home session in `~/tasks`, scheduled work.
- D9. Editing in `$EDITOR`, a built-in editor later.
- D10. The default fields are the minimum.
- D11. Body and comments are free-form, templates are in `RULES.md`; format checks - only 2.5.
- D12. History comes from `git log` and the comments of the file.
- D13. The card is read by the user and the agents: technical details are allowed.
- D14. External trackers are connected by a tracker command, not by a skill.
- D15. Go. Rejected: Rust - the agent writes the code, Go has more ready parts for a TUI.
- D16. Local only, personal use; one machine in the MVP (D57).
- D17. Agents Claude Code, Codex, OpenCode; the minimum of tokens and time spent on the tracker.
- D18. The project does not grow, the focus is the MVP.
- D19. The architecture is approved; trackers and agents are adapters and connectors.
- D20. Columns are configured by the agent or by hand; custom fields - after the MVP.
- D21. MVP fields: `waiting`, `claim`, `due`, `tags`, `ask`.
- D22. Backup: push to a private remote.
- D23. Task: title, abstract, summary, details, comments.
- D24. Five column roles, one role is one column.
- D25. Abstract and summary are body sections; "what next" is the first line of the summary.
- D26. There is no separate summary screen.
- D27. The TUI is in the ASCII style.
- D28. Action history: took, moved, notes "did X".
- D29-D32. External trackers after the MVP per `docs/specs/integrations.md`: status in both directions; a change
  on both sides - the external tracker wins (D30); the result at `done` goes out as a comment (D31);
  in the MVP `external` is a link (D32).
- D33. The agent writes the code; acceptance is automatic.
- D34. Theme `ascii` in the MVP, `cards` after.
- D35. The MVP boundaries are the table of section 1.
- D36. `claim` for 3 hours; the agent drops it when done or extends it.
- D37. `RULES.md`: the remainder goes to a new task; new scope is a new task; the session result
  as a comment `Done / Decided / Waiting`.
- D38. The user's action is needed - a yellow star; `ask` says what is needed.
- D39. A claim belongs to a session; a takeover is explicit, `take --steal`.
- D40. `done` with unchecked criteria - refusal; `--force -m` closes.
- D41. A move to `blocked` requires `waiting`.
- D42. In the MVP the set of fields is fixed, the theme is built into the code.
- D43. The name is `tracker`, the command is `tk` (working name). Replaced by D67.
- D44. A reference standard of speed, lightness and reliability; the numbers are in section 7.
- D45. Cancelled on 2026-10-21 by decision D66.
- D46. Interface and format in English: CLI and TUI replies, file sections, `RULES.md`; the Russian
  strings of the spec and the mockup are a sample of meaning. The text of tasks is in any language.
- D47. A public repository for a portfolio: a snapshot of the working one per `~/.claude/templates/github/design.md`,
  task S5 after the MVP. The command stays `tk` (the conflict with wedow/ticket is accepted), the repository name is in S5.
- D48. Short id: `12` equals `T-12` in all commands.
- D49. The Claude Code connector is a plugin; Codex and OpenCode - a skill in `~/.agents/skills`.
- D50. Install: `go install`, binaries in the release, Homebrew tap, completions for zsh, bash, fish (S5).
- D51. `take` only from `ready`, `waiting`, `active`, otherwise a refusal with the hint `move`.
- D52. Leaving `waiting` clears the field `waiting`; the reason stays in the commit of the move.
- D53. macOS and Linux, amd64 and arm64; no Windows.
- D54. From the analogs (`docs/research/competitors.md`): hook PreCompact with `ctx` (S3), own exit code
  per kind of refusal (S2), examples and a format warning in the README (S5).
- D55. `done` of a task without criteria requires `-m`.
- D56. Position: memory of agents between sessions and projects; each edit leads to the session that made
  it (in the README worded this way, not as "a commit per edit"). `Actor:` stores the full session id, the short form only on screen. After the MVP -
  a command to open the session of an edit (`claude --resume`, `codex resume`). Obsidian is an addition.
- D57. After the MVP first: `blocked_by` and a selection of ready tasks (v0.2), `tix sync` for two machines.
- D58. Link to code: the commit of a task from a project folder carries the trailer `Code: <project>@<sha HEAD>`
  (HEAD is read in Go without git); `RULES.md` tells to commit code with `T-12` in the message and to write
  into the task after the code commit (S2).
- D59. Storage is plain git, with no database of our own and no go-git. Rejected: SQLite (no files for
  the agent and Obsidian, history to be written ourselves), Dolt (a heavy dependency, its own history instead of git),
  files with our own journal (half of git by hand). Analysis - `docs/api/git.md`, 2026-10-20.
- D60. Card history: consecutive edits by the same `Actor` - one line "N edits", `enter` expands it;
  `external` is a separate author; moves, notes and comments are always visible; commits stay
  one per edit (S4).
- D61. `tix sync` syncs two machines; external trackers are pulled by `tix pull` and the key `s` in the TUI.
  After the MVP.
- D62. Git lock files `.git/*.lock` and `refs/**/*.lock` older than 10 s are removed by `tix`; if the file is fresh - the reply is
  `git is busy`, no write is made (spec 2.1, section 3).
- D63. Minimum of traces: `tix` writes only to `~/tasks`; connecting an agent is a skill, the start hook `tix ctx` and
  the permission for `tix`, without plugins and marketplaces; service data goes to `~/tasks/.tix`. OpenCode - after the MVP.
  User decision 2026-10-20.
- D64. Speed is a guideline and an argument for the README, not a readiness condition: a task is closed by a green
  `make check`, `make bench` runs before a release. User decision 2026-10-21.
- D65. The MVP boundaries were rebuilt on 2026-10-21 (section 1): first a body of real tasks and a trial run, features grow after.
  Added to the MVP: mouse, pulse on the board, work log rules, the `Model:` trailer, English as the language
  of the repository. Discussion and briefing stay in the session chat, the task is the boundary between "discussed" and
  "doing". Task links are an investigation after the MVP. User decision 2026-10-21.
- D66. The human does not need the CLI: any action on tasks is done in the TUI, both by key and by mouse
  or touchpad. Cancels D45. User decision 2026-10-21 after the trial run of the board.
- D67. The public name is tix: repository daniil4545/tix, module github.com/daniil4545/tix, command tix,
  Homebrew through our own tap. tk is taken by wedow/ticket and 3-4 more trackers of the niche; the namesake Heliopsy/tix (3 stars,
  same niche) is an accepted risk. Name research and the check of tix on 2026-10-21. User
  decision 2026-10-21. Cancels the name ticket from D47.
- D68. Development moves to the public repository daniil4545/tix: it is the `origin` of the working copy, the
  whole history and the internal docs are public. The tix repository becomes the bench of the admitd agent farm:
  agents send PRs, the owner reviews and merges. User decision 2026-10-23 ("tix is fully public, a pet project,
  no reason to hide"). Cancels the snapshot from D47; `scripts/snapshot.sh` is removed.
