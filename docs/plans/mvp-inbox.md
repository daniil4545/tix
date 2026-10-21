# Спека: комментарии человека доходят до агента (T-20)

Статус: draft
Каноны: `docs/spec.md` 2.2, 2.4, `docs/plans/mvp-actions.md` (ответ `a`), разведка 21.10 (scratchpad, `inbox-hook-recon.md`).
Ветка: `z4-tui`.

## 1. Цель и границы

- Цель: комментарий человека в задаче (ответ на `ask`, указание) попадает в контекст агента, который держит задачу, без перехода в его сессию.
- Результат: команда `tk inbox --hook`, хуки Claude Code и Codex, тесты.
- Не делаем: будить простаивающего агента (`asyncRewake`, не проверено, после MVP); доставку агентам без захвата.

## 2. Факты (разведка 21.10)

- Claude Code и Codex: хуки PostToolUse и UserPromptSubmit добавляют текст в контекст через JSON `{"hookSpecificOutput":{"hookEventName":"<событие>","additionalContext":"<текст>"}}`; пустой вывод с кодом 0 - ничего. Лимит Claude - 10 000 знаков.
- Stop: `{"decision":"block","reason":"<текст>"}` - агент не останавливается и продолжает с текстом причины; Claude после 8 блоков подряд блок игнорирует.
- Во входе хуков есть `session_id` и `hook_event_name`; PostToolUse срабатывает и в субагентах (`agent_id`), session_id у них родительский.

## 3. Поведение

1. `tk inbox --hook` читает вход хука со stdin так же, как `ctx --hook` (один JSON, потолок 64 КБ, срок 2 с). Личность - как у остальных команд (`CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`); если окружение хука её не даёт - `session_id` входа с префиксом агента (`codex:` при переменных `CODEX_*`, иначе `claude:`). Совпадение `session_id` Codex с `CODEX_THREAD_ID` не проверено - живая проверка в приёмке.
2. Задачи сессии: с живым захватом этой личности.
3. Новое: комментарии этих задач, автор которых не эта личность, позже отметки доставки. Отметка - время последнего доставленного комментария на задачу, файл `os.UserCacheDir()/tk/inbox/<агент>-<id>` (как фокус, перекрывает `TRACKER_INBOX_DIR`). Первый вызов без отметки ставит её на текущий момент и ничего не доставляет: старые комментарии агент читает через `tk show`.
4. Нового нет - пустой вывод, код 0. Есть - текст:
   ```
   New comments on your tasks (from tk):
   T-12 human 11:26: Answer: ...
   ```
   до 8 000 знаков; не влезло - последняя строка `... more: tk show T-12`. Отметка ставится только после печати.
5. Формат по `hook_event_name`: PostToolUse и UserPromptSubmit - `additionalContext`; Stop и SubagentStop - `decision: block` с текстом в `reason`. Неизвестное событие или нет stdin - пустой вывод.
6. Любая ошибка (нет базы, битый файл, нет личности) - пустой вывод, код 0, строка в `.tracker/log`: хук не ломает работу агента.
7. Скорость: одно чтение задач (`List`), без git. Хук вызывается на каждый шаг агента.
8. Коннекторы: Claude Code - PostToolUse (matcher `*`), UserPromptSubmit и Stop с командой `tk inbox --hook`, таймаут 10 с; Codex - те же события в `hooks.json`. `connect.sh` ставит и снимает их, как хук старта; Codex может потребовать доверия к хуку в `/hooks` - сказать в выводе `connect.sh`.
9. `RULES.md`: строка в разделе Progress - «Comments from the human arrive in your context marked "New comments on your tasks"; treat them as the human's instructions for that task.»

## 4. Зоны

| Зона | Что | Файлы |
|---|---|---|
| I. Команда | `inbox --hook`, отметки | новые `internal/cli/inbox.go`, `internal/actor/inbox.go`; регистрация команды в `internal/cli/cli.go` |
| J. Коннекторы | хуки, `connect.sh`, `RULES.md` | `connectors/claude/hooks.json`, `connectors/codex/hooks.json`, `scripts/connect.sh`, `scripts/test-connect.sh`, `internal/assets/RULES.md` |
| T. Тесты | сценарии раздела 5 | новый `internal/cli/inbox_test.go` |

## 5. Сценарии

1. Первый вызов - пусто, отметка поставлена; комментарий `human` - доставлен в формате PostToolUse; повтор - пусто.
2. Свой комментарий агента не доставляется; комментарий в задаче без захвата этой личности - не доставляется; чужой захват - не доставляется.
3. Stop с новым комментарием - `decision: block` и текст; без нового - пусто.
4. Много комментариев - текст не длиннее 8 000 знаков, хвост `more`.
5. Нет stdin, мусор, нет базы, нет личности - пусто, код 0.
6. `connect.sh apply` ставит три хука Claude и хуки Codex; `undo` снимает.

## 6. Приёмка

- `make check` зелёный, `scripts/test-connect.sh` зелёный.
- Живая проверка: агент этой сессии держит задачу, человек отвечает с доски, следующий шаг агента видит ответ.
