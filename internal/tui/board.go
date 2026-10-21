package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/actor"
	"tracker/internal/core"
	"tracker/internal/task"
	"tracker/internal/tui/view"
)

// unknownColumn - колонка для задач, чей статус не описан в config.toml (в конце доски).
const unknownColumn = view.UnknownColumn

// apply принимает свежий список задач и перестраивает доску, выбор держится на id.
func (m *Model) apply(tasks []*task.Task, skipped int) {
	m.tasks, m.skipped = tasks, skipped
	m.dropGone()
	m.rebuild()
}

// dropGone снимает захват, если захваченной задачи нет на доске или её перенесли с начала захвата.
func (m *Model) dropGone() {
	if m.grab == nil {
		return
	}
	t := m.byID(m.grab.id)
	switch {
	case t == nil || (m.project != "" && t.Project != m.project):
		m.setStatus(m.grab.id+" removed", true)
		m.grab = nil
	case t.Status != m.grab.status:
		m.setStatus(m.grab.id+" moved meanwhile, grab cancelled", true)
		m.grab = nil
	}
}

// rebuild строит колонки по фильтру проекта и ставит курсор на selID.
func (m *Model) rebuild() {
	names := make([]string, 0, len(m.cfg.Columns)+1)
	for _, c := range m.cfg.Columns {
		names = append(names, c.Name)
	}
	byCol := map[string][]*task.Task{}
	total := 0
	for _, t := range m.tasks {
		if m.project != "" && t.Project != m.project {
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

// fitWindow сдвигает окна колонок и карточек, только если курсор вышел за них, и запоминает
// их в доске. Окно другой колонки начинается сверху, как её рисуют, пока она не выбрана.
func (m *Model) fitWindow() {
	if m.board.Col != m.winCol {
		m.board.RowTop, m.winCol = 0, m.board.Col
	}
	m.board.FirstCol, m.board.RowTop = view.FitWindow(m.layout())
}

// sortTasks сортирует колонку по rank; byDue: сначала задачи со сроком по дате, затем без срока по rank.
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

// dueDate - срок задачи датой; пустой или нечитаемый срок - false.
func dueDate(t *task.Task) (time.Time, bool) {
	d, err := time.Parse(time.DateOnly, t.Due)
	return d, err == nil
}

// toggleOrder переключает порядок всех колонок: по rank или по сроку.
func (m *Model) toggleOrder() {
	m.board.ByDue = !m.board.ByDue
	m.rebuild()
}

// reorderKey - J, K: в порядке по сроку ручной порядок выключен.
func (m *Model) reorderKey(dir int) tea.Cmd {
	if m.board.ByDue {
		m.setStatus("order by due: J K off", false)
		return nil
	}
	return m.reorder(dir)
}

// restoreSelection ищет selID в колонках; нет - остаётся позиция, зажатая в границы.
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

// syncSelID запоминает id под курсором.
func (m *Model) syncSelID() {
	m.selID = ""
	if t := m.selected(); t != nil {
		m.selID = t.ID
	}
}

// selected - задача под курсором доски; nil в пустой колонке.
func (m *Model) selected() *task.Task {
	if m.board.Col >= len(m.colTasks) || m.board.Row >= len(m.colTasks[m.board.Col]) {
		return nil
	}
	return m.colTasks[m.board.Col][m.board.Row]
}

// current - задача, над которой работают клавиши: открытая карточка или строка под курсором.
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
	for i, t := range ts {
		out[i] = view.Row{
			ID: t.ID, Project: t.Project, Title: t.Title, Summary: t.Summary(),
			Meta: meta(t, now), Ask: strings.TrimSpace(t.Ask), Note: m.note(t, now),
			Grabbed: m.grab != nil && m.grab.id == t.ID,
		}
	}
	return out
}

// meta - краткая строка доски: критерии, срок, живой claim.
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

func checks(crit []task.Criterion) string {
	done := 0
	for _, c := range crit {
		if c.Done {
			done++
		}
	}
	return fmt.Sprintf("%d/%d", done, len(crit))
}

// dueText: дата вида 2026-10-28 печатается как 28.10, прочее - как есть.
func dueText(due string) string {
	d, err := time.Parse(time.DateOnly, due)
	if err != nil {
		return due
	}
	return d.Format("02.01")
}

// cycleProject переключает фильтр по кругу: все, затем проекты по алфавиту.
func (m *Model) cycleProject() {
	seen := map[string]bool{}
	var list []string
	for _, t := range m.tasks {
		if t.Project != "" && !seen[t.Project] {
			seen[t.Project] = true
			list = append(list, t.Project)
		}
	}
	sort.Strings(list)
	list = append([]string{""}, list...)
	i := slices.Index(list, m.project)
	m.project = list[(i+1)%len(list)]
	m.rebuild()
}

// moveCursor сдвигает курсор доски: dc колонок, dr строк.
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
