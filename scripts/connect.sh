#!/usr/bin/env bash
# Установка коннекторов агентов (Claude Code, Codex) для tix.
#
#   connect.sh plan|apply|undo [--agent claude|codex|all] [--now ISO] [--force]
#
# plan (по умолчанию) печатает, что изменит apply, и ничего не пишет; повторный plan пуст.
# apply добавляет недостающее и записывает добавленное в манифест; чужое не трогает.
# Бэкапы <файл>.tix-bak-<время> живут только на время apply; при сбое остаются и называются.
# Наш файл или скилл уже есть и отличается от источника: warn и пропуск; перезапись только с --force.
# undo снимает только записи манифеста (файлы скилла по списку) и сам манифест.
# Манифест: ${TIX_HOME:-$HOME/tasks}/.tix/connect-state.json. Других следов нет.
# HOME берётся из окружения. Нужны bash, jq; для apply ещё tix.
# shellcheck disable=SC2016  # $p, $v в программах jq - переменные jq, не shell
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
CONN="$ROOT/connectors"

SKILL_SRC="$CONN/agents/skills/tix"
CLAUDE_HOOKS_SRC="$CONN/claude/hooks.json"
CODEX_HOOKS_SRC="$CONN/codex/hooks.json"
CODEX_RULES_SRC="$CONN/codex/tix.rules"

CMD=plan
AGENT=all
NOW=""
FORCE=0
DRY=1
QUIET=0
BAKED=""
BAKLIST=""
CODEX_NEW=0
HOOK_EVENTS='["SessionStart","PostToolUse","UserPromptSubmit","Stop"]'

die() { printf 'err: %s\n' "$*" >&2; exit 1; }
warn() { printf 'warn: %s\n' "$*" >&2; }
act() { [ "$QUIET" = 1 ] || printf '%s\n' "$*"; }
want() { [ "$AGENT" = all ] || [ "$AGENT" = "$1" ]; }

usage() {
  printf 'usage: %s plan|apply|undo [--agent claude|codex|all] [--now ISO] [--force]\n' "$0" >&2
  exit 2
}

# ---------- разбор аргументов ----------
if [ $# -gt 0 ]; then
  case $1 in
    plan|apply|undo) CMD=$1; shift ;;
    -h|--help) usage ;;
  esac
fi
while [ $# -gt 0 ]; do
  case $1 in
    --agent)
      [ $# -ge 2 ] || usage
      AGENT=$2; shift 2 ;;
    --now)
      [ $# -ge 2 ] || usage
      NOW=$2; shift 2 ;;
    --force) FORCE=1; shift ;;
    *) usage ;;
  esac
done
case $AGENT in claude|codex|all) ;; *) usage ;; esac

[ -n "${HOME:-}" ] && [ -d "$HOME" ] || die "HOME не задан или не каталог"
command -v jq >/dev/null 2>&1 || die "нет jq в PATH"
if [ -n "${CLAUDE_CONFIG_DIR:-}" ] && [ "$CLAUDE_CONFIG_DIR" != "$HOME/.claude" ]; then
  die "CLAUDE_CONFIG_DIR=$CLAUDE_CONFIG_DIR отличается от \$HOME/.claude: скрипт пишет в \$HOME/.claude"
fi

if [ -z "$NOW" ]; then NOW=$(date -u +%Y%m%dT%H%M%SZ); fi
TAG=$(printf '%s' "$NOW" | tr -c 'A-Za-z0-9._-' '-')

MAN="${TIX_HOME:-$HOME/tasks}/.tix/connect-state.json"
SETTINGS="$HOME/.claude/settings.json"
CLAUDE_SKILL="$HOME/.claude/skills/tix"
CODEX_HOOKS="$HOME/.codex/hooks.json"
CODEX_RULES="$HOME/.codex/rules/tix.rules"
CODEX_SKILL="$HOME/.agents/skills/tix"

# Прежняя установка под именем tk (миграция): манифест в .tracker, скиллы и правило с именем tk.
LEGACY_MAN="$(dirname "$(dirname "$MAN")")/.tracker/connect-state.json"
LEGACY_CLAUDE_SKILL="$HOME/.claude/skills/tk"
LEGACY_CODEX_RULES="$HOME/.codex/rules/tk.rules"
LEGACY_CODEX_SKILL="$HOME/.agents/skills/tk"

on_exit() {
  local rc=$? b
  if [ "$rc" -ne 0 ] && [ -n "$BAKLIST" ]; then
    printf 'warn: apply не закончен; бэкапы оставлены (undo их не трогает):\n' >&2
    while IFS= read -r b; do printf '  %s\n' "$b" >&2; done <<<"$BAKLIST"
  fi
}
trap on_exit EXIT

# ---------- файлы ----------

# Путь без симлинков (в том числе у последнего звена); цель может не существовать.
resolve() {
  local p=$1 t head tail n=0
  case $p in /*) ;; *) p=$PWD/$p ;; esac
  while [ -L "$p" ]; do
    n=$((n + 1))
    [ "$n" -le 40 ] || die "цикл симлинков: $1"
    t=$(readlink "$p")
    case $t in /*) p=$t ;; *) p=$(dirname "$p")/$t ;; esac
  done
  head=$(dirname "$p")
  tail=$(basename "$p")
  while [ ! -d "$head" ]; do
    tail=$(basename "$head")/$tail
    head=$(dirname "$head")
  done
  head=$(cd "$head" && pwd -P)
  [ "$head" = / ] && head=""
  printf '%s/%s\n' "$head" "$tail"
}

fmode() { stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1"; }

# Каталоги над файлом, которых ещё нет: JSON-массив, самый глубокий первым.
missing_dirs() {
  local d out='[]'
  d=$(dirname "$1")
  while [ ! -d "$d" ]; do
    out=$(jq -c --arg d "$d" '. + [$d]' <<<"$out")
    d=$(dirname "$d")
  done
  printf '%s' "$out"
}

# Запись stdin в $1 (уже разрешённый путь): временный файл рядом, права цели, rename.
write_atomic() {
  local t=$1 dir tmp mode
  dir=$(dirname "$t")
  mkdir -p "$dir"
  tmp=$(mktemp "$dir/.tix-tmp.XXXXXX")
  if [ -e "$t" ]; then
    mode=$(fmode "$t")
  else
    mode=$(printf '%03o' $((0666 & ~$(umask))))
  fi
  if ! { cat >"$tmp" && chmod "$mode" "$tmp" && mv -f "$tmp" "$t"; }; then
    rm -f "$tmp"
    die "не удалось записать $t"
  fi
}

# ---------- манифест ----------

man_json() {
  if [ -e "$MAN" ]; then cat "$MAN"; else printf '{"version":1,"entries":[]}'; fi
}
man_check() {
  [ -e "$MAN" ] || return 0
  jq -e '.version==1 and (.entries|type=="array")' "$MAN" >/dev/null 2>&1 \
    || die "манифест повреждён или другой версии: $MAN"
}
man_has() { man_json | jq -e --arg k "$1" 'any(.entries[]; .key==$k)' >/dev/null; }
man_add() { # $1 = JSON записи с полем key; повтор того же key не пишется
  local k real new md
  k=$(jq -r .key <<<"$1")
  if man_has "$k"; then return 0; fi
  real=$(resolve "$MAN")
  if [ -e "$MAN" ]; then
    new=$(man_json | jq --argjson e "$1" '.entries += [$e]')
  else
    md=$(missing_dirs "$real")
    new=$(jq -n --argjson e "$1" --argjson md "$md" '{version:1, entries:[$e], dirs_created:$md}')
  fi
  printf '%s\n' "$new" | write_atomic "$real"
}
man_drop() {
  man_json | jq --arg k "$1" '.entries |= map(select(.key != $k))' | write_atomic "$(resolve "$MAN")"
}

# Бэкап перед первой правкой файла за запуск; удаляется после успешного apply.
backup_once() { # $1 = разрешённый путь
  local b n=1
  [ -e "$1" ] || return 0
  case $BAKED in *"|$1|"*) return 0 ;; esac
  b="$1.tix-bak-$TAG"
  while [ -e "$b" ]; do n=$((n + 1)); b="$1.tix-bak-$TAG.$n"; done
  cp -p "$1" "$b" || die "не удалось сделать бэкап $1"
  BAKED="$BAKED|$1|"
  BAKLIST="$BAKLIST${BAKLIST:+$'\n'}$b"
}
drop_backups() {
  local b
  while IFS= read -r b; do
    [ -n "$b" ] && rm -f -- "$b"
  done <<<"$BAKLIST"
  BAKLIST=""
}

# ---------- JSON-правки ----------

json_load() { # $1 = разрешённый путь; печатает содержимое или {}
  if [ -e "$1" ]; then
    jq -e 'type=="object"' "$1" >/dev/null 2>&1 || die "битый JSON или не объект: $1"
    cat "$1"
  else
    printf '{}'
  fi
}

# Программы jq: статус цели, добавление, снятие. Параметры: $p путь, $v значение.
ST_ARR='(try (getpath($p)|type) catch "bad") as $t
  | if $t=="null" then "add"
    elif $t!="array" then "err: \($p|join(".")) не массив"
    elif (getpath($p)|index([$v])) != null then "ok"
    else "add" end'
# Прежние команды хука под именем tk: хук tk ... заменяется хуком tix ... на месте.
LEGACY='def legacy($w): [$w | sub("^tix "; "tk ")] + (if $w == "tix ctx --hook" then ["tk ctx"] else [] end);'
ST_HOOK="$LEGACY"'(try (getpath($p)|type) catch "bad") as $t
  | ($v.hooks[0].command) as $want
  | if $t=="null" then "add"
    elif $t!="array" then "err: \($p|join(".")) не массив"
    elif any(getpath($p)[]?; any((.hooks // [])[]?; .command == $want)) then "ok"
    elif any(getpath($p)[]?; any((.hooks // [])[]?; (.command? // "") as $c | any(legacy($want)[]; . == $c))) then "upd"
    else "add" end'
FROM_HOOK="$LEGACY"'($v.hooks[0].command) as $want
  | [getpath($p)[]? | (.hooks // [])[]? | (.command? // empty) | select(. as $c | any(legacy($want)[]; . == $c))] | .[0] // ""'
AD_ARR='setpath($p; (getpath($p) // []) + [$v])'
RM_ARR='if (try (getpath($p)|type) catch "x") == "array" then setpath($p; getpath($p) | map(select(. != $v))) else . end'
RM_HOOK='if (try (getpath($p)|type) catch "x") == "array" then
    setpath($p; getpath($p) | map(select((type == "object" and ((.hooks // []) | length) == 1 and ((.hooks[0].command) as $c | ["tk ctx", "tk ctx --hook", "tk inbox --hook", "tix ctx", "tix ctx --hook", "tix inbox --hook"] | any(. == $c))) | not)))
  else . end'
# Хук tk меняется на tix (UP_HOOK); снятие возвращает записанную в манифесте команду from (DN_HOOK).
UP_HOOK="$LEGACY"'($v.hooks[0].command) as $want | setpath($p; getpath($p) | map(if (.hooks | type) == "array"
    then .hooks |= map(if type == "object" and ((.command? // "") as $c | any(legacy($want)[]; . == $c)) then .command = $want else . end) else . end))'
DN_HOOK='($v.hooks[0].command) as $now | (if $from != "" then $from else ($now | sub("^tix "; "tk ")) end) as $back
  | if (try (getpath($p)|type) catch "x") == "array" then
    setpath($p; getpath($p) | map(if (.hooks | type) == "array"
      then .hooks |= map(if type == "object" and .command == $now then .command = $back else . end) else . end))
  else . end'
CREATED='. as $d | [range(1; ($p|length) + 1) as $i | $p[:$i] as $q | select(($d | getpath($q)) == null) | $q]'
PRUNE='reduce ($c | reverse)[] as $q (.; (try getpath($q) catch null) as $x
  | if $x == [] or $x == {} then delpaths([$q]) else . end)'

# jedit_apply KEY_PREFIX FILE PATH_JSON VALUE_JSON STATUS_JQ ADD_JQ OP LABEL [FILE_CREATED]
jedit_apply() {
  local pre=$1 file=$2 p=$3 v=$4 st=$5 ad=$6 op=$7 label=$8 fcreated=${9:-}
  local real doc status created entry new key ag from=""
  case $pre in codex-hook*) ag=codex ;; *) ag=claude ;; esac
  real=$(resolve "$file")
  doc=$(json_load "$real")
  status=$(jq -r --argjson p "$p" --argjson v "$v" "$st" <<<"$doc") || die "не разобрать $real"
  case $status in
    ok) return 0 ;;
    err*) die "${status#err: } ($real); запись не сделана" ;;
    upd)
      from=$(jq -r --argjson p "$p" --argjson v "$v" "$FROM_HOOK" <<<"$doc")
      pre=${pre}-up; ad=$UP_HOOK; op=hookup; label="$ag: $(jq -r 'join(".")' <<<"$p"): $from -> $(jq -r '.hooks[0].command' <<<"$v")" ;;
  esac
  act "$label: $real"
  [ "$ag" = codex ] && CODEX_NEW=1
  [ "$DRY" = 1 ] && return 0
  if [ -z "$fcreated" ]; then
    if [ -e "$real" ]; then fcreated=false; else fcreated=true; fi
  fi
  created=$(jq -c --argjson p "$p" "$CREATED" <<<"$doc")
  key="$pre|$real|$(jq -c . <<<"$v")"
  entry=$(jq -nc --arg key "$key" --arg ag "$ag" --arg file "$real" --arg op "$op" --argjson p "$p" --argjson v "$v" \
    --argjson c "$created" --argjson fc "$fcreated" --argjson md "$(missing_dirs "$real")" --arg from "$from" \
    '{key:$key, agent:$ag, kind:"json", op:$op, file:$file, path:$p, value:$v, created:$c, file_created:$fc, dirs_created:$md}
     + (if $from != "" then {from:$from} else {} end)')
  new=$(jq --argjson p "$p" --argjson v "$v" "$ad" <<<"$doc") || die "не удалось собрать JSON для $real"
  man_add "$entry"
  backup_once "$real"
  printf '%s\n' "$new" | write_atomic "$real"
}

# ---------- файлы и каталоги ----------

# put_file KEY REAL SRC LABEL AGENT
put_file() {
  local key=$1 real=$2 src=$3 label=$4 ag=$5 exists=0
  [ -e "$real" ] || [ -L "$real" ] && exists=1
  if [ "$exists" = 1 ]; then
    cmp -s "$src" "$real" && return 0
    if [ "$FORCE" = 0 ]; then
      [ "$QUIET" = 1 ] || warn "$label: $real отличается от коннектора; пропуск (перезапись: --force)"
      return 0
    fi
    act "$label: перезаписать $real"
  else
    act "$label: создать $real"
  fi
  [ "$DRY" = 1 ] && return 0
  man_add "$(jq -nc --arg key "$key" --arg ag "$ag" --arg p "$real" --argjson md "$(missing_dirs "$real")" \
    '{key:$key, agent:$ag, kind:"file", path:$p, dirs_created:$md}')"
  [ "$exists" = 1 ] && backup_once "$real"
  write_atomic "$real" <"$src"
}

put_skill() { # $1 = каталог скилла у агента, $2 = агент
  local src=$SKILL_SRC real key tmp exists=0 files
  real=$(resolve "$1")
  key="dir|$real"
  [ -e "$real" ] && exists=1
  if [ "$exists" = 1 ]; then
    [ -d "$real" ] || die "$real не каталог; запись не сделана"
    diff -r -q "$src" "$real" >/dev/null 2>&1 && return 0
    if [ "$FORCE" = 0 ]; then
      [ "$QUIET" = 1 ] || warn "skill: $real отличается от скилла коннектора; пропуск (перезапись: --force)"
      return 0
    fi
    act "skill: перезаписать $real"
  else
    act "skill: создать $real"
  fi
  [ "$DRY" = 1 ] && return 0
  files=$(cd "$src" && find . -type f | jq -Rsc 'split("\n") | map(select(length > 0) | ltrimstr("./"))')
  man_add "$(jq -nc --arg key "$key" --arg ag "$2" --arg p "$real" --argjson f "$files" --argjson md "$(missing_dirs "$real")" \
    '{key:$key, agent:$ag, kind:"dir", path:$p, files:$f, dirs_created:$md}')"
  if [ "$exists" = 1 ]; then
    cp -R "$src/." "$real/" || die "не удалось скопировать скилл в $real"
    return 0
  fi
  mkdir -p "$(dirname "$real")"
  tmp=$(mktemp -d "$(dirname "$real")/.tix-tmp.XXXXXX")
  cp -R "$src/." "$tmp/" || { rm -r "$tmp"; die "не удалось скопировать скилл"; }
  mv "$tmp" "$real"
  chmod 755 "$real"
}

# ---------- проверка исходников и инструментов ----------

check_sources() {
  [ -f "$SKILL_SRC/SKILL.md" ] || die "нет скилла: $SKILL_SRC/SKILL.md"
  if want claude; then
    [ -f "$CLAUDE_HOOKS_SRC" ] || die "нет $CLAUDE_HOOKS_SRC"
  fi
  if want codex; then
    [ -f "$CODEX_HOOKS_SRC" ] || die "нет $CODEX_HOOKS_SRC"
    [ -f "$CODEX_RULES_SRC" ] || die "нет $CODEX_RULES_SRC"
  fi
}

check_tools() {
  if [ "$CMD" = apply ]; then
    command -v tix >/dev/null 2>&1 || die "нет tix в PATH"
  else
    command -v tix >/dev/null 2>&1 || warn "нет tix в PATH: apply не пройдёт"
  fi
}

# ---------- миграция установки под именем tk ----------

# Манифест из .tracker переезжает в .tix. Служебная папка базы переименовывается так же, как
# в ядре (если .tix нет), и в .gitignore базы .tracker/ меняется на .tix/. plan только читает.
migrate_manifest() {
  local base g
  [ ! -e "$MAN" ] && [ -f "$LEGACY_MAN" ] || return 0
  if [ "$CMD" = plan ]; then MAN=$LEGACY_MAN; return 0; fi
  base=$(dirname "$(dirname "$MAN")")
  if [ ! -e "$base/.tix" ]; then
    mv "$base/.tracker" "$base/.tix" || die "не удалось переименовать $base/.tracker"
    g=$base/.gitignore
    if [ -f "$g" ] && grep -q '^\.tracker/' "$g"; then
      sed 's#^\.tracker/#.tix/#' "$g" | write_atomic "$(resolve "$g")"
    fi
  else
    mv -f "$LEGACY_MAN" "$MAN" || die "не удалось перенести манифест в $MAN"
  fi
  jq '(.. | strings) |= (sub("/\\.tracker$"; "/.tix") | sub("/\\.tracker/"; "/.tix/"))' "$MAN" | write_atomic "$(resolve "$MAN")"
}

# Разрешение Bash(tk *), поставленное прежним apply, становится Bash(tix *) на месте: запись
# манифеста сохраняет created и file_created. Чужой Bash(tk *) не трогается.
migrate_allow() {
  local real old_key new_key new
  real=$(resolve "$SETTINGS")
  old_key="allow|$real|\"Bash(tk *)\""
  new_key="allow|$real|\"Bash(tix *)\""
  man_has "$old_key" || return 0
  act "claude: permissions.allow Bash(tk *) -> Bash(tix *): $real"
  [ "$DRY" = 1 ] && return 0
  new=$(json_load "$real" | jq 'if (.permissions.allow | type) == "array" then .permissions.allow |= (
      if index("Bash(tix *)") != null then map(select(. != "Bash(tk *)"))
      else map(if . == "Bash(tk *)" then "Bash(tix *)" else . end) end) else . end') || die "не разобрать $real"
  backup_once "$real"
  printf '%s\n' "$new" | write_atomic "$real"
  man_json | jq --arg o "$old_key" --arg n "$new_key" \
    '.entries |= map(if .key == $o then .key = $n | .value = "Bash(tix *)" else . end)' | write_atomic "$(resolve "$MAN")"
}

# Скилл или правило tk, поставленное прежним apply, снимается по записи манифеста; созданные
# им каталоги переходят к записи нового файла. Каталог tk без записи манифеста - чужой.
# drop_legacy KIND(dir|file) СТАРЫЙ_ПУТЬ НОВЫЙ_ПУТЬ
drop_legacy() {
  local kind=$1 old new key e md p
  old=$(resolve "$2"); new=$(resolve "$3")
  key="$kind|$old"
  man_has "$key" || return 0
  act "$kind: снять прежний $old"
  [ "$DRY" = 1 ] && return 0
  e=$(man_json | jq -c --arg k "$key" '.entries[] | select(.key == $k)')
  if [ "$kind" = dir ]; then
    if [ -d "$old" ]; then
      while IFS= read -r p; do
        case $p in ''|/*|*..*) continue ;; esac
        rm -f -- "$old/$p"
      done < <(jq -r '.files // [] | .[]' <<<"$e")
      find "$old" -depth -type d -empty -exec rmdir {} + 2>/dev/null || true
      [ ! -d "$old" ] || warn "в $old остались чужие файлы; каталог не удалён"
    fi
  else
    rm -f -- "$old"
  fi
  md=$(jq -c '.dirs_created // []' <<<"$e")
  man_drop "$key"
  man_json | jq --arg k "$kind|$new" --argjson a "$md" \
    '.entries |= map(if .key == $k then .dirs_created = ((.dirs_created // []) + $a) else . end)' | write_atomic "$(resolve "$MAN")"
}

# ---------- агенты ----------

# put_hooks AGENT SRC FILE: по группе на каждое событие источника. Ключ манифеста SessionStart
# прежний (claude-hook), у остальных событий с именем события.
put_hooks() {
  local ag=$1 src=$2 file=$3 ev group pre cmd
  while IFS= read -r ev; do
    group=$(jq -ce --arg e "$ev" '.hooks[$e][0]' "$src") || die "нет группы $ev в $src"
    cmd=$(jq -r '.hooks[0].command' <<<"$group")
    pre="$ag-hook"
    [ "$ev" = SessionStart ] || pre="$ag-hook-$ev"
    jedit_apply "$pre" "$file" "$(jq -nc --arg e "$ev" '["hooks",$e]')" "$group" "$ST_HOOK" "$AD_ARR" hook \
      "$ag: hooks.$ev += $cmd"
  done < <(jq -r --argjson ok "$HOOK_EVENTS" '.hooks | keys_unsorted[] | select(. as $e | $ok | index($e))' "$src")
}

run_claude() {
  migrate_allow
  put_hooks claude "$CLAUDE_HOOKS_SRC" "$SETTINGS"
  jedit_apply "allow" "$SETTINGS" '["permissions","allow"]' '"Bash(tix *)"' "$ST_ARR" "$AD_ARR" arr \
    "claude: permissions.allow += Bash(tix *)"
  put_skill "$CLAUDE_SKILL" claude
  drop_legacy dir "$LEGACY_CLAUDE_SKILL" "$CLAUDE_SKILL"
}

run_codex() {
  put_hooks codex "$CODEX_HOOKS_SRC" "$CODEX_HOOKS"
  put_file "file|$(resolve "$CODEX_RULES")" "$(resolve "$CODEX_RULES")" "$CODEX_RULES_SRC" "codex: правило" codex
  drop_legacy file "$LEGACY_CODEX_RULES" "$CODEX_RULES"
  put_skill "$CODEX_SKILL" codex
  drop_legacy dir "$LEGACY_CODEX_SKILL" "$CODEX_SKILL"
}

run_agents() {
  man_check
  want claude && run_claude
  want codex && run_codex
  return 0
}

# ---------- откат ----------

# Белый список целей undo: манифест - данные, пути из него не доверяем.
dir_ok() { # каталог внутри HOME или корня задач, без ..
  local r roots d
  case $1 in /*) ;; *) return 1 ;; esac
  case $1 in */../*|*/..) return 1 ;; esac
  r=$(resolve "$1")
  roots="$(resolve "$HOME")/ $(resolve "$(dirname "$(dirname "$MAN")")")/"
  for d in $roots; do
    case $r in "$d"*) return 0 ;; esac
  done
  return 1
}
target_ok() { # $1 = kind, $2 = путь из манифеста
  local r
  case $2 in /*) ;; *) return 1 ;; esac
  case $2 in */../*|*/..) return 1 ;; esac
  r=$(resolve "$2")
  case $1 in
    json) [ "$r" = "$(resolve "$SETTINGS")" ] || [ "$r" = "$(resolve "$CODEX_HOOKS")" ] ;;
    file) [ "$r" = "$(resolve "$CODEX_RULES")" ] ;;
    dir) [ "$r" = "$(resolve "$CLAUDE_SKILL")" ] || [ "$r" = "$(resolve "$CODEX_SKILL")" ] ;;
    *) return 1 ;;
  esac
}

rmdirs() { # $1 = JSON-массив каталогов, самый глубокий первым
  local d
  while IFS= read -r d; do
    [ -n "$d" ] || continue
    if dir_ok "$d"; then rmdir "$d" 2>/dev/null || true; else warn "undo: каталог вне белого списка, пропуск: $d"; fi
  done < <(jq -r '.[]' <<<"$1")
}

undo_json_file() { # $1 file $2 новый JSON ($3 file_created)
  local real=$1 new=$2 fc=$3
  if [ "$fc" = true ] && jq -e '. == {}' >/dev/null <<<"$new"; then
    rm -f "$real"
  elif ! diff -q <(jq -S . "$real") <(jq -S . <<<"$new") >/dev/null; then
    printf '%s\n' "$new" | write_atomic "$real"
  fi
}

undo_entry() { # $1 = JSON записи; 0 - снято, 1 - оставлено
  local e=$1 kind key f p v op c fc md doc new rm_prog
  kind=$(jq -r .kind <<<"$e")
  key=$(jq -r .key <<<"$e")
  md=$(jq -c '.dirs_created // []' <<<"$e")
  case $kind in
    json) f=$(jq -r '.file // ""' <<<"$e") ;;
    file|dir) f=$(jq -r '.path // ""' <<<"$e") ;;
    *) f="" ;;
  esac
  if ! target_ok "$kind" "$f"; then
    warn "undo: путь вне белого списка, пропуск: ${f:-<пусто>} (kind=$kind)"
    return 1
  fi
  case $kind in
    json)
      f=$(jq -r .file <<<"$e"); p=$(jq -c .path <<<"$e"); v=$(jq -c .value <<<"$e")
      jq -e --argjson ok "$HOOK_EVENTS" '(.path | length == 2 and .[0] == "hooks" and (.[1] as $x | $ok | index($x) != null)) or .path == ["permissions","allow"]' <<<"$e" >/dev/null \
        || { warn "undo: ключ вне белого списка, пропуск: $p в $f"; return 1; }
      op=$(jq -r .op <<<"$e"); c=$(jq -c .created <<<"$e"); fc=$(jq -r .file_created <<<"$e")
      case $op in arr) rm_prog=$RM_ARR ;; hook) rm_prog=$RM_HOOK ;; hookup) rm_prog=$DN_HOOK ;; *) die "манифест: неизвестный op $op" ;; esac
      if [ -e "$f" ]; then
        doc=$(json_load "$f")
        new=$(jq --argjson p "$p" --argjson v "$v" --argjson c "$c" --arg from "$(jq -r '.from // ""' <<<"$e")" "$rm_prog | $PRUNE" <<<"$doc") \
          || die "не снять запись из $f"
        act "undo: $f"
        undo_json_file "$f" "$new" "$fc"
      fi
      rmdirs "$md"
      ;;
    file)
      f=$(jq -r .path <<<"$e")
      if [ -e "$f" ]; then act "undo: удалить $f"; rm -f "$f"; fi
      rmdirs "$md"
      ;;
    dir)
      f=$(jq -r .path <<<"$e")
      if [ -d "$f" ]; then
        act "undo: удалить файлы скилла в $f"
        while IFS= read -r p; do
          case $p in ''|/*|*..*) continue ;; esac
          rm -f -- "$f/$p"
        done < <(jq -r '.files // [] | .[]' <<<"$e")
        find "$f" -depth -type d -empty -exec rmdir {} + 2>/dev/null || true
        [ ! -d "$f" ] || warn "в $f остались чужие файлы; каталог не удалён"
      fi
      rmdirs "$md"
      ;;
    *) die "манифест: неизвестный kind $kind" ;;
  esac
  man_drop "$key"
}

run_undo() {
  local e ag left=0 real md
  man_check
  if [ ! -e "$MAN" ]; then
    printf 'nothing to undo: no manifest at %s\n' "$MAN"
    return 0
  fi
  while IFS= read -r e; do
    ag=$(jq -r .agent <<<"$e")
    if [ "$AGENT" != all ] && [ "$ag" != "$AGENT" ]; then left=1; continue; fi
    undo_entry "$e" || left=1
  done < <(man_json | jq -c '.entries | reverse | .[]')
  if [ "$(man_json | jq '.entries | length')" = 0 ]; then
    act "undo: удалить манифест $MAN"
    real=$(resolve "$MAN")
    md=$(man_json | jq -c '.dirs_created // []')
    rm -f "$real"
    rmdirs "$md"
  fi
  [ "$left" = 0 ] || return 1
}

# ---------- main ----------

migrate_manifest
case $CMD in
  plan)
    check_tools
    check_sources
    DRY=1
    run_agents
    ;;
  apply)
    check_tools
    check_sources
    DRY=1 QUIET=1
    run_agents
    DRY=0 QUIET=0
    run_agents
    drop_backups
    [ "$CODEX_NEW" = 0 ] || act "codex: новые хуки могут потребовать доверия: откройте /hooks в Codex и подтвердите tix inbox --hook"
    ;;
  undo)
    DRY=0
    run_undo
    ;;
esac
