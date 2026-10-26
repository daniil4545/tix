package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/tui/view"
)

// doubleClick - the window of the second click on the same card (the model clock).
const doubleClick = 400 * time.Millisecond

// press - a left button press on a card: the task id remembered at the press.
type press struct{ id string }

// click - the last click on a card, for the double click.
type click struct {
	id string
	at time.Time
}

// mouse handles the mouse. Mouse mode is on in the board and in the card (View). With an input line
// a left click goes to inputClick, the rest is ignored; during a grab the mouse is ignored.
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
		// X10 reports a release without a button; right and middle do not drag.
		if mouse.Button == tea.MouseLeft || mouse.Button == tea.MouseNone {
			return m.mouseRelease(mouse)
		}
	case tea.MouseWheelMsg:
		return m.mouseWheel(mouse)
	}
	return nil
}

// layout - the board as drawn right now: hit testing is computed from it.
func (m *Model) layout() view.Board {
	b := m.board
	b.W, b.H = m.w, m.h
	return b
}

// selectAt puts the cursor on the card (col, row) of the board.
func (m *Model) selectAt(col, row int) {
	m.board.Col, m.board.Row = col, row
	m.clampRow()
	m.syncSelID()
	m.fitWindow()
}

// mouseClick: a click on a card selects it; a second one within doubleClick on the same task opens it.
// The press is remembered for a drag. A hint button equals its key, "+" in a column name -
// a new task in it. A click off the cards does nothing; in a task card - cardClick.
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

// mouseRelease moves the task pressed by the mouse to the column under the cursor: through the same moveTo
// as the m key (checks, the reason line). Over the same column and outside columns - nothing.
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
	// A release over its own column is the second half of a click: the click memory stays for a double click.
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

// configured: the column is described in config.toml.
func (m *Model) configured(name string) bool {
	return slices.ContainsFunc(m.cfg.Columns, func(c config.Column) bool { return c.Name == name })
}

// selectTask puts the cursor on task id; false if it is not on the board (project filter).
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

// mouseWheel: in a task card - a scroll of the list by lines, the selection stays; on the board -
// choose the column under the cursor (as h and l before it), then a step by one card, like j and k.
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
		base := m.card.scroll
		if m.card.follow {
			// The view position right now: the state offset is behind the fit while it follows.
			base = view.CardScroll(m.cardView())
		}
		m.card.scrollBy(base, dir)
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
