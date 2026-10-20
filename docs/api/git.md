# git: факты для хранилища `tk`

Дата: 20.10.2026. Git 2.54.0 (Apple Git-157), macOS, M4, APFS. Замеры: 1000 файлов по 5 КБ, 3000 коммитов, прямой путь `xcrun --find git`, n=25-30, медиана. Скрипты замеров не сохранены (scratchpad сессии), факты - в этом файле. Источники: git-scm.com/docs, Documentation/ в github.com/git/git (master), go-git COMPATIBILITY.md.

## 1. Устройство

- Коммит правки пишет 3 loose-объекта: blob, корневой tree (1000 имён, `cat-file -s HEAD^{tree}` = 52 КБ), commit. Индекс `.git/index` читается и пишется целиком (в `commit --only` 3 записи, trace2).
- Ref - файл `.git/refs/heads/main`; `pack-refs` (зовёт gc) удаляет его и переносит в `packed-refs` (замер). Запись ref атомарна и проверяет старое значение. Reflog: строка на коммит, 90 дней; отключение скорость не меняет (10,84 против 10,62 мс).

## 2. Локи (замер: заранее создан пустой lock, команда запущена; git 2.54.0)

| Команда | index.lock | HEAD.lock | refs/heads/main.lock | packed-refs.lock |
|---|---|---|---|---|
| `status` (в т.ч. `--no-optional-locks`) | ок (необязательный) | ок | ок | ок |
| `add` | ОТКАЗ (128) | ок | ок | ок |
| `commit --only` | ОТКАЗ | ОТКАЗ | ОТКАЗ | ок |
| `write-tree` | ОТКАЗ | ок | ок | ок |
| `hash-object -w`, `merge-file`, `log`, `ls-files`, `rev-parse`, `diff-files` | ок | ок | ок | ок |

- `hash-object -w` пишет через `objects/xx/tmp_obj_*`, лока нет. `push` не мерен.
- `--no-optional-locks`: status не берёт index.lock ради обновления stat-кеша. Ловушка: без записи индекса каждый такой status заново хеширует файлы со сменившимся stat; свежая копия репозитория: `status` 28,9 мс против 5,9 после одного обычного. `commit` индекс обновляет.
- Удаление чужого lock (замер): A держит index.lock (pre-commit hook sleep 2), lock удалён, B сделал `add` и `commit`. A упал: `cannot lock ref 'HEAD': is at X but expected Y`, код 128; `fsck` чистый, коммит A висит, правка в файле. Без коммита B процесс A затёр бы индекс B. Ref защищён проверкой старого значения, индекс нет.

## 3. Сбой

- `kill -9` в случайный момент `commit` (150 попыток): `fsck` 0 ошибок. Остатки: `index.lock` 39 раз, `next-index-<pid>.lock` 12 (оба ловит глоб `.git/*.lock` в `DropGitLocks`), `tmp_obj_*` 17 файлов (чистит gc), dangling blob. Правка осталась в файле.
- Питание (git-config, core.fsync): умолчание `committed,-loose-object`; loose-объекты, refs, индекс не синхронизируются; на macOS `core.fsyncMethod` по умолчанию `writeout-only` (кеш диска не сбрасывается). Возможны потеря последних коммитов и ref на пустой loose-объект. Не воспроизводилось.
- Цена: `core.fsync=all` 11,4 мс (+1,4); с `core.fsyncMethod=fsync` 27,8 (+17,7); `batch` 28,0. Для MVP не включать.
- Безопасно чинить автоматически под flock: старые lock-файлы, `tmp_obj_*`, `rm .git/index` + `git reset -q` (индекс производный). Вручную: битый объект, откат ref по reflog, `gc --prune=now`, `reset --hard`.

## 4. Скорость записи (мс)

| Вариант | Медиана | Вывод |
|---|---|---|
| `commit --only -- файл` (текущий) | 10,6 (p95 11,5) | базовый |
| то же без `maintenance.auto=false` | 15,6 | `commit` запускает `git maintenance run --auto --quiet --detach` и ждёт его (5,0 мс в trace2) |
| то же с `gc.auto=0` без `maintenance.auto=false` | 15,9 | `gc.auto=0` этого не отключает |
| `add` + `commit --only` | 16,4 | |
| plumbing: `update-index`, `write-tree`, `rev-parse`, `commit-tree`, `update-ref` (5 процессов) | 24,6 | |
| plumbing, HEAD читаем из файла (4 процесса) | 21,2 | каждый процесс git 3,0 мс (`--version`) |
| `index v4`, `feature.manyFiles`, `core.untrackedCache` | 11,0 / 10,4 / 11,3 | без выигрыша на 1000 файлах |
| `core.logAllRefUpdates=false` | 10,8 | без выигрыша |

- Разложение 10,6 мс: запуск 3,0, остальное 7,6 (индекс, cache-tree, объекты, ref). `fast-import` пишет pack на каждый запуск, не годится.
- `status --porcelain -z --untracked-files=all` 5,9 (индекс обновлён); `ls-files -m -d -o --exclude-standard` 5,4 (не видит staged); встроенный `core.fsmonitor` 17,5 (хуже).
- `log --grep '^T-12:'`: 17 мс (3000 коммитов, pack), 13 мс с commit-graph. `log -- файл`: 37,5 мс без графа, 28,5 с графом, 8,4 с графом `--changed-paths` (Bloom). `log -1 --grep` (последний коммит задачи) 8 мс.

## 5. Рост (мерено)

- Loose: 3000 коммитов = 9000 loose-объектов = 105 МБ (35 КиБ на коммит, из-за корневого tree). 100 тыс. коммитов loose = около 3,5 ГБ и 300 тыс. файлов.
- После `git gc` (4,1 с на 6000 коммитов): `.git` 109 МБ -> 5,8 МБ. 100 тыс. коммитов, 1000 файлов в pack (fast-import): 90 МБ pack, `.git` 97-104 МБ, ссылки в одном pack.
- Чтения до gc (6000 коммитов, 9000 loose): `log --grep` 84 мс, `log -- файл` 641 мс, `rev-list --count` 106 мс. После gc: 29, 54, 6 мс.
- 100 тыс. коммитов в pack: `log --grep` 0,9 с, `log -- файл` 4,2 с без графа, 0,43 с с `--changed-paths`, 0,66 с с графом от `gc`; `log -1 --grep` 8 мс. Полная пересборка графа 4,7 с. Без `-1`/`-n` поиск медленный.
- `gc.auto=0 maintenance.auto=false` значит: loose-объекты копятся вечно, чтения деградируют в 10 раз. Порог по умолчанию `gc.auto`=6700 loose (документация), около 2200 коммитов. gc параллельно с записью (замер: 1500 loose-коммитов, gc, 33 коммита во время него): 0 отказов, задержка коммита 27 мс (медиана), `fsck` чистый. gc сам берёт `gc.pid`; `pruneExpire` 2 недели защищает объекты параллельных процессов.
- Что делать вместо: свой счётчик в `.tracker/` (каждые 200-500 записей) и после снятия flock отдельный детач-процесс `git gc --quiet`, затем `git commit-graph write --reachable --changed-paths --split` (gc строит граф без Bloom-фильтров, `commitGraph.changedPaths=true` в config этого не исправил, замер: 43 мс против 15 мс после явной записи).

## 6. go-git (v5.19.3, COMPATIBILITY.md, 20.10.2026)

- Есть: commit, status, log, add (без флагов), repack. Частично: merge (только fast-forward), worktree. Нет: gc, индекс v1/v3 (только v2). `merge-file`, reftable, lock-совместимость, commit-graph в документе не упомянуты.
- Замер (те же 1000/3000): открыть + `status` 22,1 мс; + add + commit 57,8 мс против 16-17 мс у git. `fsck` чистый.
- Вывод: не стоит. В 3 раза медленнее, нет merge-file и gc, свой код индекса и локов рядом с системным git.

## 7. Чтение HEAD без git

1. `<dir>/.git` может быть файлом `gitdir: <путь>` (worktree, submodule).
2. `HEAD`: `ref: refs/heads/x` или hex (detached; sha256 даёт 64 знака, замер).
3. Linked worktree: `.git/worktrees/<id>/HEAD`, файл `commondir` (`../..`) указывает общий git dir с refs.
4. Ref: `<common>/refs/heads/x`; нет файла - строка `<sha> <имя>` в `<common>/packed-refs` (после gc файла нет, замер).
5. Reftable (`extensions.refStorage = reftable`): `HEAD` = `ref: refs/heads/.invalid`, refs в `.git/reftable/*.ref`, читать только `git rev-parse HEAD` (3,9 мс). Будущее умолчание Git 3.0 (BreakingChanges.adoc).
6. Ref меняется rename-ом целиком; читать один раз, при сомнении - `git rev-parse`.
7. Текущий `looseHead` (`internal/gitx/gitx.go`) читает только пункты 2 и 4 (loose); `.git` файлом, packed-refs, reftable уходят в `rev-parse`. Скорость теряется только на этих случаях.

## 8. Obsidian Git (вне цели)

Плагин зовёт системный git через simple-git: `status`, `add -A`, `commit` без `--no-optional-locks`, берёт index.lock (src/gitManager/simpleGit.ts, 20.10.2026).

## 9. Готовые возможности git (вторая разведка)

Дата: 20.10.2026, git 2.54.0. Проверка запуском во временных репозиториях, без замеров скорости.
Версии введения - по документации git-scm.com по памяти, со страницей не сверены (кроме 2.54).

| Возможность | Что даёт tk | Git | Проверено |
|---|---|---|---|
| `git commit --trailer "Actor: x"` | трейлеры без ручной склейки сообщения (Р56, Р58) | 2.32 | да |
| `%(trailers:key=Actor,valueonly)` в `log --format` | `Actor:` и `Code:` читаются без разбора текста | 2.22 | да |
| `shortlog -s --group=trailer:Actor` | число правок сессии | 2.29 | да |
| `log --grep A --grep B` (ИЛИ), `--all-match` (И) | история нескольких id, фильтр id и `Actor` | давно | да |
| `log -L ':## Comments:T-1.md'` + `diff.tk.xfuncname '^(## .*)$'` | история одного раздела карточки; атрибуты в `.git/info/attributes` и `.git/config`, не в дереве. Форма `/re/,$` не работает, нужен `:funcname:`; вывод - patch, не список коммитов | 1.8.4 | да |
| `git last-modified` | дата изменения всех файлов одним вызовом вместо `log -1` на файл; нужен запасной путь (mtime) для старого git | 2.52 | да |
| `cat-file --batch` / `--batch-command` | один процесс на много чтений blob; нет объекта - строка `missing` | 2.36 | да |
| `merge-tree --write-tree` | слияние двух коммитов без рабочего дерева (`tk sync`, Р57); конфликт - код 1 и список | 2.38 | да |
| `.gitattributes *.md merge=union`, `merge-file --union` | дописываемые комментарии сливаются без конфликта; union склеивает и тело, порядок строк не гарантирован, при повторе дубли | давно | да |
| `git bundle create/verify` | офлайн-копия и перенос на вторую машину без remote | 1.5.1 | да |
| plumbing `mktree`, `commit-tree`, `update-ref --stdin` | коммит без `index.lock`. Ловушка: индекс устаревает, `status` показывает `D T-3.md` в staged; нужен `read-tree` или `update-index` | давно | да, ловушка найдена |

Не брать: `rerere` (конфликты редки и разные); свой драйвер слияния (внешний бинарь ломает ручной
git пользователя); `git notes` для `Code:` (трейлер проще, notes не едут при push без refspec);
подпись коммитов SSH (одна машина, ключ на агента дорог); author и committer вместо `Actor:` (дубль).

## 10. Атрибуция: окно между rename и commit

- Риск (проверка рисков З1, 20.10.2026): `commit --only -- <файл>` берёт файл из рабочего дерева.
  Запись мимо ядра после rename и до commit уходит в коммит под `Actor:` агента, а не как
  external edit. Данные целы, автор подменён (нарушает Р56).
- Способ починки (З1, `f9e88de`): коммитить ровно байты ядра. `hash-object -w` на файл,
  `update-index -z --index-info` (режим 0 - удаление пути), коммит индекса. Ловушку plumbing из
  раздела 9 обходит: индекс обновлён, `git status` чист. Код - `gitx.CommitFiles`
  (`internal/gitx/gitx.go`), тест `TestStore_ExternalWriteBetweenRenameAndCommit_KeepsAuthor`,
  проверено тестом и живым прогоном. Вариант `update-index --cacheinfo` не выбран.
- Остаточный риск S1: чужой `git add` между `update-index` и `commit` уйдёт под `Actor:` агента.
  Решение по S1 за пользователем.

## Что значит для tk

- `commit --only` одним процессом лучший; plumbing не ускоряет.
- `maintenance.auto=false` нужен ради скорости, но нужна своя сборка: gc и граф `--changed-paths`.
- Авточистка lock-файлов и `tmp_obj` безопасна; fsync не включать; go-git не брать.
- Трейлеры - `commit --trailer` и `%(trailers)` (З2). Даты - `last-modified`, история раздела -
  `log -L` (З4). После MVP для `tk sync`: `merge-tree --write-tree`, `merge=union`, `bundle`.
