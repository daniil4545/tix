# T-44, T-82, T-73, T-83: ask kinds and a stale ask, entry check on new, project names

Owner decisions of 2026-10-22. Branch `z17-ask-kinds` from `main`. One commit per task is fine.

## 1. T-44: the star tells the kind of need and whether the session is alive

The star means "the human is needed". Its color now carries two facts.

| State | Star color | Pulse line (third line) | `g` (jump) |
|---|---|---|---|
| `ask` with kind `decide` (default), live claim | yellow (today's `askStyle`) | `? <text>` without the prefix | opens the tab, as today |
| `ask` with kind `review`, live claim | blue | same | same |
| `ask` with kind `access`, live claim | red | same | same |
| `ask` of any kind, no live claim (none, or expired by the model clock) | grey (muted) | same text, muted | status line `T-12: session ended; the answer stays in the task` |

- Format: the kind is a prefix of the `ask` value: `decide: ...`, `review: ...`, `access: ...`.
  No prefix means `decide`. Matching is on the raw value: `^(decide|review|access):\s*`.
- A prefix is a lowercase ASCII word directly followed by `:` at the start (`^[a-z]+:`). An unknown
  one (`foo: x`) is refused by `tix set` with a rule error (code 5) that lists the three kinds.
  `Note: x` (capital) or `x: y` after a space is plain text, not a prefix. Old asks without a prefix
  keep working as `decide`.
- One pure helper in `internal/task` parses the kind and the text (no IO); core validation, the board,
  the card and `ctx` (if it prints ask) use it. Do not duplicate the regexp.
- The card (`a` answer, field view) shows the full value; the answer flow (`Answer: <text>` comment,
  then `ask=`) is unchanged. The comparison of the current ask in `startAnswer` stays on the raw value.
- Selected row: keep the existing selected/grab styles, only the star color changes by state.
- Live claim = the same rule as `focusURL` (claim present and `Until` after the model clock). Reuse it.
- Colors: add styles next to `askStyle` in `internal/tui/view`; keep the palette rule of spec 3
  (main, muted, accent + the star). Red and blue are the terminal's ANSI red and blue.

## 2. T-82: `new` into a column with an entry check refuses

- `tix new --to <col>` applies the same entry check that `tix move` applies to that column, to the
  new task (no criteria, no waiting reason). Ready, waiting and closed then refuse with a rule error
  (code 5) before any write. The message names the path, for example: `a new task has no criteria;
  create it, add criteria with tix edit, then run: tix move <id> <col>`. Inbox, active and role-less
  columns stay allowed if `move` allows them. Reuse the core entry-check function; do not write a
  second rule table.
- The TUI "new task" path uses the same core call; check it still works (it creates in inbox?).
- Fix every test, e2e test and script that relies on `new --to todo` / `--to ready`
  (`internal/cli/cli_test.go` `TestCLI_New_ToRole`, `internal/cli/agent_test.go`, `e2e/`,
  `scripts/`): create, add criteria, move. Keep what each test checks.

## 3. T-73: project names in lower case; change a task's project with set

- `Store.Create` stores the project in lower case (`strings.ToLower` after trim). Comparisons that
  lowercase today stay (old files may have upper case) or move to one helper; do not break filters
  for old data.
- `tix set T-12 project=<name>`: new settable field, lower-cased, one line, not empty. Add it to the
  allowed list in core and `internal/task/edit.go`, to `commandHelp["set"]` in `internal/cli/cli.go`,
  and to the TUI field choice if that list is driven by the same names.
- Leave `.tix/config.toml` mapping `Galera = "galera"` in the live store alone (not in the repo).

## 4. T-83: spec 2.3 fold text matches `internal/history/history.go`

Read `foldable` and spec section 2.3 (the card history), rewrite the spec sentence to what the code
does. Docs only.

## Docs (same branch)

- `docs/spec.md`: board section (star colors), 2.3 fold text, set fields, new entry rule, project case.
- `internal/assets/RULES.md`: one line on ask kinds, set fields list gains `project`, `new` rule.
  Update `RulesHash` in `internal/assets/assets.go` and add the old hash to `previousRules`.
- `connectors/agents/skills/tix/SKILL.md`: at most one short mention of `review:`/`access:`; the file
  must stay under 1536 bytes (`scripts/test-connect.sh` checks it).
- `README.md` if it lists set fields or the star; `CHANGELOG.md` `[Unreleased]`: Added (ask kinds,
  set project), Changed (grey star without a session, new refuses checked columns, project lower case).

## Test scenarios (write first, see them fail)

1. Parse: no prefix, each kind, unknown lowercase word, capitalized word, word after a space, empty.
2. `set ask="foo: x"` refused code 5, store unchanged; `set ask="review: x"` stored as is.
3. Board render: yellow / blue / red with a live claim; grey without a claim and with an expired one
   (model clock); pulse line has no prefix. Check color codes like the existing star test.
4. `g` on an ask without a live claim gives the "session ended" status.
5. `new --to ready` refused code 5, nothing written; `new` and `new --to <inbox>` still work.
6. Create with project `Galera` stores `galera`; `set project=Tarn` stores `tarn`; empty refused.

Acceptance: `make check` green; `scripts/test-connect.sh` green.
