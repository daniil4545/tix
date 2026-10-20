// Package tui - интерактивная доска: модель Bubble Tea, очередь записи, наблюдатель файлов.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"time"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/config"
	"tracker/internal/core"
	"tracker/internal/task"
	"tracker/internal/tui/view"
)

// Deps - что TUI получает снаружи. Project - начальный фильтр (папка проекта), пусто - все.
// GitBin - сырое значение TRACKER_GIT для истории карточки.
type Deps struct {
	Store                *core.Store
	Root, Actor, Project string
	GitBin               string
	Log                  *slog.Logger
}

const (
	// oneshotEnv: нарисовать первый кадр в stdout и выйти (замер открытия, tkbench).
	oneshotEnv = "TRACKER_TUI_ONESHOT"
	// oneshotW, oneshotH - размер кадра без терминала.
	oneshotW, oneshotH = 120, 40
	// watchDelay: наблюдатель стартует после первого кадра (kqueue держит дескриптор на файл).
	watchDelay = 50 * time.Millisecond
	// settleDelay склеивает пачку событий файлов в одно перечитывание.
	settleDelay = 150 * time.Millisecond
)

// Run открывает TUI на альтернативном экране; паника уходит ошибкой в Log.
func Run(ctx context.Context, d Deps) error {
	if d.Store == nil {
		return errors.New("tui: store required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := New(ctx, d)
	if os.Getenv(oneshotEnv) == "1" {
		m.w, m.h = oneshotW, oneshotH
		_, err := io.WriteString(os.Stdout, m.View().Content+"\n")
		return err
	}
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	switch {
	case errors.Is(err, tea.ErrProgramPanic):
		m.log.Error("tui panic", "err", err)
		return err
	case errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil:
		return nil
	}
	return err
}

// Model - состояние TUI: доска, карточка, строка ввода. Горутины очереди и наблюдателя
// останавливает ctx, переданный в New.
type Model struct {
	d    Deps
	log  *slog.Logger
	cfg  *config.Config
	done <-chan struct{}

	queue  chan job
	events chan struct{}

	tasks   []*task.Task
	skipped int
	project string

	board    view.Board
	colTasks [][]*task.Task
	selID    string

	card    cardState
	showing bool // карточка открыта
	input   *prompt

	status  string
	isErr   bool
	w, h    int
	pending bool // перечитывание уже ждёт окна склейки
}

// New строит модель и читает доску сразу: первый кадр не ждёт git и истории.
// Очередь записи работает, пока жив ctx.
func New(ctx context.Context, d Deps) *Model {
	m := &Model{
		d:       d,
		log:     d.Log,
		cfg:     d.Store.Config(),
		done:    ctx.Done(),
		queue:   make(chan job, queueSize),
		events:  make(chan struct{}, 1),
		project: d.Project,
	}
	if m.log == nil {
		m.log = slog.New(slog.DiscardHandler)
	}
	tasks, errs := d.Store.List()
	m.apply(tasks, len(errs))
	go m.work()
	return m
}

// Init откладывает запуск наблюдателя до первого кадра.
func (m *Model) Init() tea.Cmd {
	return tea.Tick(watchDelay, func(time.Time) tea.Msg { return watchStart{} })
}

// Update разбирает сообщение; панику пишет в лог и отдаёт Bubble Tea, он вернёт терминал.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.logPanic()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.PasteMsg:
		if m.input != nil {
			m.input.insert(msg.Content)
		}
	default:
		return m, m.event(msg)
	}
	return m, nil
}

// View рисует кадр: карточку или доску, поверх строки состояния - строку ввода.
func (m *Model) View() tea.View {
	defer m.logPanic()
	var frame string
	if m.showing {
		frame = view.RenderCard(m.cardView())
	} else {
		frame = view.RenderBoard(m.boardView())
	}
	if m.input != nil {
		frame = withInput(frame, view.RenderInput(m.input.label, m.input.String(), m.w))
	}
	v := tea.NewView(frame)
	v.AltScreen = true
	return v
}

func (m *Model) logPanic() {
	if r := recover(); r != nil {
		m.log.Error("tui panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		panic(r)
	}
}

// setStatus ставит строку состояния; err - оранжевая.
func (m *Model) setStatus(text string, isErr bool) {
	m.status, m.isErr = text, isErr
}

// statusText - строка состояния; пустая заменяется счётчиком пропущенных файлов.
func (m *Model) statusText() (string, bool) {
	if m.status == "" && m.skipped > 0 {
		noun := "files"
		if m.skipped == 1 {
			noun = "file"
		}
		return fmt.Sprintf("%d %s skipped", m.skipped, noun), false
	}
	return m.status, m.isErr
}

// guard выполняет cmd с recover: паника горутины уходит в лог и в строку состояния.
func (m *Model) guard(fn func() tea.Msg) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("tui command panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				msg = failed{err: fmt.Errorf("internal error: %v", r)}
			}
		}()
		return fn()
	}
}

// failed - ошибка фоновой команды без привязки к записи.
type failed struct{ err error }
