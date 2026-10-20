# Спека: З3 Коннекторы агентов

Статус: agreed (рамка сужена 2026-10-20: минимум настроек, без следов)
Каноны: `docs/spec.md` (2.4, 7), `docs/specs/decisions.md` (Р7, Р8, Р17, Р49, Р54, Р56), `docs/plans/z2-agent-cycle.md`

## 1. Цель и границы

- Цель: Claude Code и Codex знают `tk`, получают `tk ctx` на старте и зовут `tk` без запроса.
- Результат: скилл `tk`, разрешение `tk` и хук `ctx` в обоих. Домашняя сессия в `~/tasks` пишет идею в `inbox` (`tk new`, проект `personal`).
- Делаем: `connectors/`, установочный скрипт `scripts/connect.sh` (не команда `tk`), тесты на временном HOME, `make e2e-agents`.
- Не делаем: OpenCode, плагин и маркетплейс Claude, `tk setup`, ассистента по расписанию.
- Зависимость: З2 в `main` (кода нет, опора на её план).

## 2. Архитектура

| Блок | Ответственность |
|---|---|
| `connectors/` | один скилл (английский, Р46, до 1,5 КБ) для обоих агентов; фрагмент хука Claude; хук и правило Codex |
| `scripts/connect.sh` | слияние в настройки пользователя и откат (нужны `jq`, `tk`; `claude` и `codex` не нужны) |
| `scripts/e2e-agents.sh` | живой прогон двух агентов |

Решения задачи (модель системы не меняют):
- Скрипт, не инструкция агенту: тест на временном HOME и откат. Заденет: `tk setup` заменит скрипт.
- Claude без плагина и маркетплейса: файл скилла и две правки `settings.json` (хук, разрешение). Отброшено: `claude plugin ...` (CLI пишет в `~/.claude.json`, `~/.claude/plugins`, переписывает чужие ключи).
- Codex: свой файл `~/.codex/rules/tk.rules`, не правка `default.rules` (чужие правила).
- Скилл: одна копия-источник `connectors/agents/skills/tk`, `apply` копирует её в два каталога.
- Повтор `ctx` после сжатия: `SessionStart` с `compact` (замечание о Р54 в разделе 9).

## 3. Сценарии

| Сценарий | Шаги | Результат |
|---|---|---|
| Happy path | `plan`, `apply` на пустом HOME | ровно файлы раздела 4; повторный `plan` пуст |
| Повтор | `apply` дважды | нет изменений, нет `.tk-bak-*` |
| Откат | `apply`, `undo` | снято только записанное в манифест; JSON равен исходному по `jq -S`; на пустом HOME следов нет |
| Ключ уже был | `Bash(tk *)` или хук `tk ctx` уже есть | `apply` не пишет и не заносит в манифест, `undo` оставляет |
| Настройки - симлинк, права 600 | `apply` | пишется в цель, ссылка цела, права те же |
| Чужое | HOME с реальными хуками, правилами, разрешениями | чужие ключи и порядок целы |
| Нет `jq` или `tk`; битый JSON | `apply` | `err`, ничего не записано |
| Сбой посреди `apply` | каталог правил закрыт на запись | код не 0, оставшиеся бэкапы названы, `undo` возвращает исходное |
| Домашняя сессия | в `~/tasks`: «запиши идею X» | задача в `inbox`, `Actor:` агента |

## 4. Данные: что и куда пишет `apply`

Перед правкой существующего файла бэкап `<файл>.tk-bak-<время>`. Бэкапы живут только на время `apply`: после успешной записи всех файлов их удаляют. При сбое они остаются, и вывод называет пути. Путь файла разрешается `realpath`; временный файл и бэкап лежат рядом с целью, с теми же правами; rename поверх цели (симлинк не рвётся). Время - аргумент.

| Агент | Путь | Что | Откат |
|---|---|---|---|
| Claude | `~/.claude/settings.json` | `hooks.SessionStart += {matcher "startup\|resume\|clear\|compact", команда "tk ctx"}` | запись манифеста (группа с точной командой `tk ctx`) |
| Claude | `~/.claude/settings.json` | `permissions.allow += "Bash(tk *)"` | запись манифеста |
| Claude | `~/.claude/skills/tk/` | скилл (новый каталог) | удалить каталог |
| Codex | `~/.codex/hooks.json` | группа `SessionStart`, тот же matcher, команда `tk ctx` | запись манифеста |
| Codex | `~/.codex/rules/tk.rules` | `prefix_rule(pattern=["tk"], decision="allow")` | удалить файл |
| Codex | `~/.agents/skills/tk/` | скилл (новый каталог) | удалить каталог |
| оба | `${TRACKER_HOME:-~/tasks}/.tracker/connect-state.json` | манифест: что `apply` добавил на самом деле, созданные каталоги | удаляется последним |

Больше `apply` не пишет ничего: нет `~/.config/tk`, `~/.claude.json`, `~/.claude/plugins`, `config.toml`, `default.rules`, `CLAUDE.md`, `AGENTS.md`. Ручной шаг: одобрить хук Codex в `/hooks`.
Личность: `actor.Resolve` З2.

## 5. Кодовая модель

Кода Go нет. `scripts/connect.sh` (bash, `jq`, `shellcheck`): `connect.sh plan|apply|undo [--agent claude|codex|all] [--now ISO]`.
- `plan` (по умолчанию) печатает пути и ключи, не пишет.
- `apply`: проверить `tk`, `jq`; сначала сухой проход (конфликт или битый JSON - `err` до первой записи), затем запись: добавить ключ, если нет, и внести в манифест; порядок чужих ключей сохранить; бэкап; rename; в конце убрать бэкапы.
- `undo`: убрать записи манифеста в обратном порядке, затем манифест и созданные пустые каталоги. HOME берётся из окружения.

`SKILL.md`: description с `Use when`; цикл `ctx`, `take`, `note`, `check`, `done -m`; `tk` одиночной командой без `;`, `&&`, `|`, `cd` (Codex); вне проекта `tk new` - идея в `inbox`.

## 6. Этапы реализации

| Зона | Файлы | Проверка |
|---|---|---|
| A коннекторы | `connectors/**` | `jq empty` |
| B скрипт | `scripts/connect.sh` | `shellcheck`, `bash -n`, тесты T |
| T тесты | `scripts/test-connect.sh`, `connectors/testdata/` (фикстуры без секретов) | красные до B |
| C e2e | `scripts/e2e-agents.sh`, цель Makefile | `shellcheck`, `bash -n`, `make -n e2e-agents` |
| D документы | `AGENTS.md`, `docs/architecture.md`, `README.md` | `make check` |

1. A-D, T параллельно. Тестам `claude` и `codex` не нужны.
2. `code-reviewer`, `skeptic` по `regress`: идемпотентность, откат, чужие ключи, симлинки.
3. Только с согласия: `make e2e-agents` (тратит лимиты).
4. Только с согласия: `connect.sh apply` на настоящем HOME после `plan`; одобрение хука в `/hooks`.

## 7. Критерий приёмки

- Команда: `make check`; `scripts/test-connect.sh` идёт на `HOME=$(mktemp -d)`, настоящий HOME не читает и не пишет (отпечаток до и после).
- Тесты сверяют список файлов HOME с разделом 4; после `undo` на пустом HOME он пуст.
- `make e2e-agents` (шаг 3): задача в `ready` с одним критерием, один промпт; задачи в `~/.cache/tk-e2e/<случайное>` (не `/tmp`: у Codex ложно-зелёное). Настройки пользователя не пишутся. Claude: `claude -p --settings <файл с хуком и разрешением>`, скилл в `<фикстура>/.claude/skills`. Codex: временный `CODEX_HOME` (симлинк `auth.json`, `tk.rules`), скилл в `<фикстура>/.agents/skills`. Агент идёт через `env -u` с id запускающей сессии (`CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`, `CODEX_SESSION_ID`, `TRACKER_ACTOR`), иначе `Actor:` чужой. Второй промпт в фикстуре `~/tasks`: «запиши идею X»; pass: задача в `idea`, проект `personal`, `Actor:` агента. Pass: коммиты `take`, `note`, `done` с `Actor:` `claude:<id>`, `codex:<id>`; id равен id из JSON прогона.
- Хук: тест подаёт JSON на stdin, вывод короче 2000 знаков.
- Не автоматизируется: одобрение хука Codex, применение к настоящему HOME. Риск: симлинк `auth.json` не обновит токен; после прогона проверить `~/.codex/auth.json`.

## 8. Факты (проверено 2026-10-20)

- Хуки Claude Code в `settings.json`: ключ `hooks`, событие `SessionStart`, массив групп `{matcher, hooks:[{type:"command", command, timeout}]}`; matcher `startup|resume|clear|compact|fork`, stdout в контекст - https://code.claude.com/docs/en/hooks ; `PreCompact` контекст не добавляет.
- Скилл: `~/.claude/skills/<имя>/SKILL.md`, проектный `.claude/skills` - https://code.claude.com/docs/en/skills
- `claude --settings <файл-или-JSON>` грузит доп. настройки на один запуск (`claude --help`, 2.1.304).
- `Bash(tk *)` совпадает и с голым `tk`; составная команда по частям - https://code.claude.com/docs/en/permissions
- Codex: `~/.codex/hooks.json`, `SessionStart` с matcher, одобрение в `/hooks` - https://learn.chatgpt.com/docs/hooks ; скиллы `$HOME/.agents/skills` - https://learn.chatgpt.com/docs/build-skills ; `prefix_rule`, `CODEX_THREAD_ID` - `docs/api/codex.md`
- Ядро З1 не включает `.tracker/connect-state.json` в `.gitignore` (`internal/core/tx.go`): следующая запись `tk` закоммитит файл как external edit (раздел 9).

Не знаю (проверка на шагах 1 и 3): видит ли хук Claude `CLAUDE_CODE_SESSION_ID` (в Bash он есть); срабатывает ли хук из `--settings` в `claude -p`; пропускают ли песочницы `TRACKER_HOME`; запускает ли `codex exec` неодобренный хук; нужен ли Codex флаг хуков в `config.toml` (`codex features list`).

## 9. Пометки пользователю

1. Р54 велит хук `PreCompact` с `ctx`, но он контекст не добавляет (раздел 8). План ставит `SessionStart` с `compact`. Текст Р54 не менялся: правка за пользователем.
2. Манифест в `~/tasks/.tracker/` попадёт в git задач. Варианты: (а) добавить `.tracker/connect-state.json` в `.gitignore`, который создаёт ядро (правка З1, старые базы не обновятся); (б) оставить: файл уйдёт в коммит как external edit. Рекомендация (а).
