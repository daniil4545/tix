# Трекер и три агента: факты для `tracker setup <agent>`
Дата: 2026-10-20. Версии на машине: claude 2.1.304, codex-cli 0.163.0, opencode 1.19.3.
Источники: code.claude.com/docs/en/{memory,hooks,skills,permissions}; learn.chatgpt.com/docs/ (бывший developers.openai.com/codex/*): agent-configuration/agents-md, hooks, build-skills, agent-configuration/rules, sandboxing, agent-approvals-security; opencode.ai/docs/{rules,skills,plugins,permissions}. [L] = проверено локально на машине.

## Таблица механизм x агент
| Механизм | Claude Code | Codex CLI | OpenCode |
|---|---|---|---|
| Глобальные инструкции | ~/.claude/CLAUDE.md | ~/.codex/AGENTS.md (AGENTS.override.md главнее; CODEX_HOME) | ~/.config/opencode/AGENTS.md; фолбэк ~/.claude/CLAUDE.md |
| Проектные | ./CLAUDE.md или ./.claude/CLAUDE.md; ./AGENTS.md читается, только если нет ни одного CLAUDE.md выше (v2.1.277+) | AGENTS.md от корня git вниз до cwd, по одному на каталог, склейка; лимит project_doc_max_bytes 32 KiB | AGENTS.md вверх от cwd; фолбэк CLAUDE.md; поле `instructions` в opencode.json (пути, glob, URL) |
| Импорт файла | `@path`, относительный/абсолютный, глубина 4; грузится при старте (токены не экономит); внешний импорт в проектном файле требует одобрения, в ~/.claude/CLAUDE.md без диалога | в документации не описан (пробел) | `@` не разбирается; вместо него `instructions` |
| Скиллы SKILL.md | да; ~/.claude/skills/<имя>/SKILL.md, проект .claude/skills; в контексте только description (до 1536 симв.), тело при вызове | да; ~/.agents/skills, <repo>/.agents/skills (вверх до корня), /etc/codex/skills; в контексте name+description, бюджет 2% окна или 8000 симв. (до 10000 токенов); вызов `$name` или неявно; `agents/openai.yaml: policy.allow_implicit_invocation` | да; ~/.config/opencode/skills, ~/.claude/skills, ~/.agents/skills, проект .opencode/.claude/.agents/skills; ленивая загрузка через tool `skill` (в описании tool только name+description); name 1-64 симв., description до 1024 |
| Хук старта сессии | SessionStart (matcher startup/resume/clear/compact/fork); stdout или JSON hookSpecificOutput.additionalContext; >10000 симв. уходит в файл + превью 2000; хук задерживает первый ответ | SessionStart есть (те же источники); stdout = developer context; additionalContext в JSON; лимит ~2500 токенов (additionalContextLimit); конфиг ~/.codex/hooks.json или [hooks] в config.toml, проектный только в доверенном .codex/; новый хук надо одобрить в `/hooks` | плагин JS/TS в ~/.config/opencode/plugins/; события session.created и др.; в документации плагинов нет хука, добавляющего текст в контекст (только experimental.session.compacting и shell.env); см. пробелы |
| Slash-команды | да; .claude/commands/x.md и skills слиты в одно пространство имён | custom prompts в этой доке не найдены; скиллы `$name` и `/skills` (пробел) | да; ~/.config/opencode/commands/*.md [L], `command` в opencode.json [L]; skill с metadata `opencode/slash` даёт /name |
| MCP | да | да ([L] rmcp-клиент в config.toml) | да |
| Разрешить `tracker` без запроса | permissions.allow: `Bash(tracker *)` (пробел перед `*`, `:*` эквивалент); составные команды проверяются по частям; `allowed-tools` во frontmatter скилла | rules: ~/.codex/rules/default.rules `prefix_rule(pattern=["tracker"], decision="allow")`: команда идёт ВНЕ sandbox без запроса | opencode.json `permission.bash: {"*":"ask","tracker *":"allow"}`; последнее совпадение побеждает; большинство прав по умолчанию allow |
| Запись в ~/tasks | Bash-процесс tracker пишет сам; ограничение только разрешением Bash (sandbox Claude в этих источниках не проверялся) | workspace-write: писать можно cwd и /tmp; вне него запрос одобрения; `.git` внутри writable root read-only всегда | external_directory по умолчанию ask, но это про tools агента; bash-процесс tracker, вероятно, не затрагивается (не проверено) |
| Идентификация | CLAUDECODE=1, CLAUDE_CODE_SESSION_ID в окружении [L]; в хуках session_id в stdin | в бинаре строки CODEX_THREAD_ID, CODEX_SESSION_ID, CODEX_SANDBOX (strings, докой не подтверждено); в хуках session_id в stdin | в бинаре строки OPENCODE_CLIENT, OPENCODE_PID; id сессии в окружении не подтверждён; плагин может выставить через `shell.env` |

## Рекомендация: минимальный набор файлов
Принцип: в постоянном контексте одна-две строки; правила и задачи проекта приходят из `tracker ctx` хуком или по запросу.
- Общее: скилл `tracker` (SKILL.md, description одна фраза с «Use when» про задачи; тело - список команд). Каталог ~/.agents/skills/tracker читают Codex и OpenCode; Claude читает только ~/.claude/skills/tracker (OpenCode читает и его).
- Claude: ~/.claude/skills/tracker/SKILL.md; в settings.json permissions.allow `Bash(tracker *)`; хук SessionStart (startup|resume|clear) -> `tracker ctx --agent claude`, вывод короче 2000 симв.; в ~/.claude/CLAUDE.md не больше одной строки.
- Codex: ~/.agents/skills/tracker/SKILL.md; ~/.codex/rules/default.rules с prefix_rule allow ["tracker"]; хук SessionStart в ~/.codex/hooks.json (одобрить в /hooks); строка в ~/.codex/AGENTS.md.
- OpenCode: скилл в ~/.agents/skills; `permission.bash["tracker *"]="allow"` в opencode.json; строка в ~/.config/opencode/AGENTS.md. Плагин-стартовик только после проверки хука инъекции контекста; пока контекст проекта берётся командой `tracker ctx` по предписанию скилла.
- `setup` идемпотентный, JSON/TOML правит слиянием. У пользователя уже есть ~/.codex/config.toml, hooks.json, opencode.json; ~/.codex/AGENTS.md и ~/.config/opencode/AGENTS.md там симлинки на ~/.claude/CLAUDE.md [L], запись в них меняет один общий файл. В ~/.config/opencode/skills и commands тоже симлинки [L].

## Риски
1. Codex sandbox: `.git` в writable root read-only, запись вне cwd требует одобрения. `git commit` в ~/tasks изнутри sandbox упадёт или спросит. Спасает prefix_rule allow (команда идёт вне sandbox). Живым прогоном не проверял (платный вызов).
2. prefix_rule allow выводит из sandbox всю команду `tracker ...` со всеми аргументами: бинарь должен быть безопасен сам.
3. Rules Codex помечены экспериментальными; хуки требуют ручного доверия, изменённый хук молча пропускается.
4. Claude: Bash(tracker *) не покрывает `tracker x && other`, каждая часть проверяется отдельно.
5. Лимиты вывода хука: Claude 10000 симв., Codex ~2500 токенов; ctx должен быть короче.
6. Фолбэки OpenCode (CLAUDE.md, ~/.claude/CLAUDE.md) могут дублировать правила, если лежат и AGENTS.md, и CLAUDE.md.
7. Адреса learn.chatgpt.com/docs временные (308 с developers.openai.com).
8. Codex в этой машине: sandbox_mode workspace-write, approval_policy never/on-request в разных профилях [L]; профиль влияет на то, спросит ли запись вне cwd.

## Пробелы
- Импорт в AGENTS.md Codex: не описан.
- Хук OpenCode, добавляющий текст в контекст старта: в документации нет; в исходниках может быть experimental.chat.system.transform (не проверял).
- Переменная id сессии у Codex и OpenCode: только строки в бинарях; проверить печатью окружения внутри живой сессии.
- Custom prompts / слэш-команды Codex: актуальную страницу не нашёл.
- Sandbox Claude Code и запись в ~/tasks: не проверял.
- Реальный расход токенов скилла и хука не измерялся.
