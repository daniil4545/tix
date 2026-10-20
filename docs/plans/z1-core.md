# Спека: З1 Ядро записи и базовый CLI

Статус: agreed
Каноны: `docs/spec.md` (2-2.2, 2.5, 3, 5, 7-9), `docs/backlog.md` (З1), макет `docs/mockup/ui.html`

## 1. Цель и границы

- Цель: единственный путь записи в `~/tasks` и семь команд `tk`.
- Результат: `docs/backlog.md`, З1 (семь команд, external edit, `README.md`, `AGENTS.md`, `Makefile` с `check` и `bench`).
- Не делаем: `ctx`, `take`, `move`, `set`, `check`, `done`, `backup`, TUI, коннекторы, цепочку личности 2.4. Этап 0 выполнен.

## 2. Архитектура

Зависимости: `cli` - `core`, `project`; `core` - `task`, `gitx`, `config`.

Решения задачи:
- Прямой путь к git (`TRACKER_GIT`, кеш в `os.UserCacheDir()` через temp и rename, на darwin `xcrun --find git`): заглушка `/usr/bin/git` +5,5 мс на вызов, запись 30,2 против 18,6 мс; отброшено: заглушка (46 мс с external edit), кеш в `.tracker/`; заденет: З2, З4 зовут git через `gitx`; пересмотреть при ENOENT; модель: нет
- Чтение без git: `list` (и `ctx` З2) git не зовут, `show` - один `hash-object -w` (3,1 мс, база для `edit`); `status` 5-11 мс при бюджете 15; отброшено: `status` на чтении, кеш индекса (выигрыш 4 мс); пересмотреть при 3000+ задач; модель: нет
- Разбор в `min(4, NumCPU)` горутин, файл целиком: 10,8 мс на 1000 файлов против 24 последовательно; отброшено: первые 700 байт (выигрыш 0,8 мс); заденет: З4 (`core.List`); пересмотреть, если `list` не держит 15 мс (тогда 25 мс - с согласия пользователя); модель: нет
- Дата изменения не в `list` и `show` (2.1: из git, но `git log -- файл` 32 мс на файл): `updated` во frontmatter нет (раздел 5), З1 дату не печатает, поведение то же; отброшено: mtime, `log` на чтении; заденет: З4 берёт `git log -1 --format=%ct` в карточке; пересмотреть, если дата нужна на доске; модель: нет
- Слаг (макет): транслитерация кириллицы (ж-zh, х-kh, ц-ts, ч-ch, ш-sh, щ-shch, ю-yu, я-ya, ъ и ь опущены), строчные; всё вне `a-z0-9` - `-`, повторы схлопнуты, края обрезаны; до 40 байт; пустой - `task`; модель: нет
- Бюджет записи: `Sync` +3,9 мс, `add` +3, `rev-parse` +2,4, итого около 28 при цели 50. `rank` - `float64`, `new` ставит `max+1`. Без `--as` актор `unknown`. Модель: нет
- Выбор агента: `config.toml` (`[[column]] name, role`, `[projects]` папка = имя); `list` по колонкам и `rank`, фильтры `--project`, `--status`; битый файл - `warn` в stderr, код 0; пустая база - `нет задач`

## 3. Сценарии

Раздел 3 спеки, тесты - чёрный ящик.

| Сценарий | Шаги | Ожидание |
|---|---|---|
| Основной | `new`, `show`, `edit`, `comment` x2, `note`, `rm` | коммит на каждую: `T-N: <операция>`, `Actor:`; первый с `.gitignore`, `config.toml`, `next_id` |
| Два агента | 5 процессов по 20 `comment` в одну задачу, лок 30 с | 100 комментариев, 100 коммитов, нет `.tmp` |
| `edit`, старая база | тело изменилось после `show` | разные места - слияние; те же строки - отказ, файл цел |
| Obsidian | правка и новый файл (кириллица) мимо ядра; `git checkout` удалённого | следующая операция: коммит external edit, `Actor: external` |
| Лок занят | `TRACKER_LOCK_TIMEOUT=200ms`, лок занят | `err` одной строкой |
| `index.lock` | файл остался | удалён под локом, в лог `WARN op=drop_index_lock` |
| `kill -9` | обёртка git замирает на `commit` | нет полузаписанных файлов, `git fsck` чист, правка уходит external edit |
| Коммит упал | обёртка git падает на `commit` | `ok`, stderr `warn T-N: коммит не записан`, код 0 |
| Сломанный файл или конфиг | не разбирается | файл не изменён, ошибка `путь:строка` |
| Пустой `~/tasks` | `list`, затем `new` | одна строка; `new` создаёт папку |
| Ошибки ввода | нет задачи; `--to` неизвестной колонки; `## Комментарии` или пустой stdin в `edit`; плохой `--as` | `err`, код 1, файлы целы |

## 4. Данные и состояния

Модель - раздел 8 спеки. Заметка - пустой коммит `T-N: note <текст>`, `next_id` - в коммите задачи. Время локальное: `created` `2006-01-02T15:04:05`, маркер `<!-- c: 2006-01-02T15:04 | автор -->`, `-->` в тексте экранируется. `edit` без изменения тела - без коммита.

## 5. Кодовая модель

Модуль `tracker`. Сигнатуры - контракт зон. У методов `gitx`, `core` опущен `ctx context.Context`.

```go
// internal/task: yaml.v3 Node, без IO
type Comment struct{ At time.Time; Actor, Text string }
type Task struct { // json: имена frontmatter, rev, body, comments
    ID, Title, Project, Status, External, Waiting, Claim, Due, Ask string
    Rank float64; Created time.Time; Tags []string
    Body string // до "## Комментарии"
    Comments []Comment
    Rev, Path string // Rev: blob id из Go
}
func Parse(path string, data []byte) (*Task, error) // ошибка: путь и строка
func (t *Task) Render() []byte
func (t *Task) SetBody(body string) error           // "## Комментарии" в теле - ошибка
func (t *Task) AddComment(at time.Time, actor, text string)
func (t *Task) Summary() string                     // 1-я строка сводки
func FileName(id, title string) string

// internal/config: .tracker/config.toml
type Column struct{ Name, Role string }
type Config struct{ Columns []Column; Projects map[string]string }
func Default() *Config
func Load(path string) (*Config, error)             // нет файла - Default
func (c *Config) Render() []byte

// internal/gitx: единственный os/exec
type Repo struct{ Dir, Bin string }
type Change struct{ Path, Code string }
func Resolve(override string) (string, error)
func (r Repo) Init() error
func (r Repo) Status() ([]Change, error)            // --porcelain -z --untracked-files=all
func (r Repo) Commit(paths []string, subject, actor string) (string, error) // add -A -- paths, commit --only -- paths, хеш; пусто - `--allow-empty --only`
func (r Repo) HashObject(data []byte) (string, error)
func (r Repo) Blob(rev string) ([]byte, error)
func (r Repo) MergeFile(ours, base, theirs []byte) (out []byte, conflict bool, err error)
func (r Repo) Remove(path string) error
func (r Repo) DropGitLocks() (bool, error)

// internal/project: без git
func Detect(cwd, root string, c *config.Config) (string, bool) // false: в root или вне git

// internal/core: пишет только он
var ErrConflict, ErrLocked, ErrNotFound error
type Options struct{ LockTimeout time.Duration; GitBin string; Log *slog.Logger }
type Result struct{ ID, Rev, Commit, Warning string } // Warning: коммит упал
func Open(root string, o Options) (*Store, error)   // читает конфиг
func (s *Store) Config() *config.Config
func (s *Store) Create(title, project, column, actor string) (Result, error)
func (s *Store) Edit(id, baseRev, body, actor string) (Result, error)
func (s *Store) Comment(id, text, actor string) (Result, error)
func (s *Store) Note(id, text, actor string) (Result, error)
func (s *Store) Remove(id, actor string) (Result, error)
func (s *Store) Get(id string) (*task.Task, string, error) // + HashObject -w; отказ git - warn
func (s *Store) List() ([]*task.Task, []error)      // без git

// internal/cli
type Env struct{ Stdin io.Reader; Stdout, Stderr io.Writer; Getenv func(string) string; Cwd string }
func Run(ctx context.Context, args []string, env Env) int
```

`core`: алгоритм 2.1 с `Sync` перед `rename`; `Edit` с базой не из git - `err` «перечитай `tk show`». `cli` читает `TRACKER_HOME` (иначе `~/tasks`), `TRACKER_LOCK_TIMEOUT` (5 с), `TRACKER_GIT` через `Env.Getenv`. Актор `--as`: без `|`, `-->` и переводов строки.

Ответ stdout (`-v` добавляет хеш коммита):
- `new <название> [--to колонка] [--project p]`: `ok T-23 idea` (колонка)
- `edit <id> --base <rev>` (stdin), `comment|note <id> <текст>`, `rm <id>`: `ok T-12 edit|comment|note|rm`
- `show <id> [--json]`: `rev <blob>`, затем файл как есть; JSON - поля Task
- `list [--project p] [--status колонка] [--json]`: `<колонка> T-12 <название> | <сводка>`; JSON - Task без `body`, `comments`

`list` без `--project`: проект из `Detect`, если `ok`, иначе все; `new`: `Detect`, иначе `личное`. Ошибка: stderr `err T-12: <причина>`, код 1, без вывода git. `Run` ловит панику; ошибки - в `.tracker/log` (`slog`: `cmd`, `id`, `err`), без `.tracker/` - только stderr.

## 6. Этапы реализации

Зона 0 (один исполнитель), затем A-D и T одновременно, затем `make check` и `bench`.

| Зона | Файлы | Зависит от | Локальная проверка |
|---|---|---|---|
| 0 Каркас | `go.mod` (yaml.v3, TOML с номером строки), `Makefile`, `.golangci.yml`, `README.md`, `AGENTS.md` («Структура», «Команды»), заглушки по разделу 5 | - | `go build ./...` |
| A данные | `internal/task/`, `internal/config/` | 0 | `go build`, тесты T |
| B git, замер | `internal/gitx/`, `internal/project/`, `cmd/tkbench/` | 0 | то же |
| C ядро | `internal/core/` | 0 (сигнатуры A, B) | `go build ./...`, тесты T |
| D CLI | `internal/cli/`, `cmd/tk/` | 0 (сигнатуры C, B, A) | `go build -o bin/tk ./cmd/tk` |
| T тесты (файлы зон не пересекаются) | только `*_test.go`, `testdata/`, `e2e/`; `HOME`, `TRACKER_HOME` во временной папке, `GIT_CONFIG_GLOBAL=/dev/null`, личность очищена | 0 (спека и сигнатуры, код не читает) | `go vet ./...`; сначала красные |

## 7. Критерий приёмки

- `make check`: `gofmt -l`, `go vet`, `golangci-lint`, `go test -count=1 ./...` (гонка и `kill -9` вне `-short`), сборка; код 0.
- `make bench`: собирает `bin/tk`, создаёт 1000 задач во временном `TRACKER_HOME`, 30 прогонов после прогрева, медиана; код 0, если цели выполнены:

Цели, мс (медиана; замер этапа 0): `list` 15 (12, оценка), `show` 15 (7), `comment` 50 (28), `comment` после правки мимо ядра 50 (40, оценка).

Цели раздела 7 не меняются (замер на M4, горячий кеш). Не автоматизируется: вид Obsidian (З4). Не проверяем: холодный кеш, Codex (З3).

## Вопросы пользователю

Нет.
