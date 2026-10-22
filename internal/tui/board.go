package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
	"github.com/daniil4545/tix/internal/tui/view"
)

// unknownColumn - the column for tasks whose status is not described in config.toml (at the end of the board).
const unknownColumn = view.UnknownColumn

// apply takes a fresh task list and rebuilds the board, the selection is kept by id.
func (m *Model) apply(tasks []*task.Task, skipped int) {
	m.tasks, m.skipped = tasks, skipped
	m.dropGone()
	m.rebuild()
}

// dropGone cancels the grab if the grabbed task is not on the board or was moved since the grab began.
func (m *Model) dropGone() {
	if m.grab == nil {
		return
	}
	t := m.byID(m.grab.id)
	switch {
	case t == nil || (m.project != "" && !strings.EqualFold(t.Project, m.project)):
		m.setStatus(m.grab.id+" removed", true)
		m.grab = nil
	case t.Status != m.grab.status:
		m.setStatus(m.grab.id+" moved meanwhile, grab cancelled", true)
		m.grab = nil
	}
}

// rebuild builds the columns by the project filter and puts the cursor on selID.
func (m *Model) rebuild() {
	names := make([]string, 0, len(m.cfg.Columns)+1)
	for _, c := range m.cfg.Columns {
		names = append(names, c.Name)
	}
	byCol := map[string][]*task.Task{}
	total := 0
	for _, t := range m.tasks {
		if m.project != "" && !strings.EqualFold(t.Project, m.project) {
			continue
		}
		col := t.Status
		if !slices.Contains(names, col) {
			col = unknownColumn
		}
		if g := m.grab; g != nil && g.id == t.ID {
			g.held, g.baseCol = t, col
		}
		byCol[col] = append(byCol[col], t)
		total++
	}
	if len(byCol[unknownColumn]) > 0 {
		names = append(names, unknownColumn)
	}
	b := view.Board{Project: m.project, Total: total, Col: m.board.Col, Row: m.board.Row, ByDue: m.board.ByDue,
		FirstCol: m.board.FirstCol, RowTop: m.board.RowTop}
	m.colTasks = m.colTasks[:0]
	target := ""
	if m.grab != nil {
		m.grab.col = min(max(m.grab.col, 0), len(names)-1)
		target = names[m.grab.col]
	}
	for _, name := range names {
		ts := byCol[name]
		sortTasks(ts, m.board.ByDue)
		if m.grab != nil {
			ts = m.grab.place(name, ts, target, m.board.ByDue)
		}
		m.colTasks = append(m.colTasks, ts)
		b.Cols = append(b.Cols, view.Col{Name: name, Rows: m.rows(ts)})
	}
	m.board = b
	m.restoreSelection()
	m.fitWindow()
}

// fitWindow shifts the column and card windows only if the cursor left them, and stores
// them in the board. The window of another column starts at the top, as drawn, until it is selected.
func (m *Model) fitWindow() {
	if m.board.Col != m.winCol {
		m.board.RowTop, m.winCol = 0, m.board.Col
	}
	m.board.FirstCol, m.board.RowTop = view.FitWindow(m.layout())
}

// sortTasks sorts a column by rank; byDue: first tasks with a due date by date, then without due by rank.
func sortTasks(ts []*task.Task, byDue bool) {
	sort.SliceStable(ts, func(i, j int) bool {
		if byDue {
			di, iok := dueDate(ts[i])
			dj, jok := dueDate(ts[j])
			if iok != jok {
				return iok
			}
			if iok && !di.Equal(dj) {
				return di.Before(dj)
			}
		}
		if ts[i].Rank != ts[j].Rank {
			return ts[i].Rank < ts[j].Rank
		}
		return core.IDNum(ts[i].ID) < core.IDNum(ts[j].ID)
	})
}

// dueDate - the task due date as a date; an empty or unreadable due is false.
func dueDate(t *task.Task) (time.Time, bool) {
	d, err := time.Parse(time.DateOnly, t.Due)
	return d, err == nil
}

// toggleOrder switches the order of all columns: by rank or by due.
func (m *Model) toggleOrder() {
	m.board.ByDue = !m.board.ByDue
	m.rebuild()
}

// reorderKey - J, K: in due order the manual order is off.
func (m *Model) reorderKey(dir int) tea.Cmd {
	if m.board.ByDue {
		m.setStatus("order by due: J K off", false)
		return nil
	}
	return m.reorder(dir)
}

// restoreSelection looks for selID in the columns; if absent, the position stays, clamped to the bounds.
func (m *Model) restoreSelection() {
	for c, ts := range m.colTasks {
		if i := slices.IndexFunc(ts, func(t *task.Task) bool { return t.ID == m.selID }); i >= 0 {
			m.board.Col, m.board.Row = c, i
			return
		}
	}
	m.board.Col = min(max(m.board.Col, 0), max(len(m.colTasks)-1, 0))
	m.clampRow()
	m.syncSelID()
}

func (m *Model) clampRow() {
	if len(m.colTasks) == 0 {
		m.board.Row = 0
		return
	}
	m.board.Row = min(max(m.board.Row, 0), max(len(m.colTasks[m.board.Col])-1, 0))
}

// syncSelID remembers the id under the cursor.
func (m *Model) syncSelID() {
	m.selID = ""
	if t := m.selected(); t != nil {
		m.selID = t.ID
	}
}

// selected - the task under the board cursor; nil in an empty column.
func (m *Model) selected() *task.Task {
	if m.board.Col >= len(m.colTasks) || m.board.Row >= len(m.colTasks[m.board.Col]) {
		return nil
	}
	return m.colTasks[m.board.Col][m.board.Row]
}

// current - the task the keys act on: the open card or the row under the cursor.
func (m *Model) current() *task.Task {
	if m.showing {
		return m.byID(m.card.id)
	}
	return m.selected()
}

func (m *Model) byID(id string) *task.Task {
	for _, t := range m.tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (m *Model) boardView() view.Board {
	b := m.board
	b.W, b.H = m.w, m.h
	b.Status, b.IsErr = m.statusText()
	return b
}

func (m *Model) rows(ts []*task.Task) []view.Row {
	out := make([]view.Row, len(ts))
	now := m.now()
	names := make([]string, len(m.tasks))
	for i, t := range m.tasks {
		names[i] = t.Project
	}
	colors := view.ProjectColors(names) // over all tasks: a project keeps its color under any filter
	for i, t := range ts {
		out[i] = view.Row{
			ProjectColor: colors[strings.ToLower(t.Project)],
			ID:           t.ID, Project: t.Project, Title: t.Title, Summary: t.Summary(),
			Meta: meta(t, now), Ask: strings.TrimSpace(t.Ask), Live: m.liveClaim(t), Overdue: overdue(t, now), Note: m.note(t, now),
			Grabbed: m.grab != nil && m.grab.id == t.ID,
		}
	}
	return out
}

// meta - the short board line: criteria, due, live claim.
func meta(t *task.Task, now time.Time) string {
	var parts []string
	if crit := t.Criteria(); len(crit) > 0 {
		parts = append(parts, checks(crit))
	}
	if t.Due != "" {
		parts = append(parts, "due "+dueText(t.Due))
	}
	if c, ok := t.ClaimInfo(); ok && c.Until.After(now) {
		parts = append(parts, "@"+actor.Short(c.Actor))
	}
	return strings.Join(parts, " ")
}

// overdue - the due text of the meta line when the date is before the model-clock day; else empty.
func overdue(t *task.Task, now time.Time) string {
	d, err := time.Parse(time.DateOnly, t.Due)
	if err != nil || d.Format(time.DateOnly) >= now.Format(time.DateOnly) {
		return ""
	}
	return "due " + dueText(t.Due)
}

func checks(crit []task.Criterion) string {
	done := 0
	for _, c := range crit {
		if c.Done {
			done++
		}
	}
	return fmt.Sprintf("%d/%d", done, len(crit))
}

// dueText: a date like 2026-10-28 is printed as 28.10, anything else as is.
func dueText(due string) string {
	d, err := time.Parse(time.DateOnly, due)
	if err != nil {
		return due
	}
	return d.Format("02.01")
}

// cycleProject switches the filter in a circle: all, then projects alphabetically.
func (m *Model) cycleProject() {
	seen := map[string]bool{}
	var list []string
	for _, t := range m.tasks {
		if p := strings.ToLower(t.Project); p != "" && !seen[p] {
			seen[p] = true
			list = append(list, p)
		}
	}
	sort.Strings(list)
	list = append([]string{""}, list...)
	i := slices.Index(list, strings.ToLower(m.project))
	m.project = list[(i+1)%len(list)]
	m.rebuild()
}

// moveCursor moves the board cursor: dc columns, dr rows.
func (m *Model) moveCursor(dc, dr int) {
	if len(m.colTasks) == 0 {
		return
	}
	m.board.Col = min(max(m.board.Col+dc, 0), len(m.colTasks)-1)
	m.board.Row += dr
	m.clampRow()
	m.syncSelID()
	m.fitWindow()
}
