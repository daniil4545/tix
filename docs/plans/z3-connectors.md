# Спека: З3 Коннекторы агентов

Статус: agreed
Каноны: `docs/spec.md` (2.4, 7), `docs/specs/decisions.md` (Р7, Р8, Р17, Р49, Р54, Р56), `docs/plans/z2-agent-cycle.md`

## 1. Цель и границы

- Цель: три агента знают `tk`, получают `tk ctx` на старте и зовут `tk` без запроса.
- Результат: скилл `tk` и разрешение `tk` во всех трёх; хук `ctx` в Claude Code и Codex. Домашняя сессия в `~/tasks` пишет идею в `inbox` (`tk new`, проект `personal`).
- Делаем: `connectors/`, установочный скрипт `scripts/connect.sh` (не команда `tk`), тесты на временном HOME, `make e2e-agents`.
- Не делаем: `tk setup`, ассистента по расписанию.
- Зависимость: З2 в `main`; кода З2 нет, опора на её план.

## 2. Архитектура

| Блок | Ответственность |
|---|---|
| `connectors/` | скилл (английский, Р46, до 1,5 КБ), плагин Claude `tk` в маркетплейсе `tk-local`, файлы Codex и OpenCode |
| `scripts/connect.sh` | слияние в настройки пользователя и откат (`jq`, `claude`) |
| `scripts/e2e-agents.sh` | живой прогон трёх агентов |

Решения задачи (модель системы не меняют):
- Скрипт, не ручная инструкция - тест на временном HOME и откат; отброшено: инструкция агенту; заденет: `tk setup` заменит скрипт
- `Bash(tk *)` в `~/.claude/settings.json`: плагин разрешений нести не может
- Codex: свой файл `~/.codex/rules/tk.rules`, не правка `default.rules` (чужие правила)
- OpenCode: разрешение пишется, только если `permission.bash` не разрешает `tk` (сейчас `"*": "allow"`)
- Скилл в двух копиях (плагин, `~/.agents/skills/tk`), тест сверяет
- Повтор `ctx` после сжатия - `SessionStart` с `compact` (замечание о Р54 в разделе 9)
- Идея в домашней сессии: скилл и `RULES.md` (З2)

## 3. Сценарии

| Сценарий | Шаги | Результат |
|---|---|---|
| Happy path | `plan`, `apply` на пустом HOME | файлы раздела 4; повторный `plan` пуст |
| Повтор | `apply` дважды | нет изменений и новых `.bak` |
| Откат | `apply`, `undo` | снято только записанное `apply` ; JSON равен исходному по `jq -S`; свои `.tk-bak-*` удалены |
| Ключ уже был | `Bash(tk *)` или `"tk *"` уже есть | `apply` не пишет и не заносит в манифест, `undo` оставляет |
| Настройки - симлинк, права 600 | `apply` | пишется в цель, ссылка цела, права те же |
| Чужое | HOME с реальными хуками, правилами, разрешениями | чужие ключи и порядок целы |
| Нет `jq`, `claude`, `tk`; битый JSON | `apply` | `err`, файл цел |
| Домашняя сессия | в `~/tasks`: «запиши идею X» | задача в `inbox`, `Actor:` агента |
| Непредусмотренное | иная версия агента | `err` с путём, без записи |

## 4. Данные: что и куда пишет `apply`

Порядок в Claude: сначала CLI, потом правка `settings.json`. Перед правкой бэкап `<файл>.tk-bak-<время>`. Путь файла разрешается `realpath`; временный файл и бэкап рядом с целью, с теми же правами; rename поверх цели (симлинк не рвётся). Время - аргумент. Манифест `~/.config/tk/connect-state.json`: что `apply` добавил на самом деле; `undo` снимает только это.

| Агент | Путь | Что | Откат |
|---|---|---|---|
| Claude | CLI: `claude plugin marketplace add`, `install tk@tk-local` | `extraKnownMarketplaces`, `enabledPlugins` | `marketplace remove tk-local` |
| Claude | `~/.claude/settings.json` | `permissions.allow += "Bash(tk *)"` | запись манифеста |
| Codex | `~/.codex/hooks.json` | группа `SessionStart`, matcher `startup\|resume\|clear\|compact`, команда `tk ctx` | запись манифеста (группа с точной командой `tk ctx`) |
| Codex | `~/.codex/rules/tk.rules` | `prefix_rule(pattern=["tk"], decision="allow")` | удалить файл |
| Codex, OpenCode | `~/.agents/skills/tk/` | скилл (новый каталог) | удалить каталог |
| OpenCode | `~/.config/opencode/opencode.json` | `permission.bash["tk *"]="allow"` последним ключом (последнее совпадение побеждает); если не разрешено | запись манифеста |
| OpenCode | `~/.config/opencode/plugins/tk.js` | `TRACKER_ACTOR=opencode:<sessionID>` в хуке `tool.execute.before`; проба в начале зоны A; не вышло - стоп и вопрос | запись манифеста |

Не пишем: `config.toml`, `default.rules`, `CLAUDE.md`, `AGENTS.md`. Ручной шаг: одобрить хук Codex в `/hooks`.
Личность: `actor.Resolve` З2 (`CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`).

## 5. Кодовая модель

Кода Go нет. `scripts/connect.sh` (bash, `jq`, `shellcheck`): `connect.sh plan|apply|undo [--agent claude|codex|opencode|all] [--now ISO]`.
- `plan` (по умолчанию) печатает пути и ключи, не пишет.
- `apply`: проверить `tk`, `jq`, `claude`; добавить ключ, если нет, и записать в манифест; порядок чужих ключей сохранить; бэкап; rename.
- `undo`: убрать записи манифеста, потом свои бэкапы и сам манифест. HOME берётся из окружения.

`SKILL.md`: description с `Use when` (задачи, идеи, «что дальше»); цикл `ctx`, `take`, `note`, `check`, `done -m`; `tk` одиночной командой без `;`, `&&`, `|`, `cd` (Codex); вне проекта `tk new` - идея в `inbox`; OpenCode: `tk ctx` первым.

## 6. Этапы реализации

| Зона | Файлы | Проверка |
|---|---|---|
| A коннекторы | `connectors/**` | `claude plugin validate`, `jq empty` |
| B скрипт | `scripts/connect.sh` | `shellcheck`, `bash -n`, тесты T |
| T тесты | `scripts/test-connect.sh`, `connectors/testdata/` (фикстуры без секретов) | красные до B |
| C e2e | `scripts/e2e-agents.sh`, цель Makefile | `bash -n` |
| D документы | `AGENTS.md`, `docs/architecture.md`, `README.md` | `make check` |

1. A-D, T параллельно. Проба в T: работают ли `claude plugin marketplace add` и `install` при временном HOME без входа; нет - тест Claude проверяет `validate` и файлы.
2. `code-reviewer`, `skeptic` по `regress`: идемпотентность, откат, чужие ключи, симлинки.
3. Только с согласия: `make e2e-agents` (тратит лимиты).
4. Только с согласия: `connect.sh apply` на настоящем HOME после `plan`; одобрение хука в `/hooks`.

## 7. Критерий приёмки

- Команда: `make check`; `scripts/test-connect.sh` идёт на `HOME=$(mktemp -d)` и настоящий HOME не читает и не пишет.
- `make e2e-agents` (шаг 3): задача в `ready` с одним критерием, один промпт; задачи в `~/.cache/tk-e2e/<случайное>` (не `/tmp`: у Codex ложно-зелёное). Настройки не пишутся: Claude `claude -p --plugin-dir connectors/claude/plugins/tk --allowedTools "Bash(tk *)"`; Codex с временным `CODEX_HOME` (симлинк `auth.json`, `tk.rules`); OpenCode `opencode run --format json`; скилл обоим в `<фикстура>/.agents/skills`. Каждый агент запускается через `env -u CLAUDE_CODE_SESSION_ID -u CODEX_THREAD_ID -u CODEX_SESSION_ID -u TRACKER_ACTOR`, иначе id запускающей сессии даёт чужой `Actor:`. Второй промпт для одного агента в фикстуре `~/tasks`: «запиши идею X»; pass: файл в `inbox`, проект `personal`, `Actor:` агента. Pass: коммиты `take`, `note`, `done` с `Actor:` `claude:<id>`, `codex:<id>`, `opencode:<id>`; id Claude и Codex равен id из JSON прогона.
- Хук: тест подаёт JSON на stdin, вывод короче 2000 знаков.
- Не автоматизируется: одобрение хука Codex, решение о применении к настоящему HOME. Риск: симлинк `auth.json` не обновит реальный токен; после прогона проверить `~/.codex/auth.json`.
- Не проверяем: Windows, другие агенты.

## 8. Факты (проверено 2026-10-20)

- Плагин Claude: `.claude-plugin/plugin.json`, `skills/`, `hooks/hooks.json`; его `settings.json` только `agent`, `subagentStatusLine` - https://code.claude.com/docs/en/plugins-reference
- `SessionStart`: matcher `startup|resume|clear|compact|fork`, stdout в контекст, до 10000 знаков; `PreCompact` контекст не добавляет, только блокирует - https://code.claude.com/docs/en/hooks
- Локальный маркетплейс: `claude plugin marketplace add <каталог>`, `install имя@маркетплейс`, `marketplace remove` снимает плагины, `--plugin-dir` на сеанс - https://code.claude.com/docs/en/plugin-marketplaces
- `Bash(tk *)` совпадает и с голым `tk`; составная команда по частям - https://code.claude.com/docs/en/permissions
- Codex: `~/.codex/hooks.json`, `SessionStart` с matcher, одобрение в `/hooks` - https://learn.chatgpt.com/docs/hooks ; скиллы `$HOME/.agents/skills` - https://learn.chatgpt.com/docs/build-skills ; `prefix_rule`, `CODEX_THREAD_ID` - `docs/api/codex.md`
- OpenCode: `permission.bash`, побеждает последнее совпадение - https://opencode.ai/docs/permissions  ; скиллы `~/.agents/skills` - https://opencode.ai/docs/skills ; плагины `~/.config/opencode/plugins/` - https://opencode.ai/docs/plugins
- `CLAUDE_CODE_SESSION_ID` есть в окружении Bash (claude 2.1.304). У пользователя `opencode.json` уже `bash "*": "allow"`.

Не знаю (проверка на шагах 1 и 3): id сессии OpenCode в окружении и поля входа `shell.env` (документирован только `cwd`); работает ли `claude plugin install` при временном HOME без входа; видит ли хук Claude `CLAUDE_CODE_SESSION_ID`; пропускают ли песочницы `TRACKER_HOME`; запускает ли `codex exec` неодобренный хук; нужен ли Codex флаг хуков в `config.toml` (`codex features list`, шаг 1).

## 9. Пометка пользователю

Р54 велит хук `PreCompact` с `ctx`, но он контекст не добавляет (раздел 8). План ставит `SessionStart` с `compact`. Текст Р54 не менялся: правка за пользователем.
