#!/usr/bin/env bash
# Тесты scripts/connect.sh (зона T плана docs/plans/z3-connectors.md).
#
# Каждый тест идёт на своём HOME из mktemp. Настоящий HOME не читается и не пишется:
# перед запуском connect.sh тест проверяет подмену HOME, в конце сверяет отпечаток
# (stat, без чтения содержимого) файлов настоящего HOME с отпечатком до прогона.
# Агенты и модели не запускаются; claude в PATH тестов нет: скрипту он не нужен.
#
# Запуск: scripts/test-connect.sh [имя_теста ...]
# Окружение: TK_BIN=<путь к настоящему tk> - прогнать хук и на нём (иначе заглушка);
#            TK_TEST_KEEP=1 - не удалять временный каталог.
set -uo pipefail

SELF_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO=$(cd "$SELF_DIR/.." && pwd)
CONNECT=$REPO/scripts/connect.sh
FIX=$REPO/connectors/testdata
CONN=$REPO/connectors
SKILL_SRC=$CONN/agents/skills/tk
NOW1=2026-10-20T10:00:00Z
NOW2=2026-10-20T11:00:00Z
MATCHER_WORDS="startup resume clear compact"

# Настоящий HOME учётной записи, а не переменная окружения.
REAL_HOME=$(eval "echo ~$(id -un)")
[ -n "$REAL_HOME" ] && [ "$REAL_HOME" != "~$(id -un)" ] || { echo "не удалось определить настоящий HOME" >&2; exit 2; }

BASE=$(mktemp -d "${TMPDIR:-/tmp}/tk-connect-test.XXXXXX") || exit 2
BASE=$(cd -P "$BASE" && pwd)
HOMES=$BASE/homes
TOOLS=$BASE/tools
mkdir -p "$HOMES" "$TOOLS" "$BASE/tmp"

cleanup() {
  [ "${TK_TEST_KEEP:-}" = 1 ] && { echo "временный каталог оставлен: $BASE" >&2; return; }
  case "$BASE" in */tk-connect-test.??????) rm -rf -- "$BASE" ;; esac
}
trap cleanup EXIT

# ---------- отпечаток настоящего HOME ----------

if stat --version >/dev/null 2>&1; then STAT_SIG=(stat -L -c '%Y %s %i %a'); STAT_MODE=(stat -L -c %a)
else STAT_SIG=(stat -L -f '%m %z %i %Lp'); STAT_MODE=(stat -L -f %Lp); fi

mode() { "${STAT_MODE[@]}" "$1"; }
sha() { if command -v shasum >/dev/null 2>&1; then shasum -a 256 <"$1" | cut -d' ' -f1; else sha256sum <"$1" | cut -d' ' -f1; fi; }

REAL_WATCH=(
  .claude/settings.json .claude/skills/tk .codex/hooks.json .codex/rules/tk.rules
  .codex/rules/default.rules .codex/config.toml .agents/skills/tk tasks/.tracker/connect-state.json
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
    -maxdepth 2 -name '*.tk-bak-*' 2>/dev/null | LC_ALL=C sort
}

REAL_BEFORE=$(real_fingerprint)

# ---------- окружение: каталог утилит и заглушка tk ----------

# PATH тестов - только этот каталог: так видно, что скрипт требует jq и tk,
# и можно убрать любую из утилит.
BASE_TOOLS=(bash env sh cat mv cp rm mkdir date dirname basename realpath stat chmod ln mktemp sed awk
  grep sort cut tr head tail readlink cmp diff ls printf touch find shasum sha256sum tee wc uname id
  xargs uniq tput rmdir mkfifo sleep expr test true false)

link_tools() { # каталог, исключение
  local dir=$1 skip=${2:-} t p
  mkdir -p "$dir"
  for t in "${BASE_TOOLS[@]}" jq; do
    [ "$t" = "$skip" ] && continue
    p=$(command -v "$t" 2>/dev/null) && [ -x "$p" ] && ln -sf "$p" "$dir/$t"
  done
  [ "$skip" = tk ] && return 0
  if [ -n "${TK_BIN:-}" ]; then ln -sf "$TK_BIN" "$dir/tk"; else cp "$BASE/tk-stub" "$dir/tk"; fi
}

# Заглушка tk: знает только `ctx` и `ctx --hook`, любая другая форма вызова - код 64.
cat >"$BASE/tk-stub" <<'EOF'
#!/bin/sh
if { [ "$#" -eq 1 ] && [ "$1" = ctx ]; } || { [ "$#" -eq 2 ] && [ "$1" = ctx ] && [ "$2" = --hook ]; }; then
  cat >/dev/null 2>&1 || true
  echo "tk ctx: stub, 0 tasks"
  exit 0
fi
echo "tk stub: unexpected args: $*" >&2
exit 64
EOF
chmod +x "$BASE/tk-stub"
link_tools "$TOOLS"
for x in jq tk; do link_tools "$BASE/tools-no-$x" "$x"; done

# ---------- проверки и утилиты ----------

fail() { echo "    FAIL: $*" >&2; exit 1; }

assert_fake_home() { # [путь]; по умолчанию проверяется HOME
  local h=${1-${HOME:-}}
  [ -n "$h" ] && [ "$h" != "$REAL_HOME" ] && [ "$h" != / ] || return 1
  case "$h" in "$HOMES"/*) return 0 ;; *) return 1 ;; esac
}

new_home() { # создаёт и экспортирует HOME теста; H - то же значение
  H=$(mktemp -d "$HOMES/h.XXXXXX") || fail "mktemp"
  H=$(cd -P "$H" && pwd)
  export HOME=$H
  ORIG=$H.orig
  mkdir -p "$ORIG"
  assert_fake_home || fail "HOME не подменён: $HOME"
}

RC=0 OUT="" ERR="" RUN_ENV=()
run() { # run <ключевые слова connect.sh>; результат в RC, OUT, ERR
  assert_fake_home || fail "HOME не подменён перед запуском connect.sh: $HOME"
  [ -f "$CONNECT" ] || fail "scripts/connect.sh не существует"
  local tools=${TOOLS_OVERRIDE:-$TOOLS}
  OUT=$(env -i HOME="$HOME" PATH="$tools" TERM=dumb LANG=C TMPDIR="$BASE/tmp" ${RUN_ENV[@]+"${RUN_ENV[@]}"} \
    "$tools/bash" "$CONNECT" "$@" 2>"$H.err" </dev/null)
  RC=$?
  ERR=$(cat "$H.err")
}
all_out() { printf '%s\n%s' "$OUT" "$ERR"; }
apply() { run apply --agent all --now "${1:-$NOW1}"; [ "$RC" -eq 0 ] || fail "apply: код $RC; $(all_out | head -5)"; }
undo() { run undo --agent all --now "${1:-$NOW2}"; [ "$RC" -eq 0 ] || fail "undo: код $RC; $(all_out | head -5)"; }

jq_eq() { # jq_eq <файл-a> <файл-b> <фильтр> <метка>: равенство по jq -S
  local a b
  a=$(jq -S "$3" "$1" 2>&1) || fail "$4: не JSON: $1"
  b=$(jq -S "$3" "$2" 2>&1) || fail "$4: не JSON: $2"
  [ "$a" = "$b" ] || fail "$4: JSON различается ($1 и $2)"$'\n'"$(diff <(echo "$a") <(echo "$b") | head -12)"
}
jq_ok() { jq -e "$2" "$1" >/dev/null 2>&1 || fail "${3:-$2}: не выполнено для $1"; }
same_bytes() { cmp -s "$1" "$2" || fail "${3:-файл изменён}: $1"; }
must_exist() { [ -e "$1" ] || [ -L "$1" ] || fail "нет $1"; }
must_absent() { [ ! -e "$1" ] && [ ! -L "$1" ] || fail "остался $1"; }
baks() { find "$HOME" -name '*.tk-bak-*' | LC_ALL=C sort; }
MANIFEST() { echo "$HOME/tasks/.tracker/connect-state.json"; }
files_in_home() { (cd "$HOME" && find . -mindepth 1 \( -type f -o -type l \) | LC_ALL=C sort); }

snap() { # снимок файлов, которые трогает connect.sh: путь, ссылка, права, хэш
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

# ---------- тесты ----------

t_guard_rejects_real_home() {
  assert_fake_home "$REAL_HOME" && fail "assert_fake_home принял настоящий HOME"
  assert_fake_home /tmp && fail "assert_fake_home принял чужой каталог"
  assert_fake_home "" && fail "assert_fake_home принял пустой HOME"
  new_home; assert_fake_home || fail "подменённый HOME отвергнут"
}

t_validate_connectors() {
  new_home
  local f
  while IFS= read -r f; do jq empty "$f" 2>/dev/null || fail "не JSON: $f"; done < <(find "$CONN" -name '*.json' -not -path '*/testdata/claude-settings-broken.json')
  [ -f "$SKILL_SRC/SKILL.md" ] || fail "нет $SKILL_SRC/SKILL.md"
  [ -f "$CONN/claude/hooks.json" ] || fail "нет connectors/claude/hooks.json"
  [ ! -e "$CONN/claude/plugins" ] && [ ! -e "$CONN/claude/.claude-plugin" ] || fail "остался плагин Claude"
  [ ! -e "$CONN/opencode" ] || fail "остался connectors/opencode"
  [ "$(find "$CONN" -name SKILL.md -not -path '*/testdata/*' | wc -l | tr -d ' ')" = 1 ] || fail "копий скилла не одна"
}

t_plan_writes_nothing_and_lists_targets() {
  new_home
  local before after
  before=$(snap)
  run plan --agent all
  [ "$RC" -eq 0 ] || fail "plan: код $RC: $(all_out | head -5)"
  run plan
  [ "$RC" -eq 0 ] || fail "plan без аргументов (по умолчанию): код $RC"
  after=$(snap)
  [ "$before" = "$after" ] || fail "plan изменил файлы HOME"
  [ -z "$(files_in_home)" ] || fail "plan создал файлы: $(files_in_home)"
  run plan --agent all
  all_out | grep -qF 'Bash(tk *)' || fail "plan не называет ключ Bash(tk *): $(all_out | head -5)"
  all_out | grep -qF 'hooks.json' || fail "plan не называет .codex/hooks.json"
}

t_apply_clean_writes_exact_paths() {
  new_home
  apply
  local want
  want=$(printf '%s\n' ./.agents/skills/tk/SKILL.md ./.claude/settings.json ./.claude/skills/tk/SKILL.md \
    ./.codex/hooks.json ./.codex/rules/tk.rules ./tasks/.tracker/connect-state.json | LC_ALL=C sort)
  [ "$(files_in_home)" = "$want" ] || fail "набор файлов HOME другой:"$'\n'"$(diff <(echo "$want") <(files_in_home) | head -8)"
  [ -z "$(baks)" ] || fail "после apply остались бэкапы: $(baks)"

  local s=$HOME/.claude/settings.json
  jq_ok "$s" '.permissions.allow | index("Bash(tk *)") != null' 'Claude: Bash(tk *) в allow'
  jq_ok "$s" '[.hooks.SessionStart[] | select(.matcher == "startup|resume|clear|compact") | .hooks[] | select(.command == "tk ctx --hook")] | length == 1' 'Claude: группа SessionStart с tk ctx --hook'
  jq_ok "$s" 'keys | sort == ["hooks","permissions"]' 'Claude: лишние ключи в settings.json'
  local h=$HOME/.codex/hooks.json
  jq_ok "$h" '[.hooks.SessionStart[] | select(.matcher == "startup|resume|clear|compact") | .hooks[] | select(.command == "tk ctx")] | length == 1' 'Codex: группа SessionStart с tk ctx'
  grep -qF 'prefix_rule(pattern=["tk"], decision="allow")' "$HOME/.codex/rules/tk.rules" || fail "Codex: tk.rules без prefix_rule для tk"
  jq_ok "$(MANIFEST)" '.version == 1 and (.entries | length) > 0' 'манифест'

  run plan --agent all
  [ "$RC" -eq 0 ] || fail "повторный plan: код $RC"
  [ -z "$(printf '%s' "$OUT" | tr -d '[:space:]')" ] || fail "повторный plan не пуст: $(echo "$OUT" | head -5)"
}

t_tracker_home_sets_manifest_place() {
  new_home
  RUN_ENV=("TRACKER_HOME=$H/alt")
  apply
  jq_ok "$H/alt/.tracker/connect-state.json" '.version == 1' 'манифест в TRACKER_HOME'
  must_absent "$HOME/tasks"
  undo
  RUN_ENV=()
  must_absent "$H/alt"
  [ -z "$(files_in_home)" ] || fail "после undo остались файлы: $(files_in_home)"
}

t_apply_twice_no_changes() {
  new_home; seed_all
  apply "$NOW1"
  local s1 s2
  s1=$(snap)
  apply "$NOW2"
  s2=$(snap)
  [ "$s1" = "$s2" ] || fail "второй apply изменил файлы:"$'\n'"$(diff <(echo "$s1") <(echo "$s2") | head -10)"
  [ -z "$(baks)" ] || fail "бэкапы остались: $(baks)"
}

t_backups_live_only_during_apply() {
  new_home; seed_all
  chmod 640 "$HOME/.codex/hooks.json"
  apply
  [ -z "$(baks)" ] || fail "успешный apply оставил бэкапы: $(baks)"
}

t_failed_apply_keeps_and_names_backups() {
  new_home; seed_all
  chmod 555 "$HOME/.codex/rules"
  run apply --agent all --now "$NOW1"
  chmod 755 "$HOME/.codex/rules"
  [ "$RC" -ne 0 ] || fail "apply вернул 0, хотя запись tk.rules невозможна"
  local f
  for f in .claude/settings.json .codex/hooks.json; do
    set -- "$HOME/$f".tk-bak-*
    [ -e "$1" ] && [ "$#" -eq 1 ] || fail "ожидался один бэкап рядом с $f, есть: $*"
    same_bytes "$1" "$ORIG/$(basename "$f")" "бэкап не равен исходному файлу"
    [ "$(mode "$1")" = "$(mode "$HOME/$f")" ] || fail "права бэкапа $(mode "$1") не равны правам цели"
    all_out | grep -qF "$1" || fail "вывод не называет бэкап $1: $(all_out | head -6)"
  done
  undo
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" . 'settings.json после undo неполного apply'
  jq_eq "$HOME/.codex/hooks.json" "$ORIG/hooks.json" . 'hooks.json после undo неполного apply'
}

t_undo_restores_original_json() {
  new_home; seed_all
  apply; undo
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" . 'Claude settings.json после undo'
  jq_eq "$HOME/.codex/hooks.json" "$ORIG/hooks.json" . 'Codex hooks.json после undo'
  same_bytes "$HOME/.codex/rules/default.rules" "$ORIG/default.rules"
  same_bytes "$HOME/.codex/config.toml" "$ORIG/config.toml"
  must_absent "$HOME/.codex/rules/tk.rules"
  must_absent "$HOME/.agents/skills/tk"
  must_absent "$HOME/.claude/skills"
  must_absent "$(MANIFEST)"
  [ -z "$(baks)" ] || fail "undo оставил бэкапы: $(baks)"
}

t_undo_on_pristine_home_leaves_no_trace() {
  new_home
  apply; undo
  [ -z "$(find "$HOME" -mindepth 1)" ] || fail "после undo на пустом HOME остались следы: $(find "$HOME" -mindepth 1 | head -5)"
}

t_existing_tk_permission_survives_undo() {
  new_home
  jq '.permissions.allow += ["Bash(tk *)"]' "$FIX/claude-settings.json" >"$BASE/s.json"
  seed_claude claude-settings.json
  cp "$BASE/s.json" "$HOME/.claude/settings.json"; cp "$BASE/s.json" "$ORIG/settings.json"
  apply
  jq_ok "$HOME/.claude/settings.json" '[.permissions.allow[] | select(. == "Bash(tk *)")] | length == 1' 'Bash(tk *) не продублирован'
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" '.permissions' 'allow изменён apply, хотя ключ уже был'
  undo
  jq_ok "$HOME/.claude/settings.json" '.permissions.allow | index("Bash(tk *)") != null' 'undo снял чужой Bash(tk *)'
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" '.permissions' 'permissions после undo'
}

t_existing_own_hook_upgraded_and_restored() {
  new_home; seed_claude claude-settings-ownhook.json
  apply
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tk ctx --hook")] | length == 1' 'свой хук tk ctx не стал tk ctx --hook'
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tk ctx")] | length == 0' 'остался старый хук tk ctx'
  run apply --agent all --now "$NOW2"
  [ "$RC" -eq 0 ] || fail "повторный apply: код $RC"
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select((.command? // "") | startswith("tk ctx"))] | length == 1' 'повторный apply продублировал хук'
  undo
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" . 'свой хук tk ctx должен вернуться как был'
}

# Хук, поставленный прежней версией (коннектор с `tk ctx`), обновляется на месте и снимается целиком.
t_old_installed_hook_upgraded() {
  new_home
  local old=$BASE/oldroot cur=$CONNECT
  rm -rf "$old"; mkdir -p "$old/scripts"
  cp "$CONNECT" "$old/scripts/connect.sh"; cp -R "$CONN" "$old/connectors"
  sed -i.bak 's/tk ctx --hook/tk ctx/' "$old/connectors/claude/hooks.json"; rm -f "$old/connectors/claude/hooks.json.bak"
  CONNECT=$old/scripts/connect.sh apply
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tk ctx")] | length == 1' 'старый коннектор не поставил tk ctx'
  CONNECT=$cur
  apply
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tk ctx --hook")] | length == 1' 'хук не обновлён на tk ctx --hook'
  jq_ok "$HOME/.claude/settings.json" '[.. | objects | select(.command? == "tk ctx")] | length == 0' 'остался старый хук tk ctx'
  jq_ok "$HOME/.claude/settings.json" '.hooks.SessionStart | length == 1' 'группа SessionStart продублирована'
  jq_ok "$HOME/.codex/hooks.json" '[.. | objects | select(.command? == "tk ctx")] | length == 1' 'Codex: хук должен остаться tk ctx'
  undo
  [ -z "$(find "$HOME" -mindepth 1)" ] || fail "после undo остались следы: $(find "$HOME" -mindepth 1 | head -5)"
}

t_modified_own_files_skipped_without_force() {
  new_home
  apply
  local f
  echo '# mine' >>"$HOME/.codex/rules/tk.rules"
  echo 'mine' >>"$HOME/.claude/skills/tk/SKILL.md"
  echo 'mine' >>"$HOME/.agents/skills/tk/SKILL.md"
  local before
  before=$(snap)
  apply "$NOW2"
  [ "$before" = "$(snap)" ] || fail "apply без --force перезаписал изменённые файлы"
  for f in "$HOME/.codex/rules/tk.rules" "$HOME/.claude/skills/tk" "$HOME/.agents/skills/tk"; do
    all_out | grep -iF 'warn' | grep -qF "$f" || fail "нет warn с путём $f: $(all_out | head -6)"
  done
  [ -z "$(baks)" ] || fail "бэкапы после apply: $(baks)"
  run apply --agent all --now "$NOW2" --force
  [ "$RC" -eq 0 ] || fail "apply --force: код $RC: $(all_out | head -5)"
  same_bytes "$HOME/.codex/rules/tk.rules" "$CONN/codex/tk.rules" 'tk.rules после --force'
  diff -r "$SKILL_SRC" "$HOME/.claude/skills/tk" >/dev/null || fail "скилл Claude после --force отличается от источника"
  diff -r "$SKILL_SRC" "$HOME/.agents/skills/tk" >/dev/null || fail "скилл Codex после --force отличается от источника"
}

t_undo_removes_only_manifest_files() {
  new_home
  apply
  echo mine >"$HOME/.claude/skills/tk/extra.txt"
  mkdir -p "$HOME/.agents/skills/tk/sub"
  echo mine >"$HOME/.agents/skills/tk/sub/note.txt"
  run undo --agent all --now "$NOW2"
  [ "$RC" -eq 0 ] || fail "undo: код $RC: $(all_out | head -5)"
  must_exist "$HOME/.claude/skills/tk/extra.txt"
  must_exist "$HOME/.agents/skills/tk/sub/note.txt"
  must_absent "$HOME/.claude/skills/tk/SKILL.md"
  must_absent "$HOME/.agents/skills/tk/SKILL.md"
  must_absent "$HOME/.codex/rules/tk.rules"
  all_out | grep -qF "$HOME/.claude/skills/tk" || fail "undo не назвал оставленный каталог: $(all_out | head -6)"
}

t_undo_skips_paths_outside_whitelist() {
  new_home
  mkdir -p "$HOME/tasks/.tracker" "$HOME/victimdir"
  echo keep >"$HOME/victim.txt"
  echo keep >"$HOME/victimdir/a.txt"
  echo '{"keep":1}' >"$HOME/other.json"
  jq -n --arg h "$HOME" '{version:1, dirs_created:[($h + "/victimdir")], entries:[
    {key:"k1", agent:"codex", kind:"file", path:($h + "/victim.txt"), dirs_created:[]},
    {key:"k2", agent:"claude", kind:"dir", path:($h + "/victimdir"), files:["a.txt"], dirs_created:[]},
    {key:"k3", agent:"claude", kind:"json", op:"arr", file:($h + "/other.json"), path:["keep"], value:1, created:[], file_created:true, dirs_created:[]},
    {key:"k4", agent:"claude", kind:"file", path:"/etc/hosts", dirs_created:["/etc"]}]}' >"$(MANIFEST)"
  run undo --agent all --now "$NOW2"
  [ "$RC" -ne 0 ] || fail "undo с чужими путями вернул 0"
  must_exist "$HOME/victim.txt"
  must_exist "$HOME/victimdir/a.txt"
  jq_ok "$HOME/other.json" '.keep == 1' 'other.json изменён'
  [ -e /etc/hosts ] || fail "нет /etc/hosts?"
  local p
  for p in "$HOME/victim.txt" "$HOME/victimdir" "$HOME/other.json" /etc/hosts; do
    all_out | grep -iF 'warn' | grep -qF "$p" || fail "нет warn с путём $p: $(all_out | head -8)"
  done
  must_exist "$(MANIFEST)"
}

t_undo_without_manifest_says_so() {
  new_home
  undo
  [ "$OUT" = "nothing to undo: no manifest at $HOME/tasks/.tracker/connect-state.json" ] || fail "вывод: '$OUT'"
  RUN_ENV=("TRACKER_HOME=$H/alt")
  undo
  RUN_ENV=()
  [ "$OUT" = "nothing to undo: no manifest at $H/alt/.tracker/connect-state.json" ] || fail "вывод с TRACKER_HOME: '$OUT'"
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
    [ -L "$HOME/$rel" ] || fail "$rel перестал быть симлинком"
    [ "$(readlink "$HOME/$rel")" = "$HOME/dotfiles/$name" ] || fail "$rel указывает на $(readlink "$HOME/$rel")"
    [ "$(mode "$HOME/dotfiles/$name")" = "$want" ] || fail "права цели $name: $(mode "$HOME/dotfiles/$name"), ожидалось $want"
  done
  jq_ok "$HOME/dotfiles/settings.json" '.permissions.allow | index("Bash(tk *)") != null' 'запись ушла в цель симлинка'
  jq_ok "$HOME/dotfiles/settings.json" '[.. | objects | select(.command? == "tk ctx --hook")] | length == 1' 'хук Claude ушёл в цель симлинка'
  jq_ok "$HOME/dotfiles/hooks.json" '[.. | objects | select(.command? == "tk ctx")] | length == 1' 'хук Codex ушёл в цель симлинка'
  undo
  for link in .claude/settings.json:settings.json .codex/hooks.json:hooks.json; do
    IFS=: read -r rel name <<<"$link"
    [ -L "$HOME/$rel" ] || fail "$rel перестал быть симлинком после undo"
  done
  jq_eq "$HOME/dotfiles/settings.json" "$ORIG/settings.json" . 'цель settings.json после undo'
  jq_eq "$HOME/dotfiles/hooks.json" "$ORIG/hooks.json" . 'цель hooks.json после undo'
}

t_symlinked_skill_dir_target_untouched() {
  new_home
  # .agents/skills - ссылка на чужой каталог: apply пишет в цель, ссылку не рвёт.
  mkdir -p "$HOME/dotfiles/skills"
  mkdir -p "$HOME/.agents"
  ln -s "$HOME/dotfiles/skills" "$HOME/.agents/skills"
  apply
  [ -L "$HOME/.agents/skills" ] || fail ".agents/skills перестал быть ссылкой"
  must_exist "$HOME/dotfiles/skills/tk/SKILL.md"
  undo
  [ -L "$HOME/.agents/skills" ] || fail ".agents/skills перестал быть ссылкой после undo"
  must_absent "$HOME/dotfiles/skills/tk"
}

t_permissions_600_preserved() {
  new_home; seed_all
  chmod 600 "$HOME/.claude/settings.json"
  chmod 640 "$HOME/.codex/hooks.json"
  apply
  [ -f "$HOME/.claude/settings.json" ] && [ ! -L "$HOME/.claude/settings.json" ] || fail "settings.json не обычный файл"
  [ "$(mode "$HOME/.claude/settings.json")" = 600 ] || fail "settings.json после apply: $(mode "$HOME/.claude/settings.json")"
  [ "$(mode "$HOME/.codex/hooks.json")" = 640 ] || fail "hooks.json после apply: $(mode "$HOME/.codex/hooks.json")"
  undo
  [ "$(mode "$HOME/.claude/settings.json")" = 600 ] || fail "settings.json после undo: $(mode "$HOME/.claude/settings.json")"
  [ "$(mode "$HOME/.codex/hooks.json")" = 640 ] || fail "hooks.json после undo: $(mode "$HOME/.codex/hooks.json")"
}

t_foreign_content_intact() {
  new_home; seed_all
  echo 'user backup' >"$HOME/.claude/settings.json.bak"
  apply
  local s=$HOME/.claude/settings.json
  jq_eq "$s" "$ORIG/settings.json" 'del(.permissions.allow) | del(.hooks.SessionStart[] | select(any(.hooks[]?; .command == "tk ctx --hook")))' 'Claude: чужие ключи (кроме allow и нашей группы)'
  [ "$(jq -c '.permissions.allow[0:3]' "$s")" = "$(jq -c '.permissions.allow' "$ORIG/settings.json")" ] || fail "порядок чужих allow нарушен: $(jq -c .permissions.allow "$s")"
  jq_ok "$s" '.hooks.SessionStart[0].hooks[0].command == "echo foreign-claude-start"' 'Claude: чужая группа осталась первой'
  jq_eq "$HOME/.codex/hooks.json" "$ORIG/hooks.json" 'del(.hooks.SessionStart[] | select(any(.hooks[]?; .command == "tk ctx")))' 'Codex: чужие группы SessionStart и Stop'
  jq_ok "$HOME/.codex/hooks.json" '.hooks.SessionStart[0].hooks[0].command == "echo foreign-codex-start"' 'Codex: чужая группа осталась первой'
  same_bytes "$HOME/.codex/rules/default.rules" "$ORIG/default.rules" 'default.rules изменён'
  same_bytes "$HOME/.codex/config.toml" "$ORIG/config.toml" 'config.toml изменён'
  must_absent "$HOME/.claude/CLAUDE.md"
  must_absent "$HOME/.codex/AGENTS.md"
  undo
  [ "$(cat "$HOME/.claude/settings.json.bak" 2>/dev/null)" = 'user backup' ] || fail "undo удалил чужой settings.json.bak"
}

t_agent_filter_touches_only_that_agent() {
  new_home
  run apply --agent claude --now "$NOW1"
  [ "$RC" -eq 0 ] || fail "apply --agent claude: код $RC: $(all_out | head -5)"
  must_exist "$HOME/.claude/settings.json"
  must_absent "$HOME/.codex"
  must_absent "$HOME/.agents"
  run apply --agent codex --now "$NOW1"
  [ "$RC" -eq 0 ] || fail "apply --agent codex: код $RC"
  run undo --agent claude --now "$NOW2"
  [ "$RC" -eq 1 ] || fail "undo одного агента при втором в манифесте: ожидался код 1, есть $RC"
  must_absent "$HOME/.claude"
  must_exist "$HOME/.codex/hooks.json"
  must_exist "$(MANIFEST)"
  undo
  [ -z "$(find "$HOME" -mindepth 1)" ] || fail "следы после undo: $(find "$HOME" -mindepth 1 | head -5)"
}

t_missing_tools_err_and_files_intact() {
  local missing
  for missing in jq tk; do
    new_home; seed_all
    local before after
    before=$(snap)
    TOOLS_OVERRIDE=$BASE/tools-no-$missing run apply --agent all --now "$NOW1"
    unset TOOLS_OVERRIDE
    [ "$RC" -ne 0 ] || fail "без $missing apply вернул 0"
    all_out | grep -qi 'err' || fail "без $missing нет сообщения err: $(all_out | head -3)"
    all_out | grep -qF "$missing" || fail "без $missing сообщение не называет утилиту: $(all_out | head -3)"
    after=$(snap)
    [ "$before" = "$after" ] || fail "без $missing apply изменил файлы"$'\n'"$(diff <(echo "$before") <(echo "$after") | head -6)"
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
    [ "$RC" -ne 0 ] || fail "$fixture: apply вернул 0"
    all_out | grep -qi 'err' || fail "$fixture: нет err: $(all_out | head -3)"
    all_out | grep -qF "$target" || fail "$fixture: сообщение не называет путь $target: $(all_out | head -3)"
    same_bytes "$HOME/$target" "$ORIG/orig" "$fixture: файл изменён"
    [ -z "$(baks)" ] || fail "$fixture: лишний бэкап: $(baks)"
    [ -z "$(find "$HOME" -type f -not -path "$HOME/$target")" ] || fail "$fixture: записаны другие файлы: $(find "$HOME" -type f -not -path "$HOME/$target" | head -3)"
  done
}

t_hook_reads_stdin_and_output_is_short() {
  new_home
  apply
  local cmds=() c name out rc w f
  for f in claude:.claude/settings.json codex:.codex/hooks.json; do
    name=${f%%:*}
    for w in $MATCHER_WORDS; do
      jq_ok "$HOME/${f#*:}" "[.hooks.SessionStart[].matcher // \"\" | split(\"|\") | index(\"$w\")] | any(. != null)" "$name: matcher SessionStart без $w"
    done
    while IFS= read -r c; do cmds+=("$name|$c"); done < <(jq -r '.hooks.SessionStart[] | select(any(.hooks[]?; (.command | startswith("tk ctx")))) | .hooks[].command' "$HOME/${f#*:}")
  done
  [ "${#cmds[@]}" -eq 2 ] || fail "найдено хуков tk ctx: ${#cmds[@]}, нужно по одному в Claude и Codex"
  for c in "${cmds[@]}"; do
    name=${c%%|*}; c=${c#*|}
    out=$(env -i HOME="$HOME" PATH="$TOOLS" LANG=C "$TOOLS/sh" -c "$c" <"$FIX/hook-stdin.json" 2>"$H.err"); rc=$?
    [ "$rc" -eq 0 ] || fail "$name: хук '$c' вернул $rc: $(head -3 "$H.err")"
    [ "${#out}" -lt 2000 ] || fail "$name: вывод хука ${#out} знаков, нужно меньше 2000"
    [ -n "$out" ] || fail "$name: хук ничего не вывел в контекст"
  done
}

t_skill_copies_identical_and_small() {
  new_home
  apply
  local a=$SKILL_SRC/SKILL.md d
  [ -f "$a" ] || fail "нет $a"
  for d in "$HOME/.claude/skills/tk" "$HOME/.agents/skills/tk"; do
    diff -r "$SKILL_SRC" "$d" >/dev/null || fail "копия скилла $d отличается от источника"
  done
  [ "$(wc -c <"$a")" -le 1536 ] || fail "SKILL.md больше 1,5 КБ: $(wc -c <"$a")"
  head -8 "$a" | grep -q '^name: tk$' || fail "в SKILL.md нет name: tk"
  head -8 "$a" | grep -q '^description:.*Use when' || fail "в description нет 'Use when'"
}

# ---------- запуск ----------

TESTS=(
  t_guard_rejects_real_home
  t_validate_connectors
  t_plan_writes_nothing_and_lists_targets
  t_apply_clean_writes_exact_paths
  t_tracker_home_sets_manifest_place
  t_apply_twice_no_changes
  t_backups_live_only_during_apply
  t_failed_apply_keeps_and_names_backups
  t_undo_restores_original_json
  t_undo_on_pristine_home_leaves_no_trace
  t_existing_tk_permission_survives_undo
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
  echo "FAIL  настоящий HOME изменён во время прогона:" >&2
  diff <(echo "$REAL_BEFORE") <(echo "$REAL_AFTER") >&2
  exit 3
fi
echo "итого: $pass ok, $failed FAIL; настоящий HOME не тронут"
[ "$failed" -eq 0 ]
