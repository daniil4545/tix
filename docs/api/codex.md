---
system: Codex CLI
version: codex-cli 0.163.0
checked_at: 2026-10-20
checked_by: recon (live codex exec + codex sandbox + documentation)
---

# Codex CLI: sandbox, rules, session id

Sources: [L] live `codex exec` on 2026-10-20 with a temporary CODEX_HOME; [S] `codex sandbox`; [D] learn.chatgpt.com/docs/agent-configuration/rules (page without a date, read on 2026-10-20; developers.openai.com/codex/rules redirects there).

| Fact | Source | For tix |
|---|---|---|
| workspace-write by default writes to the working folder, /tmp and $TMPDIR (policy: exclude_slash_tmp=false, exclude_tmpdir_env_var=false). A write outside them gives `Operation not permitted` | [L] rollout sessions, [S] | ~/tasks is not in this list: a write and a commit from the sandbox fail |
| `.git` is read-only even inside the working folder: `git add` fails with `Unable to create .git/index.lock: Operation not permitted` | [S] with exclude_slash_tmp=true, working folder in ~/.cache | git commits by tix are impossible without leaving the sandbox anywhere except /tmp |
| A check only in /tmp is falsely green: a write and a commit to an external repo in /private/tmp passed in the sandbox | [L] | Do not run tix tests in /tmp temporary directories |
| `prefix_rule(pattern=["tix"], decision="allow")` in `<CODEX_HOME>/rules/default.rules`: `codex execpolicy check --rules F -- tix add x` gives decision allow | [L] | The rule parses and matches by the prefix `tix` |
| allow = the command runs outside the sandbox without a prompt; prompt asks; forbidden blocks; with several matches the strictest wins. `bash -lc` is split into commands only for simple chains, otherwise it is checked whole | [D] | Call `tix ...` as a separate simple command; `cd x && tix` and pipes may not match |
| The allow rule `prefix_rule(["tix"], "allow")` takes only a single command `tix ...` out of the sandbox (with on-request and never; every segment of the command must match the rule). A compound command (`tix ...; echo`, `&&`, a pipe) stays in the sandbox and fails on `.git/index.lock` | [L] live run on 2026-10-20 with workspace-write, approval_policy=never: a single `tix add x` ran outside the sandbox (`CODEX_SANDBOX` empty), a compound one failed; [C] exec_policy.rs:440-456, tools/sandboxing.rs:249-295 | Option C works: the skill tells the agent to call `tix` as a single command, without `;`, `&&`, pipes and `cd`. Exception: a policy with a `deny` read |
| Environment of session commands: CODEX_THREAD_ID (UUID, 36 chars, `XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX` hex), CODEX_SESSION_ID (the same value by prefix), CODEX_SANDBOX=seatbelt, CODEX_SANDBOX_NETWORK_DISABLED=1, CODEX_CI=1, CODEX_HOME, CODEX_VERSION, CODEX_TOOL_CALL_ID. It is also the thread_id in the `thread.started` event | [L] | Take the tix session id from CODEX_THREAD_ID (or CODEX_SESSION_ID) |
| The network in the sandbox is off (CODEX_SANDBOX_NETWORK_DISABLED=1) when network_access=false | [L] | tix must not use the network from the sandbox |
| Rules: `*.rules` files in `rules/` next to the active config layer (`~/.codex/rules/default.rules`); project `<repo>/.codex/rules/` only in a trusted project. Syntax `prefix_rule(pattern=[...], decision=..., justification=..., match=[...], not_match=[...])` (Starlark). Marked experimental | [D] | setup appends a line to default.rules idempotently |
| Confirmation: a rule added through the TUI ("allow list") is written to `~/.codex/rules/default.rules`; with Smart approvals Codex offers a prefix_rule on escalation | [D] | A hand-made file is picked up at start without confirmation (except `codex exec` without the TUI: no confirmation needed, checked in a live run with a rule from a file) |
| Another CODEX_HOME: `CODEX_HOME=dir codex ...` reads config.toml, rules/ and auth.json from there; for runs without touching ~/.codex a symlink of auth.json is enough | [L] | Integration tests isolate through CODEX_HOME |
| `-c key=value` takes a TOML value: `sandbox_mode="workspace-write"` needs quotes | [L] | |
| Hook input on stdin, captured 2026-10-22 in `codex exec` 0.163: SessionStart `session_id, transcript_path, cwd, hook_event_name, model, permission_mode, source`; UserPromptSubmit and Stop add `turn_id` (Stop also `stop_hook_active, last_assistant_message`). `session_id` equals `CODEX_THREAD_ID`. Hooks run outside the sandbox | [L] wrapper `tix` on PATH logging stdin | `HookActor` matches `session_id` to the variables, `turn_id` marks Codex |
| A single `tix note` from `codex exec` (workspace-write, never) runs outside the sandbox by the allow rule and commits as `codex:<thread>`; `codex sandbox` ignores execpolicy and is no check for that | [L] 2026-10-22, store commit `a149008` | |
| The model gpt-5.5 is rejected with a ChatGPT account (400); gpt-6.1-sol works | [L] | Not critical for tix |

## Addendum 2026-10-20: writing to ~/tasks (recon codex-write)

Sources: [S] `codex sandbox` 0.163.0; [C] sources of tag rust-v0.163.0 (`codex-rs/`). A separate report was not kept, the facts are in the table.

| Fact | Source | For tix |
|---|---|---|
| `codex sandbox` 0.163 requires `-P <profile>` together with `-C`; without `-C` it takes `-c 'sandbox_mode="workspace-write"'` (old mode) or `default_permissions` | [S] | Run sandbox tests without `-C`, from the working folder |
| Protected names: `.git`, `.agents`, `.codex`, `.aws` (read-only in the working folder and in every writable root); a `.git` pointer file also protects the directory from `gitdir:` | [C] protocol/src/permissions.rs:40-43, 860, 2360-2403 | `.git` under this name is always closed |
| An explicit write for a path lifts the protection: `[permissions.x.filesystem] "<repo>" = "write"` and `"<repo>/.git" = "write"` allow a commit | [C] permissions.rs:2502; [S] `COMMIT_OK` | A `permissions` profile with `extends=":workspace"` opens ~/tasks, `.git/hooks` is writable too |
| `sandbox_workspace_write.writable_roots=[repo]`: files are written, `.git/index.lock` gives Operation not permitted | [S] | Not enough for a commit by itself |
| A git directory with another name (`<repo>/.tixgit`) without `.git` in the root + `GIT_DIR`/`GIT_WORK_TREE`: the commit passes in `writable_roots` and in a profile; with a `.git` pointer file it fails | [S] | A workaround by name works |
| `codex sandbox --allow-unix-socket <path>` allows a unix socket; without the flag it is refused. The profile key `network.unix_sockets` did not work | [S] | A daemon outside the sandbox is possible |
