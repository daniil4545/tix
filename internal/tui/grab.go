package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
)

// grab - picking up a card: it moves around the screen, files stay untouched until release.
// col and idx - the shown place: the board column index and the place in its list without the card itself.
// baseCol and baseAbove - the previous place (column and the task above it), rebuild recomputes them.
type grab struct {
	id        string
	col, idx  int
	status    string     // task status at the start of the grab
	held      *task.Task // the grabbed task, refreshed on every board build
	baseCol   string
	baseAbove string
}

// startGrab takes the selected card.
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

// grabKey handles a key during a grab; other keys are ignored.
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

// grabColumn moves the shown card to the neighbor column, to the same place or to the end.
func (m *Model) grabColumn(dir int) {
	g := m.grab
	next := g.col + dir
	if next < 0 || next >= len(m.colTasks) {
		return
	}
	g.col, g.idx = next, min(g.idx, len(m.colTasks[next]))
	m.rebuild()
}

// grabRow changes the card place in a column; in due order the place is not chosen.
func (m *Model) grabRow(dir int) {
	g := m.grab
	if m.board.ByDue {
		return
	}
	g.idx = min(max(g.idx+dir, 0), len(m.colTasks[g.col])-1)
	m.rebuild()
}

// drop releases the card: one Move to the shown column at the shown place.
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

// place takes the grabbed task out of its column (remembers who was above it) and inserts it
// into the shown column target; in due order - by due date. ts is already sorted.
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
