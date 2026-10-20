# Спека: З2 Рабочий цикл агента

Статус: agreed
Каноны: `docs/spec.md` (2.2, 2.4, 2.5, 3, 5, 7, 8, 11, Р36-Р41, Р46, Р48, Р51, Р52), `docs/backlog.md`, `docs/plans/z1-core.md`, `docs/api/codex.md`, `docs/mockup/ui.html`

## 1. Цель и границы

- Цель: агент проходит happy path раздела 3 командами `tk`.
- Результат: работают `ctx`, `take`, `move`, `set`, `check`, `done`, `backup`, личность, `claim`, проверки 2.5, коды выхода; созданы `RULES.md` с шаблоном тикета, `docs/architecture.md`.
- Не делаем: TUI, скилл и хуки, фоновый бэкап, снятие отметки.
- Зависимость: З1 в `main` (код З1 уже на английском, Р46).

## 2. Архитектура

Зависимости: `cli` - `core`, `project`, `actor`, `brief`; `core` - `task`, `gitx`, `config`, `assets`, `actor`; `brief` - `task`, `config`. Новые: `actor` (личность, единственный `Valid`), `brief` (сборка `ctx`), `assets` (`RULES.md`).

Решения задачи:
- `claim: <actor> until 2006-01-02T15:04`; истёкший свободен и в `ctx` не показан; модель: нет
- Продление - шаг `modifyOnce` З1: запись владельца (актор совпал, срок не важен), только если `mutate` изменил файл, ставит now+3 ч в том же коммите; `note` не продлевает; модель: нет
- Повтор: операция без изменения файла - `ok` без коммита; `-m` у закрытой не пишется; модель: нет
- `done` - `Move` в колонку `closed`; `-m` (строка TUI 2.3): для `waiting` текст поля, для `closed` комментарий `Result: ...`; модель: нет
- `rank`: max+1; `--first` min-1; `--after` середина; зазор меньше 1e-6 - перенумерация колонки одним коммитом (сбой `kill -9` принят); модель: нет
- `ctx` без git; 2000 рун держит уменьшение задач на колонку от 5 (`K more`); строка до 160 рун; модель: нет
- `backup`: под локом только коммит чужих правок (сбой - `warn`); push без лока: `origin`, `HEAD`, без `--force`, 30 с с `WaitDelay`, без запросов пароля (`GIT_TERMINAL_PROMPT=0`, `BatchMode`, если ssh не настроен); `user:pass@` вырезается; модель: нет
- `RULES.md`: любая запись создаёт, если нет; шаблон тикета - его раздел (Р11); модель: нет

## 3. Сценарии

Раздел 3 спеки (happy path с `Actor:` из окружения, чужой захват, `set status=`, закрытие с неотмеченными, remote недоступен) и:

| Сценарий | Шаги | Ожидание |
|---|---|---|
| Гонка, истёк, продление | 5 процессов `take`; claim в прошлом; `comment`, `note` | один `ok`; без `--steal`; срок двигает `comment` |
| `take` отказ | из `inbox`, `closed`, без роли | `ErrRule` с `move` (Р51) |
| `move` отказ | в `ready` без критерия; из/в колонку без роли; в `waiting` без поля и `-m` | `ErrRule`; с полем проходит |
| Повтор; `waiting` | `done -m` дважды (и 0 критериев без `-m`); `set` тем же; `take` свой; `move`, `take` из `waiting` | коммита нет, срок прежний; `err`; поле `waiting` пусто (Р52) |
| Порядок | `--first`, `--after` (чужая колонка: `ErrRule`), 100 вставок | порядок верен |
| `ctx`, id | 7 в `ready`; пусто; вне проекта; битый файл; `tk show 12` | `2 more`; `no tasks`; `project all`; `warn`, код 0; `T-12` (Р48) |
| Личность | `--as`, окружение, `TRACKER_ACTOR`, ничего, `--as unknown` | порядок 2.4; `take` без неё и с `unknown` - `err`, прочие пишут `unknown` |
| Backup | bare; правка мимо ядра; битый путь; нет базы | HEAD bare равен локальному; `err` |
| Коды, прочее | занято, конфликт, нет задачи, правило, ввод; иной вход | коды 2-6; иное: код 1, лог |

## 4. Данные и состояния

Раздел 8. Критерий - строка `## Details` по `^\s*[-*] \[( |x|X)\] `, номер с 1. Коммиты: `take`, `steal <owner>` (при живом чужом), `release`, `move <from> > <to>`, `set <fields>`, `check <nums>`, `done [force]` (в `closed`). Claim ставит только `take`.

## 5. Кодовая модель

```go
// internal/actor (новый)
func Resolve(flag string, getenv func(string) string) (name string, known bool) // 2.4; "claude:<id>", "codex:<id>"
func Valid(name string) error  // ввод (`--as`, `TRACKER_ACTOR`): без пробелов, `|`, `-->`, переводов строки; `unknown` допустим; зовут core и cli
func Short(name string) string // "kind:" и 8 знаков id

// internal/task (добавления)
type Claim struct{ Actor string; Until time.Time }
type Criterion struct{ N int; Text string; Done bool }
func (t *Task) ClaimInfo() (Claim, bool) // пусто, битое: false
func (t *Task) SetClaim(c Claim)
func (t *Task) ClearClaim()
func (t *Task) Criteria() []Criterion
func (t *Task) Check(nums ...int) (changed bool, err error)
func (t *Task) SetField(name, value string) error  // title waiting due tags ask external
func (t *Task) SetSection(name, text string) error // Abstract, Summary; "## " в тексте - ошибка

// internal/config (добавления)
func (c *Config) ByRole(role string) (Column, bool)
func (c *Config) RoleOf(column string) string // "" - нет колонки или роли

// internal/brief (новый, без IO); JSON: имена полей в нижнем регистре, лимит только у текста
type Input struct{ Tasks []*task.Task; Cfg *config.Config; Project, RulesPath, Actor string; Now time.Time; PerColumn, Limit int }
type Row struct{ ID, Title, Detail, Claim string } // Detail: summary, "Waiting: ...", "k/n"; свой claim "@you"
type Section struct{ Column string; Rows []Row; More int }
type Brief struct{ Project, Rules string; Sections []Section; Next string }
func Build(in Input) Brief   // роли active, waiting, ready; сжатие до Limit
func (b Brief) Text() string

// internal/gitx (добавления)
func (r Repo) Remotes() ([]string, error)
func (r Repo) Push(ctx context.Context, remote string) error

// internal/core (аддитивно к З1): Options + Now func() time.Time; Result + Info string (хвост ok)
var ErrClaimed, ErrNoIdentity, ErrRule, ErrNoRemote error
type TakeMode int // Take, Steal, Release
type Place struct{ First bool; After string }
type MoveOpts struct{ Place Place; Message string; Force bool }
type Field struct{ Name, Value string }
func (s *Store) Take(id string, mode TakeMode, actor string) (Result, error) // "" или "unknown": ErrNoIdentity
func (s *Store) Move(id, column string, o MoveOpts, actor string) (Result, error)
func (s *Store) Set(id string, fields []Field, actor string) (Result, error)
func (s *Store) Check(id string, nums []int, actor string) (Result, error)
func (s *Store) Backup(ctx context.Context) (Result, error)
```

`cli`: `Run`, `Env` те же; `known=false`: везде `unknown`; `12` разворачивается в `T-12` в `<id>`, `--after`. Строки:
- `ctx [--project p] [--json]`: `project ST-project   rules: ~/tasks/RULES.md`, `- take: tk take; step: tk note; end: tk set summary=`, `<column> T-12 <title> | <detail>`, `next T-17`
- `take <id> [--steal|--release]`: `ok T-17 todo > doing  @claude until 20:40`
- `move <id> <column> [--first|--after T-9] [-m text] [--force]`: `ok T-9 doing > blocked`
- `set <id> field=value...`: `ok T-17 summary`; `check <id> N...`: `ok T-17 1/3`
- `done <id> [-m result] [--force]`: `ok T-12 doing > done`; `backup`: `ok backup origin`

Коды выхода (`errors.Is` в `cli`, зона D; З1 даёт 1, её тесты правит T): 0 успех; 2 занято другим (`ErrClaimed`, `ErrLocked`); 3 `ErrConflict`; 4 `ErrNotFound`; 5 `ErrRule`; 6 ввод (`ErrNoIdentity`, флаг, id); 1 прочее (push).

Алгоритмы (под локом З1, кроме push) и ошибки:
- `Take`: нет личности - `no identity; pass --as agent:id`. Роль не `ready|waiting|active` - `T-5 is in inbox; run: tk move T-5 <ready column>`. Чужой живой claim без `--steal` - `claimed by claude:8f3a1c2e until 20:40; steal: tk take T-5 --steal`. Иначе `active` (в конец, если колонка другая), claim now+3 ч. `Release`: свой снимает, чужой живой - `ErrClaimed`, без claim - без коммита.
- `Move`: источник или цель без роли - `column review has no role`. Цель `ready`: `ready needs a criterion`. Цель `waiting`: поле или `-m` (пишет поле), иначе `waiting needs a reason: use -m`. Закрытие: `1/3 criteria checked; run: tk check T-12 2 3`; 0 критериев без `-m` - `no criteria; use -m result`; `--force` требует `-m`. `-m` вне `waiting`, `closed` - `ErrRule`. Выход из `waiting` (и `Take` из неё) чистит поле; выход из `active` и закрытие снимают claim.
- `Set`: `status`, `claim`, `rank` - `status is changed by tk move`; `id`, `created` - `cannot be changed`; иное - `allowed: title ...`. По первому `=`; `tags` через запятую; `due` - `2006-01-02`; пустой `title`, `waiting=` в `waiting` - `ErrRule`.
- `Backup`: нет `origin` - `no remote origin; run: git remote add origin <url>`.

## 6. Этапы реализации

Зона 0 (один исполнитель): заглушки раздела 5, `assets/assets.go` (`go:embed`), вызов продления в `modifyOnce`. Затем A-D и T вместе, документы.

| Зона | Файлы | Зависит от | Проверка |
|---|---|---|---|
| 0 Каркас | заглушки, `internal/assets/`, `core/tx.go` | З1 в `main` | `go build ./...` |
| A данные | `internal/{task,config,actor,brief}` | 0 | `go build`, тесты T |
| B git, замер, текст | `internal/gitx`, `cmd/tkbench`, `assets/RULES.md` | 0 | то же |
| C ядро | `internal/core` | 0 (A, B) | `go build`, тесты T |
| D CLI | `internal/cli`, `cmd/tk` | 0 (A, C) | `go build -o bin/tk ./cmd/tk` |
| T тесты | `*_test.go`, `testdata/`, `e2e/`; временные `TRACKER_HOME`, `HOME` | 0, раздел 3 | `go vet`; сначала красные |
| Документы | `docs/architecture.md`, `AGENTS.md`, `README.md` | A-D, T | `mermaid-lint.sh` |

`RULES.md` - английский, из спеки: цикл раздела 3; захват (Р36, Р39); остаток и новый объём - новая задача, итог `Done / Decided / Waiting` (Р37); `ask` (Р38); проверки 2.5, Р51, Р52; права агента (8); идея - `inbox`, `personal`; `--as opencode:<id>`; шаблон тикета (Р23, Р25).

## 7. Критерий приёмки

- `make check` и `make bench`: код 0; `ctx` до 15 мс на 1000 задач, `take` и `move` до 50 мс. Каждый прогон `tkbench` пишет коммит: `take` - 30 задач `ready`; `move` - между двумя колонками.
- Не автоматизируется: push в настоящий remote (у пользователя). Не проверяем: агенты (З3), TUI (З4).

## Вопросы пользователю

Нет.
