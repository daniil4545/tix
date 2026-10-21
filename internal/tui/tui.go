// Package tui - the interactive board: the Bubble Tea model, the write queue, the file watcher.
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

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/task"
	"github.com/daniil4545/tix/internal/tui/view"
)

// Deps - what the TUI gets from outside. Project - the initial filter (the project folder), empty - all.
// GitBin - the raw TIX_GIT value for the card history.
type Deps struct {
	Store                *core.Store
	Root, Actor, Project string
	GitBin               string
	Log                  *slog.Logger
}

const (
	// oneshotEnv: draw the first frame to stdout and exit (open-time measurement, tixbench).
	oneshotEnv = "TIX_TUI_ONESHOT"
	// oneshotW, oneshotH - the frame size without a terminal.
	oneshotW, oneshotH = 120, 40
	// watchDelay: the watcher starts after the first frame (kqueue holds a descriptor per file).
	watchDelay = 50 * time.Millisecond
	// settleDelay merges a batch of file events into one reread.
	settleDelay = 150 * time.Millisecond
)

// Run opens the TUI on the alternate screen; a panic goes out as an error to Log.
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

// Model - the TUI state: the board, the card, the input line. The queue and watcher goroutines
// are stopped by the ctx passed to New.
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
	showing bool // the card is open
	help    bool // the help screen is open
	input   *prompt
	acts    cardActs // card actions: field choice
	grab    *grab    // a card grab on the board; nil outside a grab

	notes map[string]gitx.Note // the latest task notes for the pulse; nil until git answers
	now   func() time.Time     // the model clock: meta, pulse, double click

	press     *press // a mouse press on a card; nil outside a drag
	lastClick click  // the last click on a card, for the double click
	winCol    int    // the column that board.RowTop belongs to

	status  string
	isErr   bool
	w, h    int
	pending bool // a reread is already waiting for the merge window
}

// New builds the model and reads the board at once: the first frame does not wait for git and history.
// The write queue runs while ctx is alive.
func New(ctx context.Context, d Deps) *Model {
	m := &Model{
		d:       d,
		log:     d.Log,
		cfg:     d.Store.Config(),
		done:    ctx.Done(),
		queue:   make(chan job, queueSize),
		events:  make(chan struct{}, 1),
		project: d.Project,
		now:     time.Now,
	}
	if m.log == nil {
		m.log = slog.New(slog.DiscardHandler)
	}
	tasks, errs := d.Store.List()
	m.apply(tasks, len(errs))
	go m.work()
	return m
}

// Init defers the watcher start until the first frame.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(watchDelay, func(time.Time) tea.Msg { return watchStart{} }),
		m.notesCmd(),
	)
}

// Update handles a message; a panic is logged and handed to Bubble Tea, which restores the terminal.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.logPanic()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.fitWindow()
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.MouseMsg:
		return m, m.mouse(msg)
	case tea.PasteMsg:
		if m.input != nil {
			m.input.insert(msg.Content)
		}
	default:
		return m, m.event(msg)
	}
	return m, nil
}

// View draws the frame: the card or the board, with the input line over the status line.
func (m *Model) View() tea.View {
	defer m.logPanic()
	var frame string
	switch {
	case m.help:
		frame = view.RenderHelp(m.w, m.h)
	case m.showing:
		frame = view.RenderCard(m.cardView())
	default:
		frame = view.RenderBoard(m.boardView())
	}
	if m.input != nil {
		frame = withInput(frame, view.RenderInput(m.input.label, m.input.String(), m.w))
	}
	v := tea.NewView(frame)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *Model) logPanic() {
	if r := recover(); r != nil {
		m.log.Error("tui panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		panic(r)
	}
}

// setStatus sets the status line; err - orange.
func (m *Model) setStatus(text string, isErr bool) {
	m.status, m.isErr = text, isErr
}

// statusText - the status line; an empty one is replaced by the count of skipped files.
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

// guard runs cmd with recover: a goroutine panic goes to the log and to the status line.
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

// failed - a background command error not tied to a write.
type failed struct{ err error }
