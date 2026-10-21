# Спека: З4 TUI

Статус: agreed
Каноны: `docs/spec.md` (2.3, 7), `docs/specs/decisions.md` (Р9, Р26, Р27, Р34, Р38, Р45, Р60), `docs/plans/z2-agent-cycle.md`, `docs/api/git.md`, `docs/mockup/ui.html`

MVP: рамки уточняются после пробной эксплуатации. Строки интерфейса на английском (Р46).

## 1. Цель и границы

- Цель: `tk` без аргументов открывает доску и карточку с историей.
- Результат: клавиши 2.3, перенос со строкой ввода, порядок, фильтр по проекту, `$EDITOR` с повтором при конфликте, жёлтая звёздочка `ask`, живое обновление.
- Не делаем: тема `cards`, ответ агенту, правка полей (Р45), клавиша `s`.
- Зависимость: З2 в `main` (`Move`, `actor`, `Criteria`, `ClaimInfo`). Перестановку `Move` с `Place` в своей колонке (с коммитом) закрепляет З2 (зона T З2); `J`, `K` на ней стоят.

## 2. Архитектура

Поток: клавиши и fsnotify - модель `tui` - чтение `Store.List/Get`, запись очередью (актор `human`) - `view` рисует. История - `history` через `gitx.Log`; редактор - `tea.ExecProcess`.

Зависимости: `cmd/tk` - `tui`, `cli`; `tui` - `view`, `history`, `core`, `task`, `config`, `actor`; `history` - `gitx`.

Решения задачи:
- Bubble Tea v2.1.0, Lip Gloss v2.0.6, fsnotify, teatest; без Bubbles - строка ввода своя, 30 строк (Р44). Модель системы: нет
- Запись только через `core`; одна очередь записи (буфер 16, переполнение - `queue full`), экран не ждёт лок. Модель системы: нет
- Доска читает `Store.List()` (без git, ~11 мс на 1000 задач); история - при открытии карточки, в фоне. Модель системы: нет
- Дата изменения в карточке - первая запись истории, запасной путь mtime; `git last-modified` и `git log -L` в MVP не берём. Модель системы: нет
- Конфликт `$EDITOR`: ядро отдаёт `ConflictError{Rev, Merged}`; редактор открывается на `Merged` с базой `Rev`. Модель системы: нет
- Колонка вне конфига - `?` в конце. Колонки по 24 знака, не влезли - окно за курсором

## 3. Сценарии

| Сценарий | Шаги | Ожидание |
|---|---|---|
| Просмотр | `l`, `j`, `enter`, `esc` | колонки по конфигу; карточка с историей; выбор сохранён |
| Пусто, битый файл | пустая папка; файл не разбирается | пустые колонки; `1 file skipped` в строке состояния |
| Перенос | `m`, `M` | `Move` в соседнюю колонку; `ErrRule` - текст в строке состояния |
| В `waiting` | `m`, поле пусто | строка `waiting for:`; `enter` - `Move` с `-m`; `esc` - отмена |
| В `closed` | неотмеченные критерии; критериев нет; все отмечены | строка `close reason:` и `Force`; строка и обычный `-m`; без вопроса |
| Порядок | `J`, `K` | `Move` в той же колонке: `After` следующей или через одну, у верха `First`; на краю ничего |
| Фильтр, комментарий | `p`; `c`, текст | проекты кругом; `Comment`, пустой текст - отмена |
| `$EDITOR` | выход без правки; правка при параллельном комментарии агента | копия удалена, коммита нет; `Edit` с базой, слияние чистое |
| Конфликт, отказ записи | та же строка правится параллельно; `ErrLocked`, `ErrRule` | редактор с маркерами; без правки - отмена; копия и путь в строке, ошибка оранжевым |
| Живое обновление | `tk new` в другом окне; правка Obsidian | через 0,3 с на доске; выбор по id; карточка обновляется |
| Задача удалена | `tk rm` открытой | доска, `T-12 removed` |
| `ask` | поле не пусто | жёлтая `*` в строке и `* needs you: <ask>` в карточке |
| Паника, ресайз, не терминал | паника в `Update`; окно меняется; `tk` в пайпе | терминал восстановлен, ошибка в `.tracker/log`, код 1; перерисовка; как сейчас `cli.Run` |

## 4. Данные и состояния

- Выбор - (колонка, id), не индекс. Перечитывание по событию целиком и идемпотентно, события склеивает окно 150 мс; fsnotify смотрит корень.
- Копия редактора: `.tracker/tmp/ed-<id>-<n>.md`, 0600; удаляется при успехе и отмене, при отказе остаётся (`cleanTmp` ядра трогает только префикс `w-`).
- История: `git log --grep '^T-12: '`, формат `%H %aI %s` и `%(trailers:key=Actor,valueonly)`. Тип: `Actor: external` - `external edit`, иначе слово после `T-12: ` (в коммите `external edit` несколько id); незнакомый тип - строка как есть.
- Комментарии берутся из `t.Comments` (время, автор, текст по маркерам) и встают в историю по времени. Коммиты `comment` в историю не идут: `done -m` пишет комментарий коммитом `done`.
- Всегда видны: `new`, `take`, `steal`, `move`, `done`, `note`, комментарии. Сворачиваются: `set`, `check`, `edit`, `release`, `external edit`: от двух подряд одного `Actor` - `N edits`, `enter` раскрывает (Р60); комментарий разрывает серию.
- Копия с маркерами `<<<<<<<` не пишется: отказ, копия цела, путь в строке.

## 5. Кодовая модель

```go
// internal/gitx/history.go (H)
type Commit struct{ Hash string; At time.Time; Subject, Actor string }
func (r Repo) Log(id string) ([]Commit, error) // --no-optional-locks; нет коммитов - пусто

// internal/history (H)
type Entry struct{ At time.Time; Actor, Op, Text string; Folded []Entry } // Folded - строка "N edits"; Op comment - из файла
func Load(r gitx.Repo, t *task.Task) ([]Entry, error) // Log без коммитов `comment`, плюс t.Comments, по времени, Fold
func Fold(es []Entry) []Entry

// internal/core/conflict.go (H); mergeBody возвращает его вместо голого ErrConflict
type ConflictError struct{ Rev, Merged string } // Is(ErrConflict); Error() прежний (код 3 в CLI); Merged с маркерами

// internal/tui/view (V): без IO
type Row struct{ ID, Project, Title, Summary, Meta string; Star bool }
type Col struct{ Name string; Rows []Row }
type Board struct{ Project string; Total int; Cols []Col; Col, Row, W, H int; Status string; IsErr bool }
type Card struct{ ID, Title, Column, Fields, Ask, Abstract, Summary, Details, Checks string
	History []history.Entry; Cursor, Scroll, W, H int; Status string; IsErr bool }
func RenderBoard(b Board) string
func RenderCard(c Card) string
func RenderInput(prompt, text string, w int) string

// internal/tui (M)
type Deps struct{ Store *core.Store; Root, Actor, Project string; Log *slog.Logger } // Project - начальный фильтр (папка проекта), пусто - все
func Run(ctx context.Context, d Deps) error // View.AltScreen; паника - ошибка в Log

// internal/cli/open.go (M): код app.open, app.open его зовёт
func OpenStore(env Env) (*core.Store, *slog.Logger, string, error) // база, лог, проект папки
```

`cmd/tk`: нет аргументов и stdout - терминал (`os.Stdout.Stat()`, `ModeCharDevice`) - `tui.Run`, актор `human` (2.4); иначе `cli.Run`.

Правила `tui`:
- Клавиши: `h j k l`, `enter`, `m M`, `J K`, `p`, `e`, `c`, `esc`, `q`. В карточке `j k` ходят по истории.
- Запись - `tea.Cmd` через очередь, результат `opDone{err}`, затем перечитывание. Ошибки: `errors.Is` для `ErrRule`, `ErrLocked`, `ErrConflict`, `ErrNotFound`.
- Редактор: `$VISUAL`, `$EDITOR`, `vi`; делится по пробелам, без оболочки. Тело `t.Body`, база - `rev` из `Get` до запуска.
- Рисуются только строки, влезающие в экран; наблюдатель стартует после первого кадра (kqueue держит дескриптор на файл). Горутины (очередь, наблюдатель): владелец `Run`, стоп по `ctx`, `recover` в лог.

## 6. Этапы реализации

Зона 0 и M стартуют после мерджа З2 (правят `cli.go`, `cmd/tk`); V и H можно раньше. Зона 0 - один исполнитель: зависимости в `go.mod` (версии сверить `go list -m -versions`), заглушки раздела 5. Затем V, H, M, T параллельно.

| Зона | Файлы | Зависит от | Проверка |
|---|---|---|---|
| 0 Каркас | `go.mod`, заглушки | З2 в `main` | `go build ./...` |
| V вид | `internal/tui/view/` | 0 | `go build`, тесты T |
| H история | `internal/gitx/history.go`, `internal/history/`, `internal/core/conflict.go`, `mergeBody` | 0 | то же |
| M модель | `internal/tui/*.go`, `cmd/tk/main.go`, `internal/cli/` | 0 (V, H) | `go build -o bin/tk ./cmd/tk` |
| T тесты | `*_test.go`, `testdata/`, `e2e/tui_test.go`, `cmd/tkbench/tui.go`, заглушка `EDITOR` | 0, раздел 3 | `go vet`; сначала красные |
| Документы | `docs/architecture.md`, `AGENTS.md`, `README.md` | V, H, M, T | `mermaid-lint.sh` |

T: вид - golden; модель - `teatest` (`NewTestModel`, `Send`, `WaitFor`, `FinalModel`) на реальном `Store` во временной папке; конфликт - скрипт-заглушка `EDITOR`. Замер: `TRACKER_TUI_ONESHOT=1` пропускает проверку терминала, рисует первый кадр, выходит; `tkbench` запускает с выводом в пайп.

## 7. Критерий приёмки

- `make check` и `make bench`: код 0; `tui open` до 100 мс на 1000 задач (медиана).
- Не автоматизируется: вид TUI и правка в Obsidian - пользователь глазами, один раз.

## Непроверенное

- Bubble Tea без терминала (`WithInput(nil)`) рисует кадр; иначе замер через `script`.
- fsnotify на macOS (kqueue) видит запись на месте (Obsidian): тест T решает, нужна ли сверка mtime раз в 2 с.
- go.dev: Bubble Tea v2.1.0 (08.10.2026); Lip Gloss v2.0.6 и fsnotify v1.10.1 - новее есть, номер не выяснен. teatest v2 - только псевдоверсия `v2.0.0-20261018003744-cb897cb4ed72`, она встала с Bubble Tea v2.1.0 (`go get` в пробном модуле).
