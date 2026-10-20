---
system: Codex CLI
version: codex-cli 0.163.0
checked_at: 2026-10-20
checked_by: recon (живой codex exec + codex sandbox + документация)
---

# Codex CLI: песочница, правила, id сессии

Источники: [L] живой `codex exec` 20.10.2026 с временным CODEX_HOME; [S] `codex sandbox`; [D] learn.chatgpt.com/docs/agent-configuration/rules (страница без даты, прочитана 20.10.2026; developers.openai.com/codex/rules редиректит туда).

| Факт | Источник | Для tk |
|---|---|---|
| workspace-write по умолчанию пишет в рабочую папку, /tmp и $TMPDIR (policy: exclude_slash_tmp=false, exclude_tmpdir_env_var=false). Запись вне них: `Operation not permitted` | [L] rollout sessions, [S] | ~/tasks в этот список не входит: запись и коммит из песочницы упадут |
| `.git` внутри даже рабочей папки read-only: `git add` падает `Unable to create .git/index.lock: Operation not permitted` | [S] с exclude_slash_tmp=true, рабочая папка в ~/.cache | git-коммиты tk без выхода из песочницы невозможны нигде, кроме /tmp |
| Проверка только в /tmp ложно-зелёная: запись и коммит во внешний репо в /private/tmp прошли в песочнице | [L] | Тесты tk не делать во временных каталогах /tmp |
| `prefix_rule(pattern=["tk"], decision="allow")` в `<CODEX_HOME>/rules/default.rules`: `codex execpolicy check --rules F -- tk add x` даёт decision allow | [L] | Правило парсится и совпадает по префиксу `tk` |
| allow = команда идёт вне песочницы без запроса; prompt спрашивает; forbidden блокирует; при нескольких совпадениях побеждает строгое. `bash -lc` режется на команды только для простых цепочек, иначе проверяется целиком | [D] | Вызывать `tk ...` отдельной простой командой; `cd x && tk` и пайпы могут не совпасть |
| ОПРОВЕРГНУТО документацией: при approval_policy=never правило allow для `tk` НЕ выводит команду из песочницы. Живой прогон 20.10.2026, цель в ~/.cache: контроль без tk отказ (`operation not permitted`, `.git/index.lock: Operation not permitted`, commit_exit=128); `tk add` с правилом allow тоже отказ (tk_exit=128), внутри заглушки `CODEX_SANDBOX=seatbelt CODEX_SANDBOX_NETWORK_DISABLED=1`, т.е. запущена внутри песочницы; в репо остался только init | [L] | prefix_rule allow сам по себе схему хранения не спасает при never. Не проверено: on-request/эскалация от модели (`require_escalated`), writable_roots, danger-full-access |
| Окружение команд сессии: CODEX_THREAD_ID (UUID, 36 симв., `XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX` hex), CODEX_SESSION_ID (то же значение по префиксу), CODEX_SANDBOX=seatbelt, CODEX_SANDBOX_NETWORK_DISABLED=1, CODEX_CI=1, CODEX_HOME, CODEX_VERSION, CODEX_TOOL_CALL_ID. Он же thread_id в событии `thread.started` | [L] | id сессии tk брать из CODEX_THREAD_ID (или CODEX_SESSION_ID) |
| Сеть в песочнице выключена (CODEX_SANDBOX_NETWORK_DISABLED=1) при network_access=false | [L] | tk не должен ходить в сеть из песочницы |
| Правила: файлы `*.rules` в `rules/` рядом с активным слоем конфига (`~/.codex/rules/default.rules`); проектные `<repo>/.codex/rules/` только в доверенном проекте. Синтаксис `prefix_rule(pattern=[...], decision=..., justification=..., match=[...], not_match=[...])` (Starlark). Помечено experimental | [D] | setup дописывает строку в default.rules идемпотентно |
| Подтверждение: правило, добавленное через TUI («allow list»), пишется в `~/.codex/rules/default.rules`; при Smart approvals Codex предлагает prefix_rule при эскалации | [D] | Ручной файл подхватывается при старте без подтверждения (кроме `codex exec` без TUI: подтверждение не нужно, проверено живым прогоном с правилом из файла) |
| Другой CODEX_HOME: `CODEX_HOME=dir codex ...` читает config.toml, rules/ и auth.json оттуда; для прогонов без правки ~/.codex достаточно symlink auth.json | [L] | Тесты интеграции изолируются через CODEX_HOME |
| `-c key=value` значение TOML: `sandbox_mode="workspace-write"` нужны кавычки | [L] | |
| Модель gpt-5.5 с ChatGPT-аккаунтом отвергается (400); рабочая gpt-6.1-sol | [L] | Не критично для tk |

## Дополнение 20.10.2026: запись в ~/tasks (recon codex-write)

Источники: [S] `codex sandbox` 0.163.0; [C] исходники тега rust-v0.163.0 (`codex-rs/`). Полный отчёт: `docs/research/codex-write.md`.

| Факт | Источник | Для tk |
|---|---|---|
| `codex sandbox` 0.163 требует `-P <профиль>` вместе с `-C`; без `-C` берёт `-c 'sandbox_mode="workspace-write"'` (старый режим) или `default_permissions` | [S] | Тесты песочницы запускать без `-C`, из рабочей папки |
| Защищённые имена: `.git`, `.agents`, `.codex`, `.aws` (read-only в рабочей папке и в каждом writable root); `.git` файл-указатель защищает и каталог из `gitdir:` | [C] protocol/src/permissions.rs:40-43, 860, 2360-2403 | `.git` под этим именем закрыт всегда |
| Явная запись для пути снимает защиту: `[permissions.x.filesystem] "<repo>" = "write"` и `"<repo>/.git" = "write"` дают коммит | [C] permissions.rs:2502; [S] `COMMIT_OK` | Профиль `permissions` с `extends=":workspace"` открывает ~/tasks, `.git/hooks` тоже пишется |
| `sandbox_workspace_write.writable_roots=[repo]`: файлы пишутся, `.git/index.lock` Operation not permitted | [S] | Само по себе для коммита не годится |
| Каталог git с другим именем (`<repo>/.tkgit`) без `.git` в корне + `GIT_DIR`/`GIT_WORK_TREE`: коммит проходит в `writable_roots` и в профиле; с файлом `.git`-указателем падает | [S] | Обход по имени работает |
| Allow-правило по коду снимает песочницу, если каждый сегмент команды совпал с правилом, при on-request и never; тесты exec_policy_tests.rs:1865, 1896. Исключение: политика с `deny`-чтением | [C] exec_policy.rs:440-456, tools/sandboxing.rs:249-295 | Прежнее «ОПРОВЕРГНУТО» выше вызвано командой `tk add x ; echo ...` (второй сегмент без правила); не перепроверено живьём |
| `codex sandbox --allow-unix-socket <путь>` пускает к unix-сокету; без флага отказ. Ключ профиля `network.unix_sockets` не сработал | [S] | Возможен демон вне песочницы |
| ПОДТВЕРЖДЕНО живьём 20.10.2026: одиночная команда `tk add x` при workspace-write, approval_policy=never и `prefix_rule(["tk"], "allow")` выполнилась вне песочницы: `CODEX_SANDBOX` пуст, коммит в репозиторий в ~/.cache прошёл. Строка «ОПРОВЕРГНУТО» выше получена на составной команде `tk ...; echo` | [L] | Способ C рабочий: скилл велит звать `tk` одиночной командой, без `;`, `&&`, пайпов и `cd` |
