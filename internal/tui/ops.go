package tui

import (
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
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

// checkStatus отказывает, если задачу перенесли, пока была открыта строка ввода.
func (m *Model) checkStatus(id, status string) error {
	cur, _, err := m.d.Store.Get(id)
	if err != nil {
		return err
	}
	if cur.Status != status {
		return fmt.Errorf("moved to %s meanwhile, not moved", cur.Status)
	}
	return nil
}

// moveTo переносит задачу в колонку target одной записью Move; opts.Place - место в колонке.
// В waiting и closed ядро требует причину, её спрашивает строка ввода; перестановка внутри
// колонки причины не требует. Общий шаг для перетаскивания и захвата.
func (m *Model) moveTo(t *task.Task, target string, opts core.MoveOpts) tea.Cmd {
	if !m.configured(t.Status) {
		m.setStatus(t.ID+": column "+t.Status+" is not in config", true)
		return nil
	}
	i := slices.IndexFunc(m.cfg.Columns, func(c config.Column) bool { return c.Name == target })
	if i < 0 {
		m.setStatus(t.ID+": unknown column", true)
		return nil
	}
	col, from, label := m.cfg.Columns[i], t.Status, "moved"
	if target != from {
		label = from + " > " + target
	}
	move := func(o core.MoveOpts) tea.Cmd {
		return m.submit(t.ID, label, nil, func() (core.Result, error) {
			if err := m.checkStatus(t.ID, from); err != nil {
				return core.Result{}, err
			}
			return m.d.Store.Move(t.ID, target, o, m.d.Actor)
		})
	}
	if target == from {
		return move(opts)
	}
	ask := func(prompt string, force bool) tea.Cmd {
		m.ask(prompt, func(text string) tea.Cmd {
			o := opts
			o.Message, o.Force = text, force
			return move(o)
		})
		return nil
	}
	switch col.Role {
	case "waiting":
		if strings.TrimSpace(t.Waiting) == "" {
			return ask("waiting for:", false)
		}
	case "closed":
		total, open := countCriteria(t)
		if total == 0 || open > 0 {
			return ask("close reason:", open > 0)
		}
	}
	return move(opts)
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

// countCriteria возвращает число критериев и неотмеченных среди них.
func countCriteria(t *task.Task) (total, open int) {
	for _, c := range t.Criteria() {
		total++
		if !c.Done {
			open++
		}
	}
	return total, open
}
