#!/usr/bin/env bash
# Установка коннекторов агентов (Claude Code, Codex) для tk.
#
#   connect.sh plan|apply|undo [--agent claude|codex|all] [--now ISO] [--force]
#
# plan (по умолчанию) печатает, что изменит apply, и ничего не пишет; повторный plan пуст.
# apply добавляет недостающее и записывает добавленное в манифест; чужое не трогает.
# Бэкапы <файл>.tk-bak-<время> живут только на время apply; при сбое остаются и называются.
# Наш файл или скилл уже есть и отличается от источника: warn и пропуск; перезапись только с --force.
# undo снимает только записи манифеста (файлы скилла по списку) и сам манифест.
# Манифест: ${TRACKER_HOME:-$HOME/tasks}/.tracker/connect-state.json. Других следов нет.
# HOME берётся из окружения. Нужны bash, jq; для apply ещё tk.
# shellcheck disable=SC2016  # $p, $v в программах jq - переменные jq, не shell
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
CONN="$ROOT/connectors"

SKILL_SRC="$CONN/agents/skills/tk"
CLAUDE_HOOKS_SRC="$CONN/claude/hooks.json"
CODEX_HOOKS_SRC="$CONN/codex/hooks.json"
CODEX_RULES_SRC="$CONN/codex/tk.rules"

CMD=plan
AGENT=all
NOW=""
FORCE=0
DRY=1
QUIET=0
BAKED=""
BAKLIST=""

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

MAN="${TRACKER_HOME:-$HOME/tasks}/.tracker/connect-state.json"
SETTINGS="$HOME/.claude/settings.json"
CLAUDE_SKILL="$HOME/.claude/skills/tk"
CODEX_HOOKS="$HOME/.codex/hooks.json"
CODEX_RULES="$HOME/.codex/rules/tk.rules"
CODEX_SKILL="$HOME/.agents/skills/tk"

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
  tmp=$(mktemp "$dir/.tk-tmp.XXXXXX")
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
  b="$1.tk-bak-$TAG"
  while [ -e "$b" ]; do n=$((n + 1)); b="$1.tk-bak-$TAG.$n"; done
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
ST_HOOK='(try (getpath($p)|type) catch "bad") as $t
  | if $t=="null" then "add"
    elif $t!="array" then "err: \($p|join(".")) не массив"
    elif any(getpath($p)[]?; any((.hooks // [])[]?; .command == "tk ctx")) then "ok"
    else "add" end'
AD_ARR='setpath($p; (getpath($p) // []) + [$v])'
RM_ARR='if (try (getpath($p)|type) catch "x") == "array" then setpath($p; getpath($p) | map(select(. != $v))) else . end'
RM_HOOK='if (try (getpath($p)|type) catch "x") == "array" then
    setpath($p; getpath($p) | map(select((type == "object" and ((.hooks // []) | length) == 1 and .hooks[0].command == "tk ctx") | not)))
  else . end'
CREATED='. as $d | [range(1; ($p|length) + 1) as $i | $p[:$i] as $q | select(($d | getpath($q)) == null) | $q]'
PRUNE='reduce ($c | reverse)[] as $q (.; (try getpath($q) catch null) as $x
  | if $x == [] or $x == {} then delpaths([$q]) else . end)'

# jedit_apply KEY_PREFIX FILE PATH_JSON VALUE_JSON STATUS_JQ ADD_JQ OP LABEL [FILE_CREATED]
jedit_apply() {
  local pre=$1 file=$2 p=$3 v=$4 st=$5 ad=$6 op=$7 label=$8 fcreated=${9:-}
  local real doc status created entry new key ag
  case $pre in codex-hook) ag=codex ;; *) ag=claude ;; esac
  real=$(resolve "$file")
  doc=$(json_load "$real")
  status=$(jq -r --argjson p "$p" --argjson v "$v" "$st" <<<"$doc") || die "не разобрать $real"
  case $status in
    ok) return 0 ;;
    err*) die "${status#err: } ($real); запись не сделана" ;;
  esac
  act "$label: $real"
  [ "$DRY" = 1 ] && return 0
  if [ -z "$fcreated" ]; then
    if [ -e "$real" ]; then fcreated=false; else fcreated=true; fi
  fi
  created=$(jq -c --argjson p "$p" "$CREATED" <<<"$doc")
  key="$pre|$real|$(jq -c . <<<"$v")"
  entry=$(jq -nc --arg key "$key" --arg ag "$ag" --arg file "$real" --arg op "$op" --argjson p "$p" --argjson v "$v" \
    --argjson c "$created" --argjson fc "$fcreated" --argjson md "$(missing_dirs "$real")" \
    '{key:$key, agent:$ag, kind:"json", op:$op, file:$file, path:$p, value:$v, created:$c, file_created:$fc, dirs_created:$md}')
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
  tmp=$(mktemp -d "$(dirname "$real")/.tk-tmp.XXXXXX")
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
    command -v tk >/dev/null 2>&1 || die "нет tk в PATH"
  else
    command -v tk >/dev/null 2>&1 || warn "нет tk в PATH: apply не пройдёт"
  fi
}

# ---------- агенты ----------

run_claude() {
  local group
  group=$(jq -ce '.hooks.SessionStart[0]' "$CLAUDE_HOOKS_SRC") || die "нет группы SessionStart в $CLAUDE_HOOKS_SRC"
  jedit_apply "claude-hook" "$SETTINGS" '["hooks","SessionStart"]' "$group" "$ST_HOOK" "$AD_ARR" hook \
    "claude: hooks.SessionStart += tk ctx"
  jedit_apply "allow" "$SETTINGS" '["permissions","allow"]' '"Bash(tk *)"' "$ST_ARR" "$AD_ARR" arr \
    "claude: permissions.allow += Bash(tk *)"
  put_skill "$CLAUDE_SKILL" claude
}

run_codex() {
  local group
  group=$(jq -ce '.hooks.SessionStart[0]' "$CODEX_HOOKS_SRC") || die "нет группы SessionStart в $CODEX_HOOKS_SRC"
  jedit_apply "codex-hook" "$CODEX_HOOKS" '["hooks","SessionStart"]' "$group" "$ST_HOOK" "$AD_ARR" hook \
    "codex: hooks.SessionStart += tk ctx"
  put_file "file|$(resolve "$CODEX_RULES")" "$(resolve "$CODEX_RULES")" "$CODEX_RULES_SRC" "codex: правило" codex
  put_skill "$CODEX_SKILL" codex
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
      jq -e '.path == ["hooks","SessionStart"] or .path == ["permissions","allow"]' <<<"$e" >/dev/null \
        || { warn "undo: ключ вне белого списка, пропуск: $p в $f"; return 1; }
      op=$(jq -r .op <<<"$e"); c=$(jq -c .created <<<"$e"); fc=$(jq -r .file_created <<<"$e")
      case $op in arr) rm_prog=$RM_ARR ;; hook) rm_prog=$RM_HOOK ;; *) die "манифест: неизвестный op $op" ;; esac
      if [ -e "$f" ]; then
        doc=$(json_load "$f")
        new=$(jq --argjson p "$p" --argjson v "$v" --argjson c "$c" "$rm_prog | $PRUNE" <<<"$doc") \
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
    ;;
  undo)
    DRY=0
    run_undo
    ;;
esac
