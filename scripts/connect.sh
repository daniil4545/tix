#!/usr/bin/env bash
# Установка коннекторов агентов (Claude Code, Codex, OpenCode) для tk.
#
#   connect.sh plan|apply|undo [--agent claude|codex|opencode|all] [--now ISO]
#
# plan (по умолчанию) печатает, что изменит apply, и ничего не пишет; повторный plan пуст.
# apply добавляет недостающее и записывает добавленное в манифест; чужое не трогает.
# undo снимает только записи манифеста, затем свои бэкапы и сам манифест.
# HOME берётся из окружения. Нужны bash, jq; для apply ещё tk и claude (если агент claude).
# shellcheck disable=SC2016  # $p, $v в программах jq - переменные jq, не shell
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
CONN="$ROOT/connectors"

MKT_NAME=tk-local
PLUGIN_ID="tk@$MKT_NAME"
MKT_DIR="$CONN/claude"
OC_PLUGIN_SRC="$CONN/opencode/plugins/tk.js"
CODEX_HOOKS_SRC="$CONN/codex/hooks.json"
CODEX_RULES_SRC="$CONN/codex/tk.rules"

CMD=plan
AGENT=all
NOW=""
DRY=1
QUIET=0
BAKED=""
CLAUDE_UNDONE=0

die() { printf 'err: %s\n' "$*" >&2; exit 1; }
warn() { printf 'warn: %s\n' "$*" >&2; }
act() { [ "$QUIET" = 1 ] || printf '%s\n' "$*"; }
want() { [ "$AGENT" = all ] || [ "$AGENT" = "$1" ]; }

usage() {
  printf 'usage: %s plan|apply|undo [--agent claude|codex|opencode|all] [--now ISO]\n' "$0" >&2
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
    *) usage ;;
  esac
done
case $AGENT in claude|codex|opencode|all) ;; *) usage ;; esac

[ -n "${HOME:-}" ] && [ -d "$HOME" ] || die "HOME не задан или не каталог"
command -v jq >/dev/null 2>&1 || die "нет jq в PATH"
if [ -n "${CLAUDE_CONFIG_DIR:-}" ] && [ "$CLAUDE_CONFIG_DIR" != "$HOME/.claude" ]; then
  die "CLAUDE_CONFIG_DIR=$CLAUDE_CONFIG_DIR отличается от \$HOME/.claude: скрипт пишет в \$HOME/.claude"
fi

if [ -z "$NOW" ]; then NOW=$(date -u +%Y%m%dT%H%M%SZ); fi
TAG=$(printf '%s' "$NOW" | tr -c 'A-Za-z0-9._-' '-')

MAN="$HOME/.config/tk/connect-state.json"
SETTINGS="$HOME/.claude/settings.json"
CODEX_HOOKS="$HOME/.codex/hooks.json"
CODEX_RULES="$HOME/.codex/rules/tk.rules"
SKILL_DST="$HOME/.agents/skills/tk"
OC_JSON="$HOME/.config/opencode/opencode.json"
OC_PLUGIN="$HOME/.config/opencode/plugins/tk.js"

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
  local k
  k=$(jq -r .key <<<"$1")
  if man_has "$k"; then return 0; fi
  man_json | jq --argjson e "$1" '.entries += [$e]' | write_atomic "$(resolve "$MAN")"
}
man_drop() {
  man_json | jq --arg k "$1" '.entries |= map(select(.key != $k))' | write_atomic "$(resolve "$MAN")"
}

# Бэкап перед первой правкой файла за запуск; запись в манифест.
backup_once() { # $1 = разрешённый путь
  local b n=1
  [ -e "$1" ] || return 0
  case $BAKED in *"|$1|"*) return 0 ;; esac
  b="$1.tk-bak-$TAG"
  while [ -e "$b" ]; do n=$((n + 1)); b="$1.tk-bak-$TAG.$n"; done
  man_add "$(jq -nc --arg p "$b" '{key:("backup|"+$p), agent:"shared", kind:"backup", path:$p}')"
  cp -p "$1" "$b" || die "не удалось сделать бэкап $1"
  BAKED="$BAKED|$1|"
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
ST_OC='def re: gsub("(?<c>[.+?^${}()|\\[\\]\\\\])"; "\\" + .c) | gsub("\\*"; ".*");
  (.permission) as $perm
  | if $perm == null then "add"
    elif ($perm|type) == "string" then (if $perm == "allow" then "ok" else "err: permission=\($perm)" end)
    elif ($perm|type) != "object" then "err: permission не объект"
    else $perm.bash as $b
      | if $b == null then (if $perm["*"] == "allow" then "ok" else "add" end)
        elif ($b|type) == "string" then (if $b == "allow" then "ok" else "err: permission.bash=\($b)" end)
        elif ($b|type) != "object" then "err: permission.bash не объект"
        else
          ([$b | to_entries[] | select(.key as $k | ("tk ctx" | test("^" + ($k|re) + "$")))] | last) as $m
          | if $m != null and $m.value == "allow" then "ok"
            elif ($b|has("tk *")) then "err: permission.bash[\"tk *\"]=\($b["tk *"]), последнее совпадение не allow"
            else "add" end
        end
    end'
AD_ARR='setpath($p; (getpath($p) // []) + [$v])'
AD_KEY='setpath($p; $v)'
RM_ARR='if (try (getpath($p)|type) catch "x") == "array" then setpath($p; getpath($p) | map(select(. != $v))) else . end'
RM_HOOK='if (try (getpath($p)|type) catch "x") == "array" then
    setpath($p; getpath($p) | map(select((type == "object" and ((.hooks // []) | length) == 1 and .hooks[0].command == "tk ctx") | not)))
  else . end'
RM_KEY='if (try getpath($p) catch null) == "allow" then delpaths([$p]) else . end'
CREATED='. as $d | [range(1; ($p|length) + 1) as $i | $p[:$i] as $q | select(($d | getpath($q)) == null) | $q]'
PRUNE='reduce ($c | reverse)[] as $q (.; (try getpath($q) catch null) as $x
  | if $x == [] or $x == {} then delpaths([$q]) else . end)'

# jedit_apply KEY_PREFIX FILE PATH_JSON VALUE_JSON STATUS_JQ ADD_JQ OP LABEL [FILE_CREATED]
jedit_apply() {
  local pre=$1 file=$2 p=$3 v=$4 st=$5 ad=$6 op=$7 label=$8 fcreated=${9:-}
  local real doc status created entry new key ag
  case $pre in allow) ag=claude ;; codex-hook) ag=codex ;; *) ag=opencode ;; esac
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
  local key=$1 real=$2 src=$3 label=$4 ag=$5 same=0 exists=0 tracked=0
  [ -e "$real" ] || [ -L "$real" ] && exists=1
  man_has "$key" && tracked=1
  if [ "$exists" = 1 ]; then
    cmp -s "$src" "$real" && same=1
    [ "$same" = 1 ] && return 0
    [ "$tracked" = 1 ] || die "$real уже есть и отличается от коннектора; запись не сделана"
    act "$label: обновить $real"
  else
    act "$label: создать $real"
  fi
  [ "$DRY" = 1 ] && return 0
  if [ "$exists" = 0 ]; then
    man_add "$(jq -nc --arg key "$key" --arg ag "$ag" --arg p "$real" --argjson md "$(missing_dirs "$real")" \
      '{key:$key, agent:$ag, kind:"file", path:$p, dirs_created:$md}')"
  fi
  write_atomic "$real" <"$src"
}

put_skill() {
  local src real key tmp exists=0 tracked=0
  src=$SKILL_SRC
  real=$(resolve "$SKILL_DST")
  key="dir|$real"
  [ -e "$real" ] && exists=1
  man_has "$key" && tracked=1
  if [ "$exists" = 1 ]; then
    [ -d "$real" ] || die "$real не каталог; запись не сделана"
    diff -r -q "$src" "$real" >/dev/null 2>&1 && return 0
    [ "$tracked" = 1 ] || die "$real уже есть и отличается от скилла коннектора; запись не сделана"
    act "skill: обновить $real"
  else
    act "skill: создать $real"
  fi
  [ "$DRY" = 1 ] && return 0
  if [ "$exists" = 0 ]; then
    man_add "$(jq -nc --arg key "$key" --arg p "$real" --argjson md "$(missing_dirs "$real")" \
      '{key:$key, agent:"shared", kind:"dir", path:$p, dirs_created:$md}')"
  fi
  mkdir -p "$(dirname "$real")"
  tmp=$(mktemp -d "$(dirname "$real")/.tk-tmp.XXXXXX")
  cp -R "$src/." "$tmp/" || { rm -r "$tmp"; die "не удалось скопировать скилл"; }
  [ "$exists" = 1 ] && rm -r "$real"
  mv "$tmp" "$real"
  chmod 755 "$real"
}

# ---------- проверка исходников и инструментов ----------

check_sources() {
  if want claude; then
    [ -f "$MKT_DIR/.claude-plugin/marketplace.json" ] \
      || die "нет $MKT_DIR/.claude-plugin/marketplace.json"
  fi
  if want codex; then
    [ -f "$CODEX_HOOKS_SRC" ] || die "нет $CODEX_HOOKS_SRC"
    [ -f "$CODEX_RULES_SRC" ] || die "нет $CODEX_RULES_SRC"
  fi
  if want codex || want opencode; then
    SKILL_SRC=""
    local c
    for c in "$CONN/agents/skills/tk" "$CONN/claude/plugins/tk/skills/tk"; do
      if [ -f "$c/SKILL.md" ]; then SKILL_SRC=$c; break; fi
    done
    [ -n "$SKILL_SRC" ] || die "нет скилла: $CONN/agents/skills/tk/SKILL.md"
  fi
}

check_tools() {
  if [ "$CMD" = apply ]; then
    command -v tk >/dev/null 2>&1 || die "нет tk в PATH"
    if want claude; then command -v claude >/dev/null 2>&1 || die "нет claude в PATH"; fi
  else
    command -v tk >/dev/null 2>&1 || warn "нет tk в PATH: apply не пройдёт"
    if want claude; then command -v claude >/dev/null 2>&1 || warn "нет claude в PATH: apply не пройдёт"; fi
  fi
}

# ---------- агенты ----------

run_claude() {
  local real doc ms ps pre_existing=true created
  real=$(resolve "$SETTINGS")
  doc=$(json_load "$real")
  [ -e "$real" ] || pre_existing=false
  ms=$(jq -r --arg d "$MKT_DIR" --arg n "$MKT_NAME" '
    (try .extraKnownMarketplaces[$n] catch null) as $m
    | if $m == null then "add"
      elif ($m.source.path // "") == $d then "ok"
      else "err: маркетплейс \($n) уже указывает на \($m.source.path // "другой источник")" end' <<<"$doc") \
    || die "не разобрать $real"
  case $ms in err*) die "${ms#err: } ($real); запись не сделана" ;; esac
  ps=ok
  jq -e --arg id "$PLUGIN_ID" '(try .enabledPlugins[$id] catch null) != null' <<<"$doc" >/dev/null || ps=add
  created=$(jq -c '[("extraKnownMarketplaces","enabledPlugins") as $k | select(.[$k] == null) | [$k]]' <<<"$doc" 2>/dev/null) \
    || die "не разобрать $real"

  if [ "$ms" = add ]; then
    act "claude: claude plugin marketplace add $MKT_DIR"
    if [ "$DRY" = 0 ]; then
      man_add "$(jq -nc --arg key "cli|marketplace|$MKT_NAME" --arg n "$MKT_NAME" --arg f "$real" \
        --argjson c "$created" --argjson fc "$([ "$pre_existing" = true ] && echo false || echo true)" \
        --argjson md "$(missing_dirs "$real")" \
        '{key:$key, agent:"claude", kind:"claude-marketplace", name:$n, file:$f, created:$c, file_created:$fc, dirs_created:$md}')"
      backup_once "$real"
      claude_cli plugin marketplace add "$MKT_DIR"
      claude_declared marketplace || die "claude не записал маркетплейс $MKT_NAME в $real: иная версия CLI?"
    fi
  fi
  if [ "$ps" = add ]; then
    act "claude: claude plugin install $PLUGIN_ID"
    if [ "$DRY" = 0 ]; then
      man_add "$(jq -nc --arg key "cli|plugin|$PLUGIN_ID" --arg n "$PLUGIN_ID" --arg f "$real" \
        --argjson c "$created" --argjson fc "$([ "$pre_existing" = true ] && echo false || echo true)" \
        --argjson md "$(missing_dirs "$real")" \
        '{key:$key, agent:"claude", kind:"claude-plugin", name:$n, file:$f, created:$c, file_created:$fc, dirs_created:$md}')"
      backup_once "$real"
      claude_cli plugin install "$PLUGIN_ID"
      claude_declared plugin || die "claude не записал плагин $PLUGIN_ID в $real: иная версия CLI?"
    fi
  fi
  jedit_apply "allow" "$SETTINGS" '["permissions","allow"]' '"Bash(tk *)"' "$ST_ARR" "$AD_ARR" arr \
    "claude: permissions.allow += Bash(tk *)" "$([ "$pre_existing" = true ] && echo false || echo true)"
}

# Ключи верхнего уровня settings.json, которые пишет connect.sh (сам или через CLI).
# Остальные claude CLI менять не должен (он, например, переписывает model): после вызова они возвращаются.
MANAGED_KEYS='["extraKnownMarketplaces","enabledPlugins","permissions"]'
GUARD='. as $n
  | ([$o | keys_unsorted[]] + [$n | keys_unsorted[]] | unique - $m) as $ks
  | reduce $ks[] as $k ($n; if ($o | has($k)) then .[$k] = $o[$k] else del(.[$k]) end)'
CHANGED='. as $n
  | [([$o | keys_unsorted[]] + [$n | keys_unsorted[]] | unique - $m)[] as $k
     | select(($o | has($k)) != ($n | has($k)) or $o[$k] != $n[$k]) | $k] | join(", ")'

claude_cli() {
  local out real before after fixed changed
  real=$(resolve "$SETTINGS")
  before=$(json_load "$real")
  out=$(claude "$@" 2>&1) || die "claude $*: ${out:-ошибка без вывода}"
  [ -e "$real" ] || return 0
  after=$(json_load "$real")
  changed=$(jq -r --argjson o "$before" --argjson m "$MANAGED_KEYS" "$CHANGED" <<<"$after")
  [ -n "$changed" ] || return 0
  fixed=$(jq --argjson o "$before" --argjson m "$MANAGED_KEYS" "$GUARD" <<<"$after") || die "не вернуть ключи в $real"
  printf '%s\n' "$fixed" | write_atomic "$real"
  warn "claude CLI изменил ключи settings.json: $changed; значения возвращены ($real)"
}
claude_declared() { # marketplace|plugin
  local real
  real=$(resolve "$SETTINGS")
  case $1 in
    marketplace) jq -e --arg n "$MKT_NAME" '(try .extraKnownMarketplaces[$n] catch null) != null' "$real" >/dev/null 2>&1 ;;
    plugin) jq -e --arg n "$PLUGIN_ID" '(try .enabledPlugins[$n] catch null) != null' "$real" >/dev/null 2>&1 ;;
  esac
}

run_codex() {
  local group
  group=$(jq -ce '.hooks.SessionStart[0]' "$CODEX_HOOKS_SRC") || die "нет группы SessionStart в $CODEX_HOOKS_SRC"
  jedit_apply "codex-hook" "$CODEX_HOOKS" '["hooks","SessionStart"]' "$group" "$ST_HOOK" "$AD_ARR" hook \
    "codex: hooks.SessionStart += tk ctx"
  put_file "file|$(resolve "$CODEX_RULES")" "$(resolve "$CODEX_RULES")" "$CODEX_RULES_SRC" "codex: правило" codex
}

run_opencode() {
  jedit_apply "opencode-perm" "$OC_JSON" '["permission","bash","tk *"]' '"allow"' "$ST_OC" "$AD_KEY" key \
    'opencode: permission.bash["tk *"] = allow'
  if [ -f "$OC_PLUGIN_SRC" ]; then
    put_file "file|$(resolve "$OC_PLUGIN")" "$(resolve "$OC_PLUGIN")" "$OC_PLUGIN_SRC" "opencode: плагин" opencode
  else
    warn "нет $OC_PLUGIN_SRC: плагин OpenCode пропущен"
  fi
}

run_agents() {
  man_check
  want claude && run_claude
  want codex && run_codex
  if want codex || want opencode; then put_skill; fi
  want opencode && run_opencode
  return 0
}

# ---------- откат ----------

rmdirs() { # $1 = JSON-массив каталогов, самый глубокий первым
  local d
  while IFS= read -r d; do
    [ -n "$d" ] && rmdir "$d" 2>/dev/null || true
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
    json)
      f=$(jq -r .file <<<"$e"); p=$(jq -c .path <<<"$e"); v=$(jq -c .value <<<"$e")
      op=$(jq -r .op <<<"$e"); c=$(jq -c .created <<<"$e"); fc=$(jq -r .file_created <<<"$e")
      case $op in arr) rm_prog=$RM_ARR ;; hook) rm_prog=$RM_HOOK ;; key) rm_prog=$RM_KEY ;; *) die "манифест: неизвестный op $op" ;; esac
      if [ -e "$f" ]; then
        doc=$(json_load "$f")
        new=$(jq --argjson p "$p" --argjson v "$v" --argjson c "$c" "$rm_prog | $PRUNE" <<<"$doc") \
          || die "не снять запись из $f"
        act "undo: $f"
        undo_json_file "$f" "$new" "$fc"
      fi
      rmdirs "$md"
      ;;
    claude-marketplace|claude-plugin)
      CLAUDE_UNDONE=1
      f=$(jq -r .file <<<"$e"); c=$(jq -c .created <<<"$e"); fc=$(jq -r .file_created <<<"$e")
      v=$(jq -r .name <<<"$e")
      command -v claude >/dev/null 2>&1 || { warn "нет claude в PATH: $v оставлен в манифесте"; return 1; }
      if [ "$kind" = claude-marketplace ]; then
        if jq -e --arg n "$v" '(try .extraKnownMarketplaces[$n] catch null) != null' "$f" >/dev/null 2>&1; then
          act "undo: claude plugin marketplace remove $v"
          claude_cli plugin marketplace remove "$v"
        fi
      else
        if jq -e --arg n "$v" '(try .enabledPlugins[$n] catch null) != null' "$f" >/dev/null 2>&1; then
          act "undo: claude plugin uninstall $v"
          claude_cli plugin uninstall "$v"
        fi
      fi
      if [ -e "$f" ]; then
        doc=$(json_load "$f")
        new=$(jq --argjson c "$c" "$PRUNE" <<<"$doc") || die "не прибрать $f"
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
      if [ -d "$f" ]; then act "undo: удалить $f"; rm -r "$f"; fi
      rmdirs "$md"
      ;;
    *) die "манифест: неизвестный kind $kind" ;;
  esac
  man_drop "$key"
}

run_undo() {
  local e kind key f left=0 ag
  man_check
  [ -e "$MAN" ] || return 0
  # Сначала записи в обратном порядке, потом бэкапы.
  while IFS= read -r e; do
    kind=$(jq -r .kind <<<"$e")
    [ "$kind" = backup ] && continue
    ag=$(jq -r .agent <<<"$e")
    if [ "$AGENT" != all ] && [ "$ag" != "$AGENT" ]; then left=1; continue; fi
    undo_entry "$e" || left=1
  done < <(man_json | jq -c '.entries | reverse | .[]')
  if [ "$AGENT" = all ] && [ "$left" = 0 ]; then
    while IFS= read -r e; do
      key=$(jq -r .key <<<"$e")
      f=$(jq -r .path <<<"$e")
      if [ -e "$f" ]; then act "undo: удалить $f"; rm -f "$f"; fi
      man_drop "$key"
    done < <(man_json | jq -c '.entries[] | select(.kind == "backup")')
  fi
  if [ "$CLAUDE_UNDONE" = 1 ]; then
    local x left_cli=""
    for x in "$HOME/.claude.json" "$HOME/.claude/plugins" "$HOME/.claude/backups"; do
      [ -e "$x" ] && left_cli="$left_cli $x"
    done
    [ -z "$left_cli" ] || act "undo: оставлено claude CLI, не трогаю:$left_cli"
  fi
  if [ "$(man_json | jq '.entries | length')" = 0 ]; then
    act "undo: удалить манифест $MAN"
    rm -f "$(resolve "$MAN")"
    rmdir "$(dirname "$(resolve "$MAN")")" 2>/dev/null || true
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
    ;;
  undo)
    DRY=0
    run_undo
    ;;
esac
