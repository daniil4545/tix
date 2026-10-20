package tui

import (
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/core"
)

// queueSize - ёмкость очереди записи; полная очередь отвечает queue full (Р44).
const queueSize = 16

var errQueueFull = errors.New("queue full")

// job - одна запись в ядро. reply получает результат от work.
type job struct {
	run   func() (core.Result, error)
	reply chan opDone
}

// opDone - итог записи. edit не nil, если запись шла из редактора.
type opDone struct {
	id    string
	label string
	res   core.Result
	err   error
	edit  *editSession
}

// work - единственный писатель в ядро: операции идут по одной в порядке нажатий.
// Горутину запускает New, она живёт до конца ctx.
func (m *Model) work() {
	for {
		select {
		case <-m.done:
			return
		case j := <-m.queue:
			j.reply <- m.runJob(j.run)
		}
	}
}

func (m *Model) runJob(run func() (core.Result, error)) (out opDone) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Error("write panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			out = opDone{err: fmt.Errorf("internal error: %v", r)}
		}
	}()
	res, err := run()
	return opDone{res: res, err: err}
}

// submit ставит запись в очередь; экран не ждёт лок ядра. label - текст успеха, если ядро
// не вернуло своего.
func (m *Model) submit(id, label string, edit *editSession, run func() (core.Result, error)) tea.Cmd {
	return func() tea.Msg {
		reply := make(chan opDone, 1)
		select {
		case m.queue <- job{run: run, reply: reply}:
		default:
			return opDone{id: id, label: label, err: errQueueFull, edit: edit}
		}
		select {
		case out := <-reply:
			out.id, out.label, out.edit = id, label, edit
			return out
		case <-m.done:
			return nil
		}
	}
}

// opResult показывает итог записи и перечитывает доску.
func (m *Model) opResult(msg opDone) tea.Cmd {
	if msg.edit != nil {
		return m.editResult(msg)
	}
	m.report(msg)
	return m.loadCmd()
}

func (m *Model) report(msg opDone) {
	switch {
	case msg.err != nil:
		m.setStatus(msg.id+": "+errText(msg.err), true)
	case msg.res.Warning != "":
		m.setStatus(msg.id+": "+firstLine(msg.res.Warning), true)
	case msg.res.Info != "":
		m.setStatus(msg.id+" "+firstLine(msg.res.Info), false)
	default:
		m.setStatus(msg.id+" "+msg.label, false)
	}
}

// errText - короткий текст ошибки ядра для строки состояния.
func errText(err error) string {
	switch {
	case errors.Is(err, core.ErrLocked):
		return "store is busy, try again"
	case errors.Is(err, core.ErrNotFound):
		return "task not found"
	}
	return firstLine(err.Error())
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// startMove переносит текущую задачу в соседнюю колонку: dir 1 вправо, -1 влево.
// В waiting и closed ядро требует причину, её спрашивает строка ввода.
func (m *Model) startMove(dir int) tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	cols := m.cfg.Columns
	from := -1
	for i, c := range cols {
		if c.Name == t.Status {
			from = i
		}
	}
	if from < 0 {
		m.setStatus(t.ID+": column "+t.Status+" is not in config", true)
		return nil
	}
	to := from + dir
	if to < 0 || to >= len(cols) {
		return nil
	}
	target := cols[to]
	move := func(opts core.MoveOpts) tea.Cmd {
		return m.submit(t.ID, t.Status+" > "+target.Name, nil, func() (core.Result, error) {
			return m.d.Store.Move(t.ID, target.Name, opts, m.d.Actor)
		})
	}
	switch target.Role {
	case "waiting":
		if strings.TrimSpace(t.Waiting) == "" {
			m.ask("waiting for:", func(text string) tea.Cmd { return move(core.MoveOpts{Message: text}) })
			return nil
		}
	case "closed":
		crit := t.Criteria()
		open := 0
		for _, c := range crit {
			if !c.Done {
				open++
			}
		}
		if len(crit) == 0 || open > 0 {
			m.ask("close reason:", func(text string) tea.Cmd {
				return move(core.MoveOpts{Message: text, Force: open > 0})
			})
			return nil
		}
	}
	return move(core.MoveOpts{})
}

// reorder сдвигает текущую задачу в своей колонке: dir 1 вниз, -1 вверх. На краю - ничего.
func (m *Model) reorder(dir int) tea.Cmd {
	t := m.selected()
	if t == nil {
		return nil
	}
	col, i := m.colTasks[m.board.Col], m.board.Row
	var place core.Place
	switch {
	case dir > 0 && i+1 < len(col):
		place.After = col[i+1].ID
	case dir < 0 && i == 1:
		place.First = true
	case dir < 0 && i > 1:
		place.After = col[i-2].ID
	default:
		return nil
	}
	label := "moved up"
	if dir > 0 {
		label = "moved down"
	}
	return m.submit(t.ID, label, nil, func() (core.Result, error) {
		return m.d.Store.Move(t.ID, t.Status, core.MoveOpts{Place: place}, m.d.Actor)
	})
}

// startComment спрашивает текст и пишет комментарий от имени человека.
func (m *Model) startComment() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	m.ask("comment:", func(text string) tea.Cmd {
		return m.submit(t.ID, "comment added", nil, func() (core.Result, error) {
			return m.d.Store.Comment(t.ID, text, m.d.Actor)
		})
	})
	return nil
}
