package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/core"
	"tracker/internal/task"
)

// grab - захват карточки: она переезжает по экрану, файлы не трогаются до отпускания.
// col и idx - показанное место: индекс колонки доски и место в её списке без самой карточки.
// baseCol и baseAbove - прежнее место (колонка и задача над ней), rebuild считает их заново.
type grab struct {
	id        string
	col, idx  int
	status    string     // статус задачи в начале захвата
	held      *task.Task // захваченная задача, обновляется при каждой сборке доски
	baseCol   string
	baseAbove string
}

// startGrab берёт выбранную карточку.
func (m *Model) startGrab() {
	t := m.selected()
	if t == nil {
		return
	}
	m.grab = &grab{id: t.ID, status: t.Status, col: m.board.Col, idx: m.board.Row}
	m.lastClick, m.press = click{}, nil
	m.rebuild()
	m.grabStatus()
}

func (m *Model) grabStatus() {
	if m.board.ByDue {
		m.setStatus("moving "+m.grab.id+": order by due: place off, space drop, esc cancel", false)
		return
	}
	m.setStatus("moving "+m.grab.id+": arrows move, space drop, esc cancel", false)
}

// grabKey разбирает клавишу при захвате; прочие клавиши игнорируются.
func (m *Model) grabKey(k string) tea.Cmd {
	switch k {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.grab = nil
		m.setStatus("", false)
		m.rebuild()
		return nil
	case "space", "enter":
		return m.drop()
	}
	g := m.grab
	switch k {
	case "left", "h":
		m.grabColumn(-1)
	case "right", "l":
		m.grabColumn(1)
	case "up", "k":
		m.grabRow(-1)
	case "down", "j":
		m.grabRow(1)
	default:
		return nil
	}
	if m.grab == g {
		m.grabStatus()
	}
	return nil
}

// grabColumn переносит показанную карточку в соседнюю колонку на то же место или в конец.
func (m *Model) grabColumn(dir int) {
	g := m.grab
	next := g.col + dir
	if next < 0 || next >= len(m.colTasks) {
		return
	}
	g.col, g.idx = next, min(g.idx, len(m.colTasks[next]))
	m.rebuild()
}

// grabRow меняет место карточки в колонке; в порядке по сроку место не выбирается.
func (m *Model) grabRow(dir int) {
	g := m.grab
	if m.board.ByDue {
		return
	}
	g.idx = min(max(g.idx+dir, 0), len(m.colTasks[g.col])-1)
	m.rebuild()
}

// drop отпускает карточку: один Move в показанную колонку на показанное место.
func (m *Model) drop() tea.Cmd {
	g := m.grab
	col, row := m.board.Col, m.board.Row
	target, above := m.board.Cols[col].Name, ""
	if row > 0 {
		above = m.colTasks[col][row-1].ID
	}
	byDue := m.board.ByDue
	m.grab = nil
	m.setStatus("", false)
	m.rebuild()
	t := m.byID(g.id)
	switch {
	case t == nil:
		m.setStatus(g.id+" removed", true)
		return nil
	case target == g.baseCol && (byDue || above == g.baseAbove):
		return nil
	case byDue:
		return m.moveTo(t, target, core.MoveOpts{})
	}
	return m.moveTo(t, target, core.MoveOpts{Place: core.Place{First: row == 0, After: above}})
}

// place вынимает захваченную задачу из её колонки (запоминает, кто был над ней) и вставляет
// в показанную колонку target; в порядке по сроку - по сроку. ts уже отсортирован.
func (g *grab) place(name string, ts []*task.Task, target string, byDue bool) []*task.Task {
	if name == g.baseCol {
		i := slices.IndexFunc(ts, func(t *task.Task) bool { return t.ID == g.id })
		g.baseAbove = ""
		if i > 0 {
			g.baseAbove = ts[i-1].ID
		}
		ts = slices.Delete(slices.Clone(ts), i, i+1)
	}
	if name != target {
		return ts
	}
	if byDue {
		ts = append(slices.Clone(ts), g.held)
		sortTasks(ts, true)
		return ts
	}
	return slices.Insert(slices.Clone(ts), min(g.idx, len(ts)), g.held)
}
