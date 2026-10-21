package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/tui/view"
)

// doubleClick - окно второго клика по той же карточке (часы модели).
const doubleClick = 400 * time.Millisecond

// press - нажатие левой кнопки на карточке: id задачи, запомненный при нажатии.
type press struct{ id string }

// click - последний клик по карточке для двойного клика.
type click struct {
	id string
	at time.Time
}

// mouse разбирает мышь. Режим мыши включён на доске и в карточке (View). При строке ввода
// левый клик уходит в inputClick, остальное игнорируется; при захвате мышь игнорируется.
func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	mouse := msg.Mouse()
	if m.input != nil {
		m.press = nil
		if _, ok := msg.(tea.MouseClickMsg); ok && mouse.Button == tea.MouseLeft {
			return m.inputClick(mouse.X, mouse.Y)
		}
		return nil
	}
	if m.grab != nil || m.help {
		m.press = nil
		return nil
	}
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button == tea.MouseLeft {
			return m.mouseClick(mouse)
		}
	case tea.MouseReleaseMsg:
		// X10 сообщает отпускание без кнопки; правая и средняя не переносят.
		if mouse.Button == tea.MouseLeft || mouse.Button == tea.MouseNone {
			return m.mouseRelease(mouse)
		}
	case tea.MouseWheelMsg:
		return m.mouseWheel(mouse)
	}
	return nil
}

// layout - доска так, как она нарисована сейчас: по ней считается попадание.
func (m *Model) layout() view.Board {
	b := m.board
	b.W, b.H = m.w, m.h
	return b
}

// selectAt ставит курсор на карточку (col, row) доски.
func (m *Model) selectAt(col, row int) {
	m.board.Col, m.board.Row = col, row
	m.clampRow()
	m.syncSelID()
	m.fitWindow()
}

// mouseClick: клик по карточке выбирает её; второй за doubleClick по той же задаче открывает.
// Нажатие запоминается для переноса. Кнопка подсказки равна клавише, "+" в имени колонки -
// новая задача в ней. Клик мимо карточек - ничего; в карточке задачи - cardClick.
func (m *Model) mouseClick(e tea.Mouse) tea.Cmd {
	m.press = nil
	if m.showing {
		return m.cardClick(e.X, e.Y)
	}
	if key, ok := view.HitHint(m.layout(), e.X, e.Y); ok {
		return m.dispatch(key)
	}
	hit := view.HitBoard(m.layout(), e.X, e.Y)
	if hit.Plus {
		m.setStatus("", false)
		return m.startNew(hit.Col)
	}
	if hit.Row < 0 {
		return nil
	}
	m.setStatus("", false)
	m.selectAt(hit.Col, hit.Row)
	t := m.selected()
	if t == nil {
		return nil
	}
	now := m.now()
	prev := m.lastClick
	if prev.id == t.ID && !prev.at.IsZero() && now.Sub(prev.at) >= 0 && now.Sub(prev.at) <= doubleClick {
		m.lastClick = click{}
		return m.openCard()
	}
	m.lastClick = click{id: t.ID, at: now}
	m.press = &press{id: t.ID}
	return nil
}

// mouseRelease переносит задачу, нажатую мышью, в колонку под курсором: через тот же moveTo,
// что у клавиши m (проверки, строка причины). Над той же колонкой и вне колонок - ничего.
func (m *Model) mouseRelease(e tea.Mouse) tea.Cmd {
	p := m.press
	m.press = nil
	if p == nil || m.showing {
		return nil
	}
	hit := view.HitBoard(m.layout(), e.X, e.Y)
	if hit.Col < 0 {
		return nil
	}
	t := m.byID(p.id)
	if t == nil {
		m.lastClick = click{}
		m.setStatus(p.id+" removed", true)
		return nil
	}
	target := m.board.Cols[hit.Col].Name
	// Отпускание над своей колонкой - вторая половина клика: память о клике остаётся для двойного.
	if target == t.Status || (target == unknownColumn && !m.configured(t.Status)) {
		return nil
	}
	m.lastClick = click{}
	m.setStatus("", false)
	if !m.selectTask(t.ID) {
		return nil
	}
	return m.moveTo(t, target, core.MoveOpts{})
}

// configured: колонка описана в config.toml.
func (m *Model) configured(name string) bool {
	return slices.ContainsFunc(m.cfg.Columns, func(c config.Column) bool { return c.Name == name })
}

// selectTask ставит курсор на задачу id; false, если на доске её нет (фильтр проекта).
func (m *Model) selectTask(id string) bool {
	for c, ts := range m.colTasks {
		for r, t := range ts {
			if t.ID == id {
				m.selectAt(c, r)
				return true
			}
		}
	}
	return false
}

// mouseWheel: в карточке задачи - шаг по истории, как j и k; на доске - выбор колонки под
// курсором (как h и l до неё), затем шаг на одну карточку, как j и k.
func (m *Model) mouseWheel(e tea.Mouse) tea.Cmd {
	var dir int
	switch e.Button {
	case tea.MouseWheelDown:
		dir = 1
	case tea.MouseWheelUp:
		dir = -1
	default:
		return nil
	}
	if m.showing {
		m.setStatus("", false)
		m.card.step(dir)
		return nil
	}
	hit := view.HitBoard(m.layout(), e.X, e.Y)
	if hit.Col < 0 {
		return nil
	}
	m.setStatus("", false)
	m.moveCursor(hit.Col-m.board.Col, 0)
	m.moveCursor(0, dir)
	return nil
}
