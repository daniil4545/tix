#!/usr/bin/env bash
# Живая проверка коннекторов З3: Claude Code и Codex проходят цикл tix на настоящих CLI.
# План: docs/plans/z3-connectors.md, раздел 7. ТРАТИТ ЛИМИТЫ ПОДПИСОК: запуск только с --yes.
#
# Что делает для каждого агента:
#   1. Фикстура в ~/.cache/tix-e2e/<случайное>/<агент> (не /tmp: у Codex там ложно-зелёный результат):
#      git-проект e2eproj, TIX_HOME с задачей T-1 в ready и одним критерием.
#   2. Прогон 1 (cwd проекта): агент берёт задачу. Pass: коммиты take, note, done с Actor: агента.
#   3. Прогон 2 (cwd = TIX_HOME, «домашняя сессия»): «запиши идею». Pass: новая задача
#      в idea, проект personal, коммит new с Actor: агента.
# Настройки пользователя не пишутся. Claude: `--settings` на файл с хуком и разрешением из
# connectors/claude/hooks.json, скилл в <фикстура>/.claude/skills. Codex: временный CODEX_HOME
# (симлинк auth.json, tix.rules, hooks.json не ставится), скилл в <фикстура>/.agents/skills.
# Каждый агент идёт через `env -u` с id запускающей сессии, иначе чужой id попадёт в Actor:.
#
# Использование: scripts/e2e-agents.sh --yes [--agents claude,codex]
# Окружение: E2E_TIMEOUT (с на один прогон, 600), E2E_CODEX_MODEL (по умолчанию gpt-6.1-sol).
# Код выхода: 0 - pass у всех выбранных агентов; 1 - есть fail; 2 - неверный вызов, нет --yes.

set -u -o pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
TIMEOUT=${E2E_TIMEOUT:-600}
CODEX_MODEL=${E2E_CODEX_MODEL:-gpt-6.1-sol}
PROMPT_TASK='Pick up the next task from this project task tracker and finish it.'
PROMPT_IDEA='Write down an idea: add dark mode to the notes app.'
# Личность запускающей сессии не должна попасть в Actor: чужого агента.
SCRUB=(-u CLAUDE_CODE_SESSION_ID -u CODEX_THREAD_ID -u CODEX_SESSION_ID -u TIX_ACTOR)

confirm=no
agents=(claude codex)
while [ $# -gt 0 ]; do
	case "$1" in
	--yes) confirm=yes ;;
	--agents)
		shift
		[ $# -gt 0 ] || { echo "e2e-agents: --agents needs a list" >&2; exit 2; }
		IFS=, read -r -a agents <<<"$1"
		;;
	*) echo "usage: scripts/e2e-agents.sh --yes [--agents claude,codex]" >&2; exit 2 ;;
	esac
	shift
done
[ "$confirm" = yes ] || {
	echo "e2e-agents: the run spends subscription limits (2 agents x 2 prompts, up to ${TIMEOUT}s each)." >&2
	echo "Repeat with --yes (make e2e-agents E2E_ARGS=--yes) after the owner agrees." >&2
	exit 2
}
for a in "${agents[@]}"; do
	case "$a" in claude | codex) ;; *) echo "e2e-agents: unknown agent $a" >&2; exit 2 ;; esac
done

for tool in go git jq perl; do
	command -v "$tool" >/dev/null || { echo "e2e-agents: $tool not found" >&2; exit 2; }
done

mkdir -p "$HOME/.cache/tix-e2e"
E2E=$(mktemp -d "$HOME/.cache/tix-e2e/run.XXXXXX") || exit 2
echo "e2e-agents: fixtures and logs in $E2E"
mkdir -p "$E2E/bin"
go build -C "$ROOT" -o "$E2E/bin/tix" ./cmd/tix || { echo "e2e-agents: go build tix failed" >&2; exit 2; }

# Файлы коннекторов (зона A).
SKILL_DIR="$ROOT/connectors/agents/skills/tix"
RULES_FILE="$ROOT/connectors/codex/tix.rules"
CLAUDE_HOOKS="$ROOT/connectors/claude/hooks.json"

D="" WORK="" TASKS="" FAILS=0
declare -a SUMMARY=()

# tix_in <cwd> <args>: tix фикстуры без личности запускающей сессии.
tix_in() {
	local cwd=$1
	shift
	(cd "$cwd" && env "${SCRUB[@]}" "TIX_HOME=$TASKS" "PATH=$E2E/bin:$PATH" tix "$@")
}

# agent_run <cwd> <out> <err> <NAME=val...> -- <команда>; промпт - в stdin из $PROMPT_FILE.
# perl alarm ставит потолок времени и переживает exec.
agent_run() {
	local cwd=$1 out=$2 err=$3
	shift 3
	local extra=()
	while [ "$1" != -- ]; do extra+=("$1"); shift; done
	shift
	(cd "$cwd" && env "${SCRUB[@]}" "TIX_HOME=$TASKS" "PATH=$E2E/bin:$PATH" ${extra[@]+"${extra[@]}"} \
		perl -e 'alarm shift; exec @ARGV or exit 127' "$TIMEOUT" "$@") <"$PROMPT_FILE" >"$out" 2>"$err"
}

setup_fixture() {
	local agent=$1
	D=$E2E/$agent
	WORK=$D/e2eproj
	TASKS=$D/tasks
	mkdir -p "$WORK"
	git -C "$WORK" init -q || return 1
	git -C "$WORK" -c user.name=e2e -c user.email=e2e@localhost commit -q --allow-empty -m init || return 1
	tix_in "$WORK" new "Report that the job is done" --to todo --as e2e:setup >/dev/null || return 1
	local rev
	rev=$(tix_in "$WORK" show T-1 | sed -n '1s/^rev //p')
	[ -n "$rev" ] || return 1
	printf '## Details\n\n- [ ] Say "job done" in your final answer\n' |
		tix_in "$WORK" edit T-1 --base "$rev" --as e2e:setup >/dev/null || return 1
	[ "$(tix_in "$WORK" list --json | jq -r '.[0].project + " " + .[0].status')" = "e2eproj todo" ] || return 1
	mkdir -p "$TASKS/.git/info"
	printf '.agents/\n.claude/\n' >>"$TASKS/.git/info/exclude"
}

# install_skill <каталог скиллов>: скилл в проект и в TIX_HOME фикстуры
# (Claude: .claude/skills, Codex: .agents/skills).
install_skill() {
	local dir
	for dir in "$WORK" "$TASKS"; do
		mkdir -p "$dir/$1/tix"
		cp -R "$SKILL_DIR/." "$dir/$1/tix/" || return 1
	done
}

# actor_of <regex>: Actor: самого нового коммита задач, чей заголовок подходит под ERE.
actor_of() {
	git -C "$TASKS" --no-optional-locks log --format='%s%x1f%(trailers:key=Actor,valueonly,separator=%x2C)' 2>/dev/null |
		awk -F'\037' -v re="$1" '$1 ~ re { print $2; exit }' || true
}

# session_id <agent> <json-файл>: id сессии из JSON прогона.
session_id() {
	case $1 in
	claude) jq -r '[.. | objects | .session_id? // empty] | first // empty' "$2" 2>/dev/null ;;
	codex) jq -rs '[.[] | select(.type == "thread.started") | .thread_id] | first // empty' "$2" 2>/dev/null ;;
	esac
}

# launch <agent> <cwd> <prompt> <tag>: один прогон, JSON в $D/<tag>.out, stderr в $D/<tag>.err.
launch() {
	local agent=$1 cwd=$2 tag=$4
	PROMPT_FILE=$D/$tag.prompt
	printf '%s' "$3" >"$PROMPT_FILE"
	local out=$D/$tag.out err=$D/$tag.err
	case $agent in
	claude)
		agent_run "$cwd" "$out" "$err" -- \
			claude -p --settings "$D/claude-settings.json" --output-format json
		;;
	codex)
		agent_run "$cwd" "$out" "$err" "CODEX_HOME=$D/codex-home" -- \
			codex exec --json --skip-git-repo-check -C "$cwd" -m "$CODEX_MODEL"
		;;
	esac
}

# prepare_claude: настройки на один прогон (--settings): хук и разрешение, как пишет connect.sh.
prepare_claude() {
	jq '. + {permissions: {allow: ["Bash(tix *)"]}}' "$CLAUDE_HOOKS" >"$D/claude-settings.json"
}

# prepare_codex: временный CODEX_HOME; Codex читает промпт из stdin, если аргумента нет.
prepare_codex() {
	[ -f "$HOME/.codex/auth.json" ] || { echo "no ~/.codex/auth.json (codex login first)"; return 1; }
	[ -f "$RULES_FILE" ] || { echo "$RULES_FILE not found"; return 1; }
	mkdir -p "$D/codex-home/rules"
	ln -s "$HOME/.codex/auth.json" "$D/codex-home/auth.json"
	cp "$RULES_FILE" "$D/codex-home/rules/tix.rules"
	printf 'sandbox_mode = "workspace-write"\napproval_policy = "never"\n' >"$D/codex-home/config.toml"
}

# check_actor <метка> <ERE заголовка> <ожидаемый Actor> <режим eq|re>
check_actor() {
	local label=$1 re=$2 want=$3 mode=$4 got
	got=$(actor_of "$re")
	if [ -z "$got" ]; then
		echo "    FAIL $label: no commit"
		return 1
	fi
	if { [ "$mode" = eq ] && [ "$got" = "$want" ]; } || { [ "$mode" = re ] && [[ $got =~ $want ]]; }; then
		echo "    ok   $label: Actor $got"
		return 0
	fi
	echo "    FAIL $label: Actor '$got', expected '$want'"
	return 1
}

# expectation <agent> <sid>: ожидаемый Actor (eq) или none, когда id не найден.
expectation() {
	if [ -n "$2" ]; then
		echo "eq $1:$2"
	else
		echo "none -"
	fi
}

run_agent() {
	local agent=$1 bad=0 rc sid mode want
	echo "== $agent"
	command -v "$agent" >/dev/null || { echo "    FAIL $agent not found in PATH"; return 1; }
	setup_fixture "$agent" || { echo "    FAIL fixture setup (tix new/edit/list)"; return 1; }
	case $agent in
	claude)
		prepare_claude || return 1
		install_skill .claude/skills || return 1
		;;
	codex)
		prepare_codex || return 1
		install_skill .agents/skills || return 1
		;;
	esac

	echo "  run 1: task cycle in $WORK"
	launch "$agent" "$WORK" "$PROMPT_TASK" run1
	rc=$?
	echo "    exit $rc, log $D/run1.out, $D/run1.err"
	sid=$(session_id "$agent" "$D/run1.out")
	echo "    session id: ${sid:-not found in JSON}"
	read -r mode want < <(expectation "$agent" "$sid")
	if [ "$mode" = none ]; then
		echo "    FAIL no session id in the run JSON"
		bad=1
	else
		check_actor take '^T-1: take' "$want" "$mode" || bad=1
		check_actor note '^T-1: note' "$want" "$mode" || bad=1
		check_actor 'done' '^T-1: done' "$want" "$mode" || bad=1
	fi

	echo "  run 2: home session in $TASKS"
	launch "$agent" "$TASKS" "$PROMPT_IDEA" run2
	rc=$?
	echo "    exit $rc, log $D/run2.out, $D/run2.err"
	sid=$(session_id "$agent" "$D/run2.out")
	echo "    session id: ${sid:-not found in JSON}"
	read -r mode want < <(expectation "$agent" "$sid")
	if [ "$mode" = none ]; then
		echo "    FAIL no session id in the run JSON"
		bad=1
	else
		check_actor new '^T-([2-9]|[1-9][0-9]+): new' "$want" "$mode" || bad=1
	fi
	local idea
	idea=$(tix_in "$TASKS" list --json | jq -r '[.[] | select(.id != "T-1")] | first | (.status // "-") + " " + (.project // "-")')
	if [ "$idea" = "idea personal" ]; then
		echo "    ok   idea task: column/project $idea"
	else
		echo "    FAIL idea task: expected 'idea personal', got '${idea:-nothing}'"
		bad=1
	fi
	return "$bad"
}

for agent in "${agents[@]}"; do
	if run_agent "$agent"; then
		SUMMARY+=("PASS  $agent")
	else
		SUMMARY+=("FAIL  $agent  (logs: $E2E/$agent)")
		FAILS=$((FAILS + 1))
	fi
done

echo
echo "== summary"
printf '  %s\n' "${SUMMARY[@]}"
if printf '%s\n' "${agents[@]}" | grep -qx codex; then
	if jq empty "$HOME/.codex/auth.json" >/dev/null 2>&1; then
		echo "  codex auth.json: readable JSON (the symlink does not refresh a token; check codex login if it expired)"
	else
		echo "  WARN ~/.codex/auth.json is not valid JSON: run codex login"
	fi
fi
[ "$FAILS" -eq 0 ] && echo "e2e-agents: all passed" && exit 0
echo "e2e-agents: $FAILS agent(s) failed"
exit 1
