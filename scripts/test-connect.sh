#!/usr/bin/env bash
# Tests for scripts/connect.sh.
#
# Each test runs on its own HOME from mktemp. The real HOME is neither read nor written:
# before running connect.sh the test checks that HOME is swapped, and at the end it compares the
# fingerprint (stat, no content read) of the real HOME files with the fingerprint before the run.
# No agents or models run; claude is not in the test PATH: the script does not need it.
#
# Run: scripts/test-connect.sh [test_name ...]
# Environment: TIX_BIN=<path to the real tix> - run the hook on it too (else a stub);
#              TIX_TEST_KEEP=1 - keep the temporary directory.
set -uo pipefail

SELF_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO=$(cd "$SELF_DIR/.." && pwd)
CONNECT=$REPO/scripts/connect.sh
FIX=$REPO/connectors/testdata
CONN=$REPO/connectors
SKILL_SRC=$CONN/agents/skills/tix
NOW1=2026-10-20T10:00:00Z
NOW2=2026-10-20T11:00:00Z
MATCHER_WORDS="startup resume clear compact"

# The real HOME of the account, not the environment variable.
REAL_HOME=$(eval "echo ~$(id -un)")
[ -n "$REAL_HOME" ] && [ "$REAL_HOME" != "~$(id -un)" ] || { echo "cannot determine the real HOME" >&2; exit 2; }

BASE=$(mktemp -d "${TMPDIR:-/tmp}/tix-connect-test.XXXXXX") || exit 2
BASE=$(cd -P "$BASE" && pwd)
HOMES=$BASE/homes
TOOLS=$BASE/tools
mkdir -p "$HOMES" "$TOOLS" "$BASE/tmp"

cleanup() {
  [ "${TIX_TEST_KEEP:-}" = 1 ] && { echo "temporary directory kept: $BASE" >&2; return; }
  case "$BASE" in */tix-connect-test.??????) rm -rf -- "$BASE" ;; esac
}
trap cleanup EXIT

# ---------- fingerprint of the real HOME ----------

if stat --version >/dev/null 2>&1; then STAT_SIG=(stat -L -c '%Y %s %i %a'); STAT_MODE=(stat -L -c %a)
else STAT_SIG=(stat -L -f '%m %z %i %Lp'); STAT_MODE=(stat -L -f %Lp); fi

mode() { "${STAT_MODE[@]}" "$1"; }
sha() { if command -v shasum >/dev/null 2>&1; then shasum -a 256 <"$1" | cut -d' ' -f1; else sha256sum <"$1" | cut -d' ' -f1; fi; }

REAL_WATCH=(
  .claude/settings.json .claude/skills/tix .codex/hooks.json .codex/rules/tix.rules
  .codex/rules/default.rules .codex/config.toml .agents/skills/tix tasks/.tix/connect-state.json
)

real_fingerprint() {
  local p f
  for p in "${REAL_WATCH[@]}"; do
    f=$REAL_HOME/$p
    if [ -e "$f" ] || [ -L "$f" ]; then
      printf '%s %s %s\n' "$p" "$(readlink "$f" 2>/dev/null || echo -)" "$("${STAT_SIG[@]}" "$f")"
    else
      printf '%s absent\n' "$p"
    fi
  done
  find "$REAL_HOME/.claude" "$REAL_HOME/.codex" \
    -maxdepth 2 -name '*.tix-bak-*' 2>/dev/null | LC_ALL=C sort
}

REAL_BEFORE=$(real_fingerprint)

# ---------- environment: tool directory and tix stub ----------

# The test PATH is this directory only: it shows that the script needs jq and tix,
# and any tool can be removed.
BASE_TOOLS=(bash env sh cat mv cp rm mkdir date dirname basename realpath stat chmod ln mktemp sed awk
  grep sort cut tr head tail readlink cmp diff ls printf touch find shasum sha256sum tee wc uname id
  xargs uniq tput rmdir mkfifo sleep expr test true false)

link_tools() { # directory, exclusion
  local dir=$1 skip=${2:-} t p
  mkdir -p "$dir"
  for t in "${BASE_TOOLS[@]}" jq; do
    [ "$t" = "$skip" ] && continue
    p=$(command -v "$t" 2>/dev/null) && [ -x "$p" ] && ln -sf "$p" "$dir/$t"
  done
  [ "$skip" = tix ] && return 0
  if [ -n "${TIX_BIN:-}" ]; then ln -sf "$TIX_BIN" "$dir/tix"; else cp "$BASE/tix-stub" "$dir/tix"; fi
}

# tix stub: knows `ctx`, `ctx --hook` and `inbox --hook` (empty reply); any other form exits with code 64.
cat >"$BASE/tix-stub" <<'EOF'
#!/bin/sh
if [ "$#" -eq 2 ] && [ "$1" = inbox ] && [ "$2" = --hook ]; then
  cat >/dev/null 2>&1 || true
  exit 0
fi
if { [ "$#" -eq 1 ] && [ "$1" = ctx ]; } || { [ "$#" -eq 2 ] && [ "$1" = ctx ] && [ "$2" = --hook ]; }; then
  cat >/dev/null 2>&1 || true
  echo "tix ctx: stub, 0 tasks"
  exit 0
fi
echo "tix stub: unexpected args: $*" >&2
exit 64
EOF
chmod +x "$BASE/tix-stub"
link_tools "$TOOLS"
for x in jq tix; do link_tools "$BASE/tools-no-$x" "$x"; done

# Migrating the .tracker store to .tix is the core's job: tests of the old install use the real tix.
REAL_TIX=${TIX_BIN:-$BASE/tix-real}
if [ -z "${TIX_BIN:-}" ]; then
  (cd "$REPO" && go build -o "$REAL_TIX" ./cmd/tix) || { echo "go build tix failed" >&2; exit 2; }
fi
TIX_BIN=$REAL_TIX link_tools "$BASE/tools-real"

# ---------- checks and helpers ----------

fail() { echo "    FAIL: $*" >&2; exit 1; }

assert_fake_home() { # [path]; HOME is checked by default
  local h=${1-${HOME:-}}
  [ -n "$h" ] && [ "$h" != "$REAL_HOME" ] && [ "$h" != / ] || return 1
  case "$h" in "$HOMES"/*) return 0 ;; *) return 1 ;; esac
}

new_home() { # creates and exports the test HOME; H holds the same value
  H=$(mktemp -d "$HOMES/h.XXXXXX") || fail "mktemp"
  H=$(cd -P "$H" && pwd)
  export HOME=$H
  ORIG=$H.orig
  mkdir -p "$ORIG"
  assert_fake_home || fail "HOME is not swapped: $HOME"
}

RC=0 OUT="" ERR="" RUN_ENV=()
run() { # run <connect.sh arguments>; result in RC, OUT, ERR
  assert_fake_home || fail "HOME is not swapped before running connect.sh: $HOME"
  [ -f "$CONNECT" ] || fail "scripts/connect.sh does not exist"
  local tools=${TOOLS_OVERRIDE:-$TOOLS}
  OUT=$(env -i HOME="$HOME" PATH="$tools" TERM=dumb LANG=C TMPDIR="$BASE/tmp" ${RUN_ENV[@]+"${RUN_ENV[@]}"} \
    "$tools/bash" "$CONNECT" "$@" 2>"$H.err" </dev/null)
  RC=$?
  ERR=$(cat "$H.err")
}
all_out() { printf '%s\n%s' "$OUT" "$ERR"; }
apply() { run apply --agent all --now "${1:-$NOW1}"; [ "$RC" -eq 0 ] || fail "apply: code $RC; $(all_out | head -5)"; }
undo() { run undo --agent all --now "${1:-$NOW2}"; [ "$RC" -eq 0 ] || fail "undo: code $RC; $(all_out | head -5)"; }

jq_eq() { # jq_eq <file-a> <file-b> <filter> <label>: equality by jq -S
  local a b
  a=$(jq -S "$3" "$1" 2>&1) || fail "$4: not JSON: $1"
  b=$(jq -S "$3" "$2" 2>&1) || fail "$4: not JSON: $2"
  [ "$a" = "$b" ] || fail "$4: JSON differs ($1 and $2)"$'\n'"$(diff <(echo "$a") <(echo "$b") | head -12)"
}
jq_ok() { jq -e "$2" "$1" >/dev/null 2>&1 || fail "${3:-$2}: not satisfied for $1"; }
same_bytes() { cmp -s "$1" "$2" || fail "${3:-file changed}: $1"; }
must_exist() { [ -e "$1" ] || [ -L "$1" ] || fail "missing $1"; }
must_absent() { [ ! -e "$1" ] && [ ! -L "$1" ] || fail "left over: $1"; }
baks() { find "$HOME" -name '*.tix-bak-*' | LC_ALL=C sort; }
# Our hook groups in all events; event arrays left empty after removal go too.
DROP_OURS='.hooks |= (with_entries(.value |= map(select(any(.hooks[]?; .command == "tix ctx" or .command == "tix ctx --hook" or .command == "tix inbox --hook") | not))) | with_entries(select(.value | length > 0)))'
MANIFEST() { echo "$HOME/tasks/.tix/connect-state.json"; }
files_in_home() { (cd "$HOME" && find . -mindepth 1 \( -type f -o -type l \) | LC_ALL=C sort); }

snap() { # snapshot of the files connect.sh touches: path, link, mode, hash
  (
    cd "$HOME" || exit 1
    { find .claude -maxdepth 1 \( -type f -o -type l \); find .claude/skills .codex .agents tasks \( -type f -o -type l \); } 2>/dev/null |
      LC_ALL=C sort | while read -r f; do
        [ -L "$f" ] && printf 'L %s -> %s\n' "$f" "$(readlink "$f")"
        [ -f "$f" ] && printf 'F %s %s %s\n' "$f" "$(mode "$f")" "$(sha "$f")"
      done
  )
}

seed_claude() { mkdir -p "$HOME/.claude"; cp "$FIX/$1" "$HOME/.claude/settings.json"; chmod 600 "$HOME/.claude/settings.json"; cp "$FIX/$1" "$ORIG/settings.json"; }
seed_codex() {
  mkdir -p "$HOME/.codex/rules"
  cp "$FIX/codex-hooks.json" "$HOME/.codex/hooks.json"; cp "$FIX/codex-hooks.json" "$ORIG/hooks.json"
  cp "$FIX/codex-default.rules" "$HOME/.codex/rules/default.rules"; cp "$FIX/codex-default.rules" "$ORIG/default.rules"
  cp "$FIX/codex-config.toml" "$HOME/.codex/config.toml"; cp "$FIX/codex-config.toml" "$ORIG/config.toml"
}
seed_all() { seed_claude claude-settings.json; seed_codex; }

# ---------- tests ----------

t_guard_rejects_real_home() {
  assert_fake_home "$REAL_HOME" && fail "assert_fake_home accepted the real HOME"
  assert_fake_home /tmp && fail "assert_fake_home accepted a foreign directory"
  assert_fake_home "" && fail "assert_fake_home accepted an empty HOME"
  new_home; assert_fake_home || fail "the swapped HOME was rejected"
}

t_validate_connectors() {
  new_home
  local f
  while IFS= read -r f; do jq empty "$f" 2>/dev/null || fail "not JSON: $f"; done < <(find "$CONN" -name '*.json' -not -path '*/testdata/claude-settings-broken.json')
  [ -f "$SKILL_SRC/SKILL.md" ] || fail "missing $SKILL_SRC/SKILL.md"
  [ -f "$CONN/claude/hooks.json" ] || fail "missing connectors/claude/hooks.json"
  [ ! -e "$CONN/claude/plugins" ] && [ ! -e "$CONN/claude/.claude-plugin" ] || fail "Claude plugin left over"
  [ ! -e "$CONN/opencode" ] || fail "connectors/opencode left over"
  [ "$(find "$CONN" -name SKILL.md -not -path '*/testdata/*' | wc -l | tr -d ' ')" = 1 ] || fail "skill copies are not exactly one"
}

t_plan_writes_nothing_and_lists_targets() {
  new_home
  local before after
  before=$(snap)
  run plan --agent all
  [ "$RC" -eq 0 ] || fail "plan: code $RC: $(all_out | head -5)"
  run plan
  [ "$RC" -eq 0 ] || fail "plan without arguments (default): code $RC"
  after=$(snap)
  [ "$before" = "$after" ] || fail "plan changed HOME files"
  [ -z "$(files_in_home)" ] || fail "plan created files: $(files_in_home)"
  run plan --agent all
  all_out | grep -qF 'Bash(tix *)' || fail "plan does not name the key Bash(tix *): $(all_out | head -5)"
  all_out | grep -qF 'hooks.json' || fail "plan does not name .codex/hooks.json"
}

t_apply_clean_writes_exact_paths() {
  new_home
  apply
  local want
  want=$(printf '%s\n' ./.agents/skills/tix/SKILL.md ./.claude/settings.json ./.claude/skills/tix/SKILL.md \
    ./.codex/hooks.json ./.codex/rules/tix.rules ./tasks/.tix/connect-state.json | LC_ALL=C sort)
  [ "$(files_in_home)" = "$want" ] || fail "HOME file set differs:"$'\n'"$(diff <(echo "$want") <(files_in_home) | head -8)"
  [ -z "$(baks)" ] || fail "backups left after apply: $(baks)"

  local s=$HOME/.claude/settings.json
  jq_ok "$s" '.permissions.allow | index("Bash(tix *)") != null' 'Claude: Bash(tix *) in allow'
  jq_ok "$s" '[.hooks.SessionStart[] | select(.matcher == "startup|resume|clear|compact") | .hooks[] | select(.command == "tix ctx --hook")] | length == 1' 'Claude: SessionStart group with tix ctx --hook'
  jq_ok "$s" 'keys | sort == ["hooks","permissions"]' 'Claude: extra keys in settings.json'
  local h=$HOME/.codex/hooks.json
  jq_ok "$h" '[.hooks.SessionStart[] | select(.matcher == "startup|resume|clear|compact") | .hooks[] | select(.command == "tix ctx")] | length == 1' 'Codex: SessionStart group with tix ctx'
  local ev
  for ev in PostToolUse UserPromptSubmit Stop; do
    jq_ok "$s" "[.hooks.${ev}[] | .hooks[] | select(.command == \"tix inbox --hook\" and .timeout == 10)] | length == 1" "Claude: hook $ev with tix inbox --hook"
    jq_ok "$h" "[.hooks.${ev}[] | .hooks[] | select(.command == \"tix inbox --hook\" and .timeout == 10)] | length == 1" "Codex: hook $ev with tix inbox --hook"
  done
  jq_ok "$s" '.hooks.PostToolUse[0].matcher == "*"' 'Claude: matcher PostToolUse'
  jq_ok "$h" '.hooks.Stop | length == 1' 'Codex: one Stop group in a clean HOME'
  grep -qF 'prefix_rule(pattern=["tix"], decision="allow")' "$HOME/.codex/rules/tix.rules" || fail "Codex: tix.rules lacks prefix_rule for tix"
  jq_ok "$(MANIFEST)" '.version == 1 and (.entries | length) > 0' 'manifest'

  run plan --agent all
  [ "$RC" -eq 0 ] || fail "repeated plan: code $RC"
  [ -z "$(printf '%s' "$OUT" | tr -d '[:space:]')" ] || fail "repeated plan is not empty: $(echo "$OUT" | head -5)"
}

t_tix_home_sets_manifest_place() {
  new_home
  RUN_ENV=("TIX_HOME=$H/alt")
  apply
  jq_ok "$H/alt/.tix/connect-state.json" '.version == 1' 'manifest in TIX_HOME'
  must_absent "$HOME/tasks"
  undo
  RUN_ENV=()
  must_absent "$H/alt"
  [ -z "$(files_in_home)" ] || fail "files left after undo: $(files_in_home)"
}

t_apply_twice_no_changes() {
  new_home; seed_all
  apply "$NOW1"
  local s1 s2
  s1=$(snap)
  apply "$NOW2"
  s2=$(snap)
  [ "$s1" = "$s2" ] || fail "the second apply changed files:"$'\n'"$(diff <(echo "$s1") <(echo "$s2") | head -10)"
  [ -z "$(baks)" ] || fail "backups left: $(baks)"
}

t_backups_live_only_during_apply() {
  new_home; seed_all
  chmod 640 "$HOME/.codex/hooks.json"
  apply
  [ -z "$(baks)" ] || fail "a successful apply left backups: $(baks)"
}

t_failed_apply_keeps_and_names_backups() {
  new_home; seed_all
  chmod 555 "$HOME/.codex/rules"
  run apply --agent all --now "$NOW1"
  chmod 755 "$HOME/.codex/rules"
  [ "$RC" -ne 0 ] || fail "apply returned 0 although tix.rules cannot be written"
  local f
  for f in .claude/settings.json .codex/hooks.json; do
    set -- "$HOME/$f".tix-bak-*
    [ -e "$1" ] && [ "$#" -eq 1 ] || fail "expected one backup next to $f, got: $*"
    same_bytes "$1" "$ORIG/$(basename "$f")" "backup differs from the original file"
    [ "$(mode "$1")" = "$(mode "$HOME/$f")" ] || fail "backup mode $(mode "$1") differs from the target mode"
    all_out | grep -qF "$1" || fail "output does not name the backup $1: $(all_out | head -6)"
  done
  undo
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" . 'settings.json after undo of an incomplete apply'
  jq_eq "$HOME/.codex/hooks.json" "$ORIG/hooks.json" . 'hooks.json after undo of an incomplete apply'
}

t_undo_restores_original_json() {
  new_home; seed_all
  apply; undo
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" . 'Claude settings.json after undo'
  jq_eq "$HOME/.codex/hooks.json" "$ORIG/hooks.json" . 'Codex hooks.json after undo'
  same_bytes "$HOME/.codex/rules/default.rules" "$ORIG/default.rules"
  same_bytes "$HOME/.codex/config.toml" "$ORIG/config.toml"
  must_absent "$HOME/.codex/rules/tix.rules"
  must_absent "$HOME/.agents/skills/tix"
  must_absent "$HOME/.claude/skills"
  must_absent "$(MANIFEST)"
  [ -z "$(baks)" ] || fail "undo left backups: $(baks)"
}

t_undo_on_pristine_home_leaves_no_trace() {
  new_home
  apply; undo
  [ -z "$(find "$HOME" -mindepth 1)" ] || fail "traces left after undo on an empty HOME: $(find "$HOME" -mindepth 1 | head -5)"
}

t_existing_tk_permission_survives_undo() {
  new_home
  jq '.permissions.allow += ["Bash(tix *)"]' "$FIX/claude-settings.json" >"$BASE/s.json"
  seed_claude claude-settings.json
  cp "$BASE/s.json" "$HOME/.claude/settings.json"; cp "$BASE/s.json" "$ORIG/settings.json"
  apply
  jq_ok "$HOME/.claude/settings.json" '[.permissions.allow[] | select(. == "Bash(tix *)")] | length == 1' 'Bash(tix *) is not duplicated'
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" '.permissions' 'allow changed by apply although the key was already there'
  undo
  jq_ok "$HOME/.claude/settings.json" '.permissions.allow | index("Bash(tix *)") != null' 'undo removed a foreign Bash(tix *)'
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" '.permissions' 'permissions after undo'
}

t_existing_own_hook_upgraded_and_restored() {
  new_home; seed_claude claude-settings-ownhook.json
  apply
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tix ctx --hook")] | length == 1' 'own hook tk ctx did not become tix ctx --hook'
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tk ctx")] | length == 0' 'old hook tk ctx left over'
  run apply --agent all --now "$NOW2"
  [ "$RC" -eq 0 ] || fail "repeated apply: code $RC"
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select((.command? // "") | startswith("tix ctx"))] | length == 1' 'repeated apply duplicated the hook'
  undo
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" . 'own hook tk ctx must return as it was'
}

# A hook set by the earlier version (connector with `tk ctx`) is updated in place and removed whole.
t_old_installed_hook_upgraded() {
  new_home
  local old=$BASE/oldroot cur=$CONNECT
  rm -rf "$old"; mkdir -p "$old/scripts"
  cp "$CONNECT" "$old/scripts/connect.sh"; cp -R "$CONN" "$old/connectors"
  sed -i.bak 's/tix ctx --hook/tk ctx/' "$old/connectors/claude/hooks.json"; rm -f "$old/connectors/claude/hooks.json.bak"
  CONNECT=$old/scripts/connect.sh apply
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tk ctx")] | length == 1' 'old connector did not set tk ctx'
  CONNECT=$cur
  apply
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tix ctx --hook")] | length == 1' 'hook not updated to tix ctx --hook'
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tk ctx")] | length == 0' 'old hook tk ctx left over'
  jq_ok "$HOME/.claude/settings.json" '.hooks.SessionStart | length == 1' 'SessionStart group duplicated'
  jq_ok "$HOME/.codex/hooks.json" '[.. | objects | select(.command? == "tix ctx")] | length == 1' 'Codex: hook must stay tix ctx'
  undo
  [ -z "$(find "$HOME" -mindepth 1)" ] || fail "traces left after undo: $(find "$HOME" -mindepth 1 | head -5)"
}

# Install of the earlier version (name tk, manifest in .tracker): apply moves everything to tix, undo cleans up completely.
t_legacy_tk_install_migrated() {
  TOOLS_OVERRIDE=$BASE/tools-real
  new_home
  cp -R "$FIX/legacy-home/." "$HOME/"
  sed -i.bak "s#@HOME@#$HOME#g" "$HOME/tasks/.tracker/connect-state.json"; rm -f "$HOME/tasks/.tracker/connect-state.json.bak"
  printf '.tracker/lock\n*.swp\n' >"$HOME/tasks/.gitignore"
  run plan
  [ "$RC" -eq 0 ] || fail "plan on the old install: code $RC"
  must_exist "$HOME/tasks/.tracker/connect-state.json"
  must_absent "$HOME/tasks/.tix"
  apply
  must_exist "$(MANIFEST)"
  must_absent "$HOME/tasks/.tracker"
  grep -q '^\.tix/lock$' "$HOME/tasks/.gitignore" || fail ".gitignore of the store not moved to .tix: $(cat "$HOME/tasks/.gitignore")"
  grep -q 'tracker' "$HOME/tasks/.gitignore" && fail ".gitignore of the store keeps .tracker"
  local f
  for f in "$HOME/.claude/settings.json" "$HOME/.codex/hooks.json"; do
    jq_ok "$f" '[.. | objects | select((.command? // "") | test("^tk "))] | length == 0' "hook tk left in $f"
    jq_ok "$f" '[.. | objects | select((.command? // "") | test("^tix "))] | length == 4' "$f does not hold four tix hooks"
  done
  jq_ok "$HOME/.claude/settings.json" '.permissions.allow | index("Bash(tix *)") != null and index("Bash(tk *)") == null' 'allow not moved to Bash(tix *)'
  must_exist "$HOME/.claude/skills/tix/SKILL.md"; must_absent "$HOME/.claude/skills/tk"
  must_exist "$HOME/.agents/skills/tix/SKILL.md"; must_absent "$HOME/.agents/skills/tk"
  must_exist "$HOME/.codex/rules/tix.rules"; must_absent "$HOME/.codex/rules/tk.rules"
  jq_ok "$(MANIFEST)" '[.entries[].key | select(test("/tk(\\.rules)?\\|?"))] | length == 0' 'tk entries left in the manifest'
  run plan
  [ -z "$OUT" ] || fail "repeated plan after migration is not empty: $OUT"
  undo
  [ -z "$(find "$HOME" -mindepth 1 \( -type f -o -type l \) ! -name .gitignore)" ] || fail "files left after undo: $(find "$HOME" -type f | head -5)"
  [ -z "$(find "$HOME" -mindepth 1 -type d -empty)" ] || fail "empty directories left after undo: $(find "$HOME" -type d -empty | head -5)"
}

# Common order: the tix hook in a new session has already moved the store to .tix, apply follows.
t_legacy_base_migrated_by_tix_first() {
  TOOLS_OVERRIDE=$BASE/tools-real
  new_home
  cp -R "$FIX/legacy-home/." "$HOME/"
  sed -i.bak "s#@HOME@#$HOME#g" "$HOME/tasks/.tracker/connect-state.json"; rm -f "$HOME/tasks/.tracker/connect-state.json.bak"
  printf '.tracker/lock\n*.swp\n' >"$HOME/tasks/.gitignore"
  env -i HOME="$HOME" TIX_HOME="$HOME/tasks" "$REAL_TIX" list >/dev/null || fail "tix list on the old store"
  must_exist "$(MANIFEST)"; must_absent "$HOME/tasks/.tracker"
  apply
  grep -q 'tracker' "$(MANIFEST)" && fail "manifest keeps .tracker paths"
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select((.command? // "") | test("^tix "))] | length == 4' 'hooks not moved to tix'
  undo
  [ -z "$(find "$HOME" -mindepth 1 \( -type f -o -type l \) ! -name .gitignore)" ] || fail "files left after undo: $(find "$HOME" -type f | head -5)"
}

# undo removes only the recorded group: the user's own group with the same command stays.
t_undo_keeps_foreign_group_with_same_command() {
  new_home
  apply
  local own='{"matcher":"Write","hooks":[{"type":"command","command":"tix inbox --hook","timeout":30}]}'
  jq --argjson g "$own" '.hooks.Stop += [$g]' "$HOME/.claude/settings.json" >"$H.tmp" && mv "$H.tmp" "$HOME/.claude/settings.json"
  undo
  jq_ok "$HOME/.claude/settings.json" ".hooks.Stop == [$own]" 'undo removed a foreign group with tix inbox --hook'
}

t_modified_own_files_skipped_without_force() {
  new_home
  apply
  local f
  echo '# mine' >>"$HOME/.codex/rules/tix.rules"
  echo 'mine' >>"$HOME/.claude/skills/tix/SKILL.md"
  echo 'mine' >>"$HOME/.agents/skills/tix/SKILL.md"
  local before
  before=$(snap)
  apply "$NOW2"
  [ "$before" = "$(snap)" ] || fail "apply without --force overwrote changed files"
  for f in "$HOME/.codex/rules/tix.rules" "$HOME/.claude/skills/tix" "$HOME/.agents/skills/tix"; do
    all_out | grep -iF 'warn' | grep -qF "$f" || fail "no warn with path $f: $(all_out | head -6)"
  done
  [ -z "$(baks)" ] || fail "backups after apply: $(baks)"
  run apply --agent all --now "$NOW2" --force
  [ "$RC" -eq 0 ] || fail "apply --force: code $RC: $(all_out | head -5)"
  same_bytes "$HOME/.codex/rules/tix.rules" "$CONN/codex/tix.rules" 'tix.rules after --force'
  diff -r "$SKILL_SRC" "$HOME/.claude/skills/tix" >/dev/null || fail "Claude skill after --force differs from the source"
  diff -r "$SKILL_SRC" "$HOME/.agents/skills/tix" >/dev/null || fail "Codex skill after --force differs from the source"
}

t_undo_removes_only_manifest_files() {
  new_home
  apply
  echo mine >"$HOME/.claude/skills/tix/extra.txt"
  mkdir -p "$HOME/.agents/skills/tix/sub"
  echo mine >"$HOME/.agents/skills/tix/sub/note.txt"
  run undo --agent all --now "$NOW2"
  [ "$RC" -eq 0 ] || fail "undo: code $RC: $(all_out | head -5)"
  must_exist "$HOME/.claude/skills/tix/extra.txt"
  must_exist "$HOME/.agents/skills/tix/sub/note.txt"
  must_absent "$HOME/.claude/skills/tix/SKILL.md"
  must_absent "$HOME/.agents/skills/tix/SKILL.md"
  must_absent "$HOME/.codex/rules/tix.rules"
  all_out | grep -qF "$HOME/.claude/skills/tix" || fail "undo did not name the kept directory: $(all_out | head -6)"
}

t_undo_skips_paths_outside_whitelist() {
  new_home
  mkdir -p "$HOME/tasks/.tix" "$HOME/victimdir"
  echo keep >"$HOME/victim.txt"
  echo keep >"$HOME/victimdir/a.txt"
  echo '{"keep":1}' >"$HOME/other.json"
  jq -n --arg h "$HOME" '{version:1, dirs_created:[($h + "/victimdir")], entries:[
    {key:"k1", agent:"codex", kind:"file", path:($h + "/victim.txt"), dirs_created:[]},
    {key:"k2", agent:"claude", kind:"dir", path:($h + "/victimdir"), files:["a.txt"], dirs_created:[]},
    {key:"k3", agent:"claude", kind:"json", op:"arr", file:($h + "/other.json"), path:["keep"], value:1, created:[], file_created:true, dirs_created:[]},
    {key:"k4", agent:"claude", kind:"file", path:"/etc/hosts", dirs_created:["/etc"]}]}' >"$(MANIFEST)"
  run undo --agent all --now "$NOW2"
  [ "$RC" -ne 0 ] || fail "undo with foreign paths returned 0"
  must_exist "$HOME/victim.txt"
  must_exist "$HOME/victimdir/a.txt"
  jq_ok "$HOME/other.json" '.keep == 1' 'other.json changed'
  [ -e /etc/hosts ] || fail "/etc/hosts missing?"
  local p
  for p in "$HOME/victim.txt" "$HOME/victimdir" "$HOME/other.json" /etc/hosts; do
    all_out | grep -iF 'warn' | grep -qF "$p" || fail "no warn with path $p: $(all_out | head -8)"
  done
  must_exist "$(MANIFEST)"
}

t_undo_without_manifest_says_so() {
  new_home
  undo
  [ "$OUT" = "nothing to undo: no manifest at $HOME/tasks/.tix/connect-state.json" ] || fail "output: '$OUT'"
  RUN_ENV=("TIX_HOME=$H/alt")
  undo
  RUN_ENV=()
  [ "$OUT" = "nothing to undo: no manifest at $H/alt/.tix/connect-state.json" ] || fail "output with TIX_HOME: '$OUT'"
}

t_symlinked_configs_stay_symlinks() {
  new_home; seed_codex
  mkdir -p "$HOME/dotfiles" "$HOME/.claude"
  local link rel name want
  cp "$FIX/claude-settings.json" "$HOME/dotfiles/settings.json"; chmod 600 "$HOME/dotfiles/settings.json"
  ln -s "$HOME/dotfiles/settings.json" "$HOME/.claude/settings.json"
  cp "$FIX/claude-settings.json" "$ORIG/settings.json"
  mv "$HOME/.codex/hooks.json" "$HOME/dotfiles/hooks.json"; chmod 640 "$HOME/dotfiles/hooks.json"
  ln -s "$HOME/dotfiles/hooks.json" "$HOME/.codex/hooks.json"
  apply
  for link in .claude/settings.json:settings.json:600 .codex/hooks.json:hooks.json:640; do
    IFS=: read -r rel name want <<<"$link"
    [ -L "$HOME/$rel" ] || fail "$rel stopped being a symlink"
    [ "$(readlink "$HOME/$rel")" = "$HOME/dotfiles/$name" ] || fail "$rel points to $(readlink "$HOME/$rel")"
    [ "$(mode "$HOME/dotfiles/$name")" = "$want" ] || fail "target mode of $name: $(mode "$HOME/dotfiles/$name"), expected $want"
  done
  jq_ok "$HOME/dotfiles/settings.json" '.permissions.allow | index("Bash(tix *)") != null' 'the entry went to the symlink target'
  jq_ok "$HOME/dotfiles/settings.json" '[.. | objects | select(.command? == "tix ctx --hook")] | length == 1' 'the Claude hook went to the symlink target'
  jq_ok "$HOME/dotfiles/hooks.json" '[.. | objects | select(.command? == "tix ctx")] | length == 1' 'the Codex hook went to the symlink target'
  undo
  for link in .claude/settings.json:settings.json .codex/hooks.json:hooks.json; do
    IFS=: read -r rel name <<<"$link"
    [ -L "$HOME/$rel" ] || fail "$rel stopped being a symlink after undo"
  done
  jq_eq "$HOME/dotfiles/settings.json" "$ORIG/settings.json" . 'settings.json target after undo'
  jq_eq "$HOME/dotfiles/hooks.json" "$ORIG/hooks.json" . 'hooks.json target after undo'
}

t_symlinked_skill_dir_target_untouched() {
  new_home
  # .agents/skills is a link to a foreign directory: apply writes into the target and keeps the link.
  mkdir -p "$HOME/dotfiles/skills"
  mkdir -p "$HOME/.agents"
  ln -s "$HOME/dotfiles/skills" "$HOME/.agents/skills"
  apply
  [ -L "$HOME/.agents/skills" ] || fail ".agents/skills stopped being a link"
  must_exist "$HOME/dotfiles/skills/tix/SKILL.md"
  undo
  [ -L "$HOME/.agents/skills" ] || fail ".agents/skills stopped being a link after undo"
  must_absent "$HOME/dotfiles/skills/tix"
}

t_permissions_600_preserved() {
  new_home; seed_all
  chmod 600 "$HOME/.claude/settings.json"
  chmod 640 "$HOME/.codex/hooks.json"
  apply
  [ -f "$HOME/.claude/settings.json" ] && [ ! -L "$HOME/.claude/settings.json" ] || fail "settings.json is not a regular file"
  [ "$(mode "$HOME/.claude/settings.json")" = 600 ] || fail "settings.json after apply: $(mode "$HOME/.claude/settings.json")"
  [ "$(mode "$HOME/.codex/hooks.json")" = 640 ] || fail "hooks.json after apply: $(mode "$HOME/.codex/hooks.json")"
  undo
  [ "$(mode "$HOME/.claude/settings.json")" = 600 ] || fail "settings.json after undo: $(mode "$HOME/.claude/settings.json")"
  [ "$(mode "$HOME/.codex/hooks.json")" = 640 ] || fail "hooks.json after undo: $(mode "$HOME/.codex/hooks.json")"
}

t_foreign_content_intact() {
  new_home; seed_all
  echo 'user backup' >"$HOME/.claude/settings.json.bak"
  apply
  local s=$HOME/.claude/settings.json
  jq_eq "$s" "$ORIG/settings.json" "del(.permissions.allow) | $DROP_OURS" 'Claude: foreign keys (except allow and our group)'
  [ "$(jq -c '.permissions.allow[0:3]' "$s")" = "$(jq -c '.permissions.allow' "$ORIG/settings.json")" ] || fail "order of foreign allow entries broken: $(jq -c .permissions.allow "$s")"
  jq_ok "$s" '.hooks.SessionStart[0].hooks[0].command == "echo foreign-claude-start"' 'Claude: foreign group stayed first'
  jq_eq "$HOME/.codex/hooks.json" "$ORIG/hooks.json" "$DROP_OURS" 'Codex: foreign SessionStart and Stop groups'
  jq_ok "$HOME/.codex/hooks.json" '.hooks.SessionStart[0].hooks[0].command == "echo foreign-codex-start"' 'Codex: foreign group stayed first'
  same_bytes "$HOME/.codex/rules/default.rules" "$ORIG/default.rules" 'default.rules changed'
  same_bytes "$HOME/.codex/config.toml" "$ORIG/config.toml" 'config.toml changed'
  must_absent "$HOME/.claude/CLAUDE.md"
  must_absent "$HOME/.codex/AGENTS.md"
  undo
  [ "$(cat "$HOME/.claude/settings.json.bak" 2>/dev/null)" = 'user backup' ] || fail "undo deleted a foreign settings.json.bak"
}

t_agent_filter_touches_only_that_agent() {
  new_home
  run apply --agent claude --now "$NOW1"
  [ "$RC" -eq 0 ] || fail "apply --agent claude: code $RC: $(all_out | head -5)"
  must_exist "$HOME/.claude/settings.json"
  must_absent "$HOME/.codex"
  must_absent "$HOME/.agents"
  run apply --agent codex --now "$NOW1"
  [ "$RC" -eq 0 ] || fail "apply --agent codex: code $RC"
  run undo --agent claude --now "$NOW2"
  [ "$RC" -eq 1 ] || fail "undo of one agent while the other is in the manifest: expected code 1, got $RC"
  must_absent "$HOME/.claude"
  must_exist "$HOME/.codex/hooks.json"
  must_exist "$(MANIFEST)"
  undo
  [ -z "$(find "$HOME" -mindepth 1)" ] || fail "traces after undo: $(find "$HOME" -mindepth 1 | head -5)"
}

t_missing_tools_err_and_files_intact() {
  local missing
  for missing in jq tix; do
    new_home; seed_all
    local before after
    before=$(snap)
    TOOLS_OVERRIDE=$BASE/tools-no-$missing run apply --agent all --now "$NOW1"
    unset TOOLS_OVERRIDE
    [ "$RC" -ne 0 ] || fail "apply returned 0 without $missing"
    all_out | grep -qi 'err' || fail "no err message without $missing: $(all_out | head -3)"
    all_out | grep -qF "$missing" || fail "the message does not name the tool without $missing: $(all_out | head -3)"
    after=$(snap)
    [ "$before" = "$after" ] || fail "apply changed files without $missing"$'\n'"$(diff <(echo "$before") <(echo "$after") | head -6)"
  done
}

t_broken_or_unexpected_json_err_and_file_intact() {
  local case_ fixture target
  for case_ in \
    "claude:.claude/settings.json:claude-settings-broken.json" \
    "claude:.claude/settings.json:claude-settings-badshape.json" \
    "claude:.claude/settings.json:claude-settings-hooks-badshape.json" \
    "codex:.codex/hooks.json:codex-hooks-badshape.json"; do
    IFS=: read -r _ target fixture <<<"$case_"
    new_home
    mkdir -p "$(dirname "$HOME/$target")"
    cp "$FIX/$fixture" "$HOME/$target"; chmod 600 "$HOME/$target"
    cp "$FIX/$fixture" "$ORIG/orig"
    run apply --agent all --now "$NOW1"
    [ "$RC" -ne 0 ] || fail "$fixture: apply returned 0"
    all_out | grep -qi 'err' || fail "$fixture: no err: $(all_out | head -3)"
    all_out | grep -qF "$target" || fail "$fixture: the message does not name the path $target: $(all_out | head -3)"
    same_bytes "$HOME/$target" "$ORIG/orig" "$fixture: file changed"
    [ -z "$(baks)" ] || fail "$fixture: extra backup: $(baks)"
    [ -z "$(find "$HOME" -type f -not -path "$HOME/$target")" ] || fail "$fixture: other files written: $(find "$HOME" -type f -not -path "$HOME/$target" | head -3)"
  done
}

t_hook_reads_stdin_and_output_is_short() {
  new_home
  apply
  local cmds=() c name out rc w f
  for f in claude:.claude/settings.json codex:.codex/hooks.json; do
    name=${f%%:*}
    for w in $MATCHER_WORDS; do
      jq_ok "$HOME/${f#*:}" "[.hooks.SessionStart[].matcher // \"\" | split(\"|\") | index(\"$w\")] | any(. != null)" "$name: SessionStart matcher lacks $w"
    done
    while IFS= read -r c; do cmds+=("$name|$c"); done < <(jq -r '.hooks.SessionStart[] | select(any(.hooks[]?; (.command | startswith("tix ctx")))) | .hooks[].command' "$HOME/${f#*:}")
  done
  [ "${#cmds[@]}" -eq 2 ] || fail "tix ctx hooks found: ${#cmds[@]}, need one each in Claude and Codex"
  for c in "${cmds[@]}"; do
    name=${c%%|*}; c=${c#*|}
    out=$(env -i HOME="$HOME" PATH="$TOOLS" LANG=C "$TOOLS/sh" -c "$c" <"$FIX/hook-stdin.json" 2>"$H.err"); rc=$?
    [ "$rc" -eq 0 ] || fail "$name: hook '$c' returned $rc: $(head -3 "$H.err")"
    [ "${#out}" -lt 2000 ] || fail "$name: hook output is ${#out} characters, must be under 2000"
    [ -n "$out" ] || fail "$name: the hook printed nothing into the context"
  done
}

# Installed inbox hooks run as they are: exit code 0 on hook input, the stub prints nothing.
t_inbox_hook_commands_run() {
  new_home
  apply
  local f ev c n=0 rc
  for f in .claude/settings.json .codex/hooks.json; do
    for ev in PostToolUse UserPromptSubmit Stop; do
      while IFS= read -r c; do
        env -i HOME="$HOME" PATH="$TOOLS" LANG=C "$TOOLS/sh" -c "$c" <"$FIX/hook-stdin.json" >/dev/null 2>"$H.err"; rc=$?
        [ "$rc" -eq 0 ] || fail "$f $ev: '$c' returned $rc: $(head -3 "$H.err")"
        n=$((n + 1))
      done < <(jq -r ".hooks.${ev}[] | select(any(.hooks[]?; .command | startswith(\"tix inbox\"))) | .hooks[].command" "$HOME/$f")
    done
  done
  [ "$n" -eq 6 ] || fail "inbox hooks run: $n, need 6 (three events for two agents)"
}

t_skill_copies_identical_and_small() {
  new_home
  apply
  local a=$SKILL_SRC/SKILL.md d
  [ -f "$a" ] || fail "missing $a"
  for d in "$HOME/.claude/skills/tix" "$HOME/.agents/skills/tix"; do
    diff -r "$SKILL_SRC" "$d" >/dev/null || fail "skill copy $d differs from the source"
  done
  [ "$(wc -c <"$a")" -le 1536 ] || fail "SKILL.md is over 1.5 KB: $(wc -c <"$a")"
  head -8 "$a" | grep -q '^name: tix$' || fail "SKILL.md has no name: tix"
  head -8 "$a" | grep -q '^description:.*Use when' || fail "description has no 'Use when'"
}

# ---------- run ----------

TESTS=(
  t_guard_rejects_real_home
  t_validate_connectors
  t_plan_writes_nothing_and_lists_targets
  t_apply_clean_writes_exact_paths
  t_tix_home_sets_manifest_place
  t_apply_twice_no_changes
  t_backups_live_only_during_apply
  t_failed_apply_keeps_and_names_backups
  t_undo_restores_original_json
  t_undo_on_pristine_home_leaves_no_trace
  t_existing_tk_permission_survives_undo
  t_legacy_tk_install_migrated
  t_legacy_base_migrated_by_tix_first
  t_undo_keeps_foreign_group_with_same_command
  t_existing_own_hook_upgraded_and_restored
  t_old_installed_hook_upgraded
  t_modified_own_files_skipped_without_force
  t_undo_removes_only_manifest_files
  t_undo_without_manifest_says_so
  t_undo_skips_paths_outside_whitelist
  t_symlinked_configs_stay_symlinks
  t_symlinked_skill_dir_target_untouched
  t_permissions_600_preserved
  t_foreign_content_intact
  t_agent_filter_touches_only_that_agent
  t_missing_tools_err_and_files_intact
  t_broken_or_unexpected_json_err_and_file_intact
  t_hook_reads_stdin_and_output_is_short
  t_inbox_hook_commands_run
  t_skill_copies_identical_and_small
)
[ "$#" -gt 0 ] && TESTS=("$@")

pass=0 failed=0 names=()
for t in "${TESTS[@]}"; do
  if ( "$t" ) 2>"$BASE/test.err"; then
    echo "ok    $t"; pass=$((pass + 1))
  else
    echo "FAIL  $t"; sed 's/^/      /' "$BASE/test.err" | head -14
    failed=$((failed + 1)); names+=("$t")
  fi
done

REAL_AFTER=$(real_fingerprint)
if [ "$REAL_BEFORE" != "$REAL_AFTER" ]; then
  echo "FAIL  the real HOME changed during the run:" >&2
  diff <(echo "$REAL_BEFORE") <(echo "$REAL_AFTER") >&2
  exit 3
fi
echo "total: $pass ok, $failed FAIL; the real HOME is untouched"
[ "$failed" -eq 0 ]
