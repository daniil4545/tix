#!/usr/bin/env bash
# Тесты scripts/connect.sh (зона T плана docs/plans/z3-connectors.md).
#
# Каждый тест идёт на своём HOME из mktemp. Настоящий HOME не читается и не пишется:
# перед запуском connect.sh тест проверяет подмену HOME, в конце сверяет отпечаток
# (stat, без чтения содержимого) файлов настоящего HOME с отпечатком до прогона.
# Агенты (codex, opencode) и модели не запускаются; claude - только `plugin ...`.
#
# Запуск: scripts/test-connect.sh [имя_теста ...]
# Окружение: TK_BIN=<путь к настоящему tk> - прогнать хук и на нём (иначе заглушка);
#            TK_TEST_KEEP=1 - не удалять временный каталог.
set -uo pipefail

SELF_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO=$(cd "$SELF_DIR/.." && pwd)
CONNECT=$REPO/scripts/connect.sh
FIX=$REPO/connectors/testdata
MKT=$REPO/connectors/claude
PLUGIN=$MKT/plugins/tk
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
  .claude/settings.json .codex/hooks.json .codex/rules/tk.rules .codex/rules/default.rules
  .codex/config.toml .agents/skills/tk .config/tk/connect-state.json
  .config/opencode/opencode.json .config/opencode/plugins/tk.js
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
  find "$REAL_HOME/.claude" "$REAL_HOME/.codex" "$REAL_HOME/.config/opencode" "$REAL_HOME/.config/tk" \
    -maxdepth 2 -name '*.tk-bak-*' 2>/dev/null | LC_ALL=C sort
}

REAL_BEFORE=$(real_fingerprint)

# ---------- окружение: каталог утилит и заглушка tk ----------

# PATH тестов - только этот каталог: так видно, что скрипт требует jq, claude и tk,
# и можно убрать любую из утилит.
BASE_TOOLS=(bash env sh cat mv cp rm mkdir date dirname basename realpath stat chmod ln mktemp sed awk
  grep sort cut tr head tail readlink cmp diff ls printf touch find shasum sha256sum tee wc uname id
  xargs uniq tput rmdir mkfifo sleep expr test true false)

link_tools() { # каталог, исключение
  local dir=$1 skip=${2:-} t p
  mkdir -p "$dir"
  for t in "${BASE_TOOLS[@]}" jq claude; do
    [ "$t" = "$skip" ] && continue
    p=$(command -v "$t" 2>/dev/null) && [ -x "$p" ] && ln -sf "$p" "$dir/$t"
  done
  [ "$skip" = tk ] && return 0
  if [ -n "${TK_BIN:-}" ]; then ln -sf "$TK_BIN" "$dir/tk"; else cp "$BASE/tk-stub" "$dir/tk"; fi
}

# Заглушка tk: знает только `ctx`, любая другая форма вызова - код 64.
cat >"$BASE/tk-stub" <<'EOF'
#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = ctx ]; then
  cat >/dev/null 2>&1 || true
  echo "tk ctx: stub, 0 tasks"
  exit 0
fi
echo "tk stub: unexpected args: $*" >&2
exit 64
EOF
chmod +x "$BASE/tk-stub"
link_tools "$TOOLS"
for x in jq claude tk; do link_tools "$BASE/tools-no-$x" "$x"; done

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

RC=0 OUT="" ERR=""
run() { # run <ключевые слова connect.sh>; результат в RC, OUT, ERR
  assert_fake_home || fail "HOME не подменён перед запуском connect.sh: $HOME"
  [ -f "$CONNECT" ] || fail "scripts/connect.sh не существует"
  local tools=${TOOLS_OVERRIDE:-$TOOLS}
  OUT=$(env -i HOME="$HOME" PATH="$tools" TERM=dumb LANG=C TMPDIR="$BASE/tmp" \
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
baks() { find "$HOME" \( -path '*/.claude/plugins' -o -path '*/.claude/backups' \) -prune -o -name '*.tk-bak-*' -print | LC_ALL=C sort; }

snap() { # снимок файлов, которые трогает connect.sh: путь, ссылка, права, хэш
  (
    cd "$HOME" || exit 1
    { find .claude -maxdepth 1 \( -type f -o -type l \); find .codex .agents .config \( -type f -o -type l \); } 2>/dev/null |
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
seed_opencode() { mkdir -p "$HOME/.config/opencode"; cp "$FIX/${1:-opencode.json}" "$HOME/.config/opencode/opencode.json"; cp "$FIX/${1:-opencode.json}" "$ORIG/opencode.json"; }
seed_all() { seed_claude claude-settings.json; seed_codex; seed_opencode; }

# Ключи, которые claude CLI добавляет сам; в тестах не про них исходный JSON не имеет таких ключей.
NO_PLUGIN_KEYS='del(.enabledPlugins, .extraKnownMarketplaces)'

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
  while IFS= read -r f; do jq empty "$f" 2>/dev/null || fail "не JSON: $f"; done < <(find "$REPO/connectors" -name '*.json' -not -path '*/testdata/claude-settings-broken.json')
  [ -f "$MKT/.claude-plugin/marketplace.json" ] || fail "нет $MKT/.claude-plugin/marketplace.json"
  local d
  for d in "$MKT" "$PLUGIN"; do
    OUT=$(env -i HOME="$HOME" PATH="$TOOLS" TERM=dumb LANG=C TMPDIR="$BASE/tmp" "$TOOLS/claude" plugin validate "$d" 2>&1); RC=$?
    [ "$RC" -eq 0 ] || fail "claude plugin validate $d: код $RC: $(echo "$OUT" | head -5)"
  done
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
  must_absent "$HOME/.claude/plugins"
  must_absent "$HOME/.config/tk/connect-state.json"
  run plan --agent all
  all_out | grep -qF 'Bash(tk *)' || fail "plan не называет ключ Bash(tk *): $(all_out | head -5)"
  all_out | grep -qF 'hooks.json' || fail "plan не называет .codex/hooks.json"
}

t_apply_clean_writes_all_targets() {
  new_home
  apply
  local s=$HOME/.claude/settings.json
  jq_ok "$s" '.permissions.allow | index("Bash(tk *)") != null' 'Claude: Bash(tk *) в allow'
  jq_ok "$s" '.enabledPlugins["tk@tk-local"] == true' 'Claude: плагин включён'
  jq_ok "$s" '.extraKnownMarketplaces["tk-local"] != null' 'Claude: маркетплейс tk-local'
  local src
  src=$(jq -r '.extraKnownMarketplaces["tk-local"].source.path // empty' "$s")
  [ -f "$src/.claude-plugin/marketplace.json" ] || fail "путь маркетплейса не ведёт к каталогу с marketplace.json: '$src'"
  jq_ok "$HOME/.claude/plugins/installed_plugins.json" '.plugins["tk@tk-local"] | length > 0' 'claude plugin install выполнен'

  local h=$HOME/.codex/hooks.json
  jq_ok "$h" '[.hooks.SessionStart[] | select(.matcher == "startup|resume|clear|compact") | .hooks[] | select(.command == "tk ctx")] | length == 1' 'Codex: группа SessionStart с tk ctx'
  grep -qF 'prefix_rule(pattern=["tk"], decision="allow")' "$HOME/.codex/rules/tk.rules" 2>/dev/null || fail "Codex: tk.rules без prefix_rule для tk"
  must_exist "$HOME/.agents/skills/tk/SKILL.md"
  must_absent "$HOME/.codex/config.toml"
  must_absent "$HOME/.codex/rules/default.rules"

  local o=$HOME/.config/opencode/opencode.json
  jq_ok "$o" '.permission.bash["tk *"] == "allow"' 'OpenCode: tk * allow'
  jq_ok "$o" '.permission.bash | keys_unsorted | last == "tk *"' 'OpenCode: tk * последним ключом'
  grep -q 'TRACKER_ACTOR' "$HOME/.config/opencode/plugins/tk.js" 2>/dev/null || fail "OpenCode: plugins/tk.js без TRACKER_ACTOR"
  jq empty "$HOME/.config/tk/connect-state.json" 2>/dev/null || fail "манифест connect-state.json отсутствует или не JSON"

  run plan --agent all
  [ "$RC" -eq 0 ] || fail "повторный plan: код $RC"
  [ -z "$(printf '%s' "$OUT" | tr -d '[:space:]')" ] || fail "повторный plan не пуст: $(echo "$OUT" | head -5)"
}

t_apply_twice_no_changes() {
  new_home; seed_all
  apply "$NOW1"
  local s1 b1 s2 b2
  s1=$(snap); b1=$(baks)
  [ -n "$b1" ] || fail "первый apply не сделал бэкап ни одного существующего файла"
  apply "$NOW2"
  s2=$(snap); b2=$(baks)
  [ "$s1" = "$s2" ] || fail "второй apply изменил файлы:"$'\n'"$(diff <(echo "$s1") <(echo "$s2") | head -10)"
  [ "$b1" = "$b2" ] || fail "второй apply добавил бэкапы:"$'\n'"$(diff <(echo "$b1") <(echo "$b2") | head -5)"
}

t_backup_before_edit_same_rights() {
  new_home; seed_all
  chmod 640 "$HOME/.codex/hooks.json"
  apply
  local pair f b
  for pair in ".claude/settings.json:settings.json" ".codex/hooks.json:hooks.json" ".config/opencode/opencode.json:opencode.json"; do
    f=$HOME/${pair%%:*}
    set -- "$f".tk-bak-*
    [ -e "$1" ] && [ "$#" -eq 1 ] || fail "ожидался один бэкап рядом с $f, есть: $*"
    same_bytes "$1" "$ORIG/${pair##*:}" "бэкап не равен исходному файлу"
    [ "$(mode "$1")" = "$(mode "$f")" ] || fail "права бэкапа $(mode "$1") не равны правам цели $(mode "$f")"
  done
}

t_undo_restores_original_json() {
  new_home; seed_all
  apply; undo
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" . 'Claude settings.json после undo'
  jq_eq "$HOME/.codex/hooks.json" "$ORIG/hooks.json" . 'Codex hooks.json после undo'
  jq_eq "$HOME/.config/opencode/opencode.json" "$ORIG/opencode.json" . 'opencode.json после undo'
  same_bytes "$HOME/.codex/rules/default.rules" "$ORIG/default.rules"
  same_bytes "$HOME/.codex/config.toml" "$ORIG/config.toml"
  must_absent "$HOME/.codex/rules/tk.rules"
  must_absent "$HOME/.agents/skills/tk"
  must_absent "$HOME/.config/opencode/plugins/tk.js"
  must_absent "$HOME/.config/tk/connect-state.json"
  [ -z "$(baks)" ] || fail "undo оставил свои бэкапы: $(baks)"
  jq_ok "$HOME/.claude/plugins/installed_plugins.json" '(.plugins["tk@tk-local"] // []) | length == 0' 'плагин tk снят'
}

t_undo_on_pristine_home_leaves_no_trace() {
  new_home
  apply; undo
  local f
  for f in .claude/settings.json .codex/hooks.json .config/opencode/opencode.json; do
    if [ -e "$HOME/$f" ]; then
      [ "$(jq -S -c . "$HOME/$f" 2>&1)" = '{}' ] || fail "после undo на пустом HOME $f не пуст: $(jq -c . "$HOME/$f" | head -c 200)"
    fi
  done
  must_absent "$HOME/.codex/rules/tk.rules"
  must_absent "$HOME/.agents/skills/tk"
  must_absent "$HOME/.config/opencode/plugins/tk.js"
  must_absent "$HOME/.config/tk/connect-state.json"
  [ -z "$(baks)" ] || fail "остались бэкапы: $(baks)"
}

t_undo_keeps_foreign_plugin_keys() {
  new_home; seed_claude claude-settings-plugins.json
  apply
  jq_ok "$HOME/.claude/settings.json" '.enabledPlugins["other@other-mkt"] == true and .extraKnownMarketplaces["other-mkt"] != null' 'чужие ключи плагинов после apply'
  undo
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" . 'settings.json с чужими плагинами после undo'
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

t_opencode_already_allowed_is_not_touched() {
  local variant
  for variant in wildcard explicit; do
    new_home
    if [ "$variant" = wildcard ]; then seed_opencode opencode-wildcard.json
    else
      jq '.permission.bash["tk *"] = "allow"' "$FIX/opencode.json" >"$BASE/o.json"
      mkdir -p "$HOME/.config/opencode"; cp "$BASE/o.json" "$HOME/.config/opencode/opencode.json"; cp "$BASE/o.json" "$ORIG/opencode.json"
    fi
    local o=$HOME/.config/opencode/opencode.json
    apply
    same_bytes "$o" "$ORIG/opencode.json" "$variant: apply переписал opencode.json, хотя tk уже разрешён"
    set -- "$o".tk-bak-*
    [ ! -e "$1" ] || fail "$variant: apply сделал бэкап opencode.json без записи"
    undo
    same_bytes "$o" "$ORIG/opencode.json" "$variant: undo изменил opencode.json"
  done
}

t_symlinked_configs_stay_symlinks() {
  new_home; seed_codex
  mkdir -p "$HOME/dotfiles" "$HOME/.claude"
  local link
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
  jq_ok "$HOME/dotfiles/hooks.json" '[.. | objects | select(.command? == "tk ctx")] | length == 1' 'хук ушёл в цель симлинка'
  undo
  for link in .claude/settings.json:settings.json .codex/hooks.json:hooks.json; do
    IFS=: read -r rel name <<<"$link"
    [ -L "$HOME/$rel" ] || fail "$rel перестал быть симлинком после undo"
  done
  jq_eq "$HOME/dotfiles/settings.json" "$ORIG/settings.json" "$NO_PLUGIN_KEYS" 'цель settings.json после undo'
  jq_eq "$HOME/dotfiles/hooks.json" "$ORIG/hooks.json" . 'цель hooks.json после undo'
}

t_permissions_600_preserved() {
  new_home; seed_all
  chmod 600 "$HOME/.claude/settings.json" "$HOME/.config/opencode/opencode.json"
  chmod 640 "$HOME/.codex/hooks.json"
  apply
  [ -f "$HOME/.claude/settings.json" ] && [ ! -L "$HOME/.claude/settings.json" ] || fail "settings.json не обычный файл"
  [ "$(mode "$HOME/.claude/settings.json")" = 600 ] || fail "settings.json после apply: $(mode "$HOME/.claude/settings.json")"
  [ "$(mode "$HOME/.config/opencode/opencode.json")" = 600 ] || fail "opencode.json после apply: $(mode "$HOME/.config/opencode/opencode.json")"
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
  jq_eq "$s" "$ORIG/settings.json" 'del(.permissions.allow) | del(.enabledPlugins, .extraKnownMarketplaces)' 'Claude: чужие ключи (кроме allow)'
  [ "$(jq -c '.permissions.allow[0:3]' "$s")" = "$(jq -c '.permissions.allow' "$ORIG/settings.json")" ] || fail "порядок чужих allow нарушен: $(jq -c .permissions.allow "$s")"
  jq_eq "$HOME/.codex/hooks.json" "$ORIG/hooks.json" 'del(.hooks.SessionStart[] | select(any(.hooks[]?; .command == "tk ctx")))' 'Codex: чужие группы SessionStart и Stop'
  jq_ok "$HOME/.codex/hooks.json" '.hooks.SessionStart[0].hooks[0].command == "echo foreign-codex-start"' 'Codex: чужая группа осталась первой'
  same_bytes "$HOME/.codex/rules/default.rules" "$ORIG/default.rules" 'default.rules изменён'
  same_bytes "$HOME/.codex/config.toml" "$ORIG/config.toml" 'config.toml изменён'
  local o=$HOME/.config/opencode/opencode.json
  jq_eq "$o" "$ORIG/opencode.json" 'del(.permission.bash["tk *"])' 'OpenCode: чужие ключи'
  [ "$(jq -c '.permission.bash | keys_unsorted[0:-1]' "$o")" = "$(jq -c '.permission.bash | keys_unsorted' "$ORIG/opencode.json")" ] || fail "порядок чужих ключей permission.bash нарушен"
  must_absent "$HOME/.claude/CLAUDE.md"
  must_absent "$HOME/.codex/AGENTS.md"
  must_absent "$HOME/.config/opencode/AGENTS.md"
  undo
  [ "$(cat "$HOME/.claude/settings.json.bak" 2>/dev/null)" = 'user backup' ] || fail "undo удалил чужой settings.json.bak"
}

t_cli_rewritten_foreign_key_is_restored() {
  new_home; seed_claude claude-settings-model.json
  run apply --agent all --now "$NOW1"
  [ "$RC" -eq 0 ] || fail "apply: код $RC: $(all_out | head -5)"
  all_out | grep -i 'warn' | grep -q 'model' || fail "apply не предупредил про ключ model: $(all_out | head -5)"
  jq_ok "$HOME/.claude/settings.json" '.model == "opus"' 'после apply model не "opus"'
  undo
  jq_ok "$HOME/.claude/settings.json" '.model == "opus"' 'после undo model не "opus"'
  jq_eq "$HOME/.claude/settings.json" "$ORIG/settings.json" . 'settings.json после undo'
}

t_missing_tools_err_and_files_intact() {
  local missing
  for missing in jq claude tk; do
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
    set -- "$HOME/$target".tk-bak-*
    [ ! -e "$1" ] || fail "$fixture: лишний бэкап"
  done
}

t_hook_reads_stdin_and_output_is_short() {
  new_home
  apply
  local cmds=() c name out rc tools
  while IFS= read -r c; do cmds+=("codex|$c"); done < <(jq -r '.hooks.SessionStart[] | select(any(.hooks[]?; .command == "tk ctx")) | .hooks[].command' "$HOME/.codex/hooks.json")
  local ph=$PLUGIN/hooks/hooks.json
  [ -f "$ph" ] || fail "нет $ph"
  for w in $MATCHER_WORDS; do
    jq_ok "$ph" "[.hooks.SessionStart[].matcher | split(\"|\") | index(\"$w\")] | any(. != null)" "Claude: matcher SessionStart без $w"
  done
  while IFS= read -r c; do cmds+=("claude|$c"); done < <(jq -r '.hooks.SessionStart[].hooks[].command' "$ph")
  [ "${#cmds[@]}" -ge 2 ] || fail "найдено хуков: ${#cmds[@]}, нужно по одному в Claude и Codex"
  for c in "${cmds[@]}"; do
    name=${c%%|*}; c=${c#*|}
    out=$(env -i HOME="$HOME" PATH="$TOOLS" CLAUDE_PLUGIN_ROOT="$PLUGIN" LANG=C "$TOOLS/sh" -c "$c" <"$FIX/hook-stdin.json" 2>"$H.err"); rc=$?
    [ "$rc" -eq 0 ] || fail "$name: хук '$c' вернул $rc: $(head -3 "$H.err")"
    [ "${#out}" -lt 2000 ] || fail "$name: вывод хука ${#out} знаков, нужно меньше 2000"
    [ -n "$out" ] || fail "$name: хук ничего не вывел в контекст"
  done
}

t_skill_copies_identical_and_small() {
  new_home
  apply
  local a=$PLUGIN/skills/tk/SKILL.md b=$HOME/.agents/skills/tk/SKILL.md
  [ -f "$a" ] || fail "нет $a"
  diff -r "$PLUGIN/skills/tk" "$HOME/.agents/skills/tk" >/dev/null || fail "копии скилла различаются"
  [ "$(wc -c <"$a")" -le 1536 ] || fail "SKILL.md больше 1,5 КБ: $(wc -c <"$a")"
  head -8 "$a" | grep -q '^name: tk$' || fail "в SKILL.md нет name: tk"
  head -8 "$a" | grep -q '^description:.*Use when' || fail "в description нет 'Use when'"
}

# ---------- запуск ----------

TESTS=(
  t_guard_rejects_real_home
  t_validate_connectors
  t_plan_writes_nothing_and_lists_targets
  t_apply_clean_writes_all_targets
  t_apply_twice_no_changes
  t_backup_before_edit_same_rights
  t_undo_restores_original_json
  t_undo_on_pristine_home_leaves_no_trace
  t_undo_keeps_foreign_plugin_keys
  t_existing_tk_permission_survives_undo
  t_opencode_already_allowed_is_not_touched
  t_symlinked_configs_stay_symlinks
  t_permissions_600_preserved
  t_foreign_content_intact
  t_cli_rewritten_foreign_key_is_restored
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
