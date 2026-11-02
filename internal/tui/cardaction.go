package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/tui/view"
)

// cardClick handles a click on the open card: a line selects (a criterion, a history entry), the box
// of a criterion toggles it, the buttons of the actions line run.
func (m *Model) cardClick(x, y int) tea.Cmd {
	hit := view.HitCard(m.cardView(), x, y)
	switch hit.Kind {
	case view.CardBox:
		m.setStatus("", false)
		m.selectCardLine(hit.Index)
		return m.toggleCriterion(hit.Index)
	case view.CardHistory:
		m.setStatus("", false)
		m.selectCardLine(m.card.nCrit + hit.Index)
	case view.CardCriterion:
		m.setStatus("", false)
		m.selectCardLine(hit.Index)
	case view.CardAction:
		m.setStatus("", false)
		return m.cardButton(hit.Key)
	}
	return nil
}

// selectCardLine puts the card cursor on a row of the card: criteria, then history entries; the window
// stays while the row is visible. A click only selects: the criterion is toggled by its box. The explicit move
// keeps the first history load from shifting the cursor like it does after plain openCard.
func (m *Model) selectCardLine(row int) {
	m.card.cursor = row
	m.card.moved = true
	m.fitScroll()
}

// startDelete asks for confirmation with one key: `y` deletes the task through the write queue,
// any other key cancels. From a card, the return to the board happens right after `y`.
func (m *Model) startDelete() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	id := t.ID
	m.confirm("delete "+id+"? y/n", func() tea.Cmd {
		if m.showing && m.card.id == id {
			m.showing = false
		}
		return m.submit(id, "removed", nil, func() (core.Result, error) {
			return m.d.Store.Remove(id, m.d.Actor)
		})
	})
	return nil
}

// cardEnter - enter in a card: on a criterion line it toggles the mark, on a series of edits it
// expands it, on any other history entry it switches between the last entries and all history.
func (m *Model) cardEnter() tea.Cmd {
	if m.card.cursor < m.card.nCrit {
		return m.toggleCriterion(m.card.cursor)
	}
	if !m.card.toggle() {
		m.card.toggleAll()
	}
	m.fitScroll()
	return nil
}

// toggleCriterion toggles a criterion: unchecked - Check, checked - uncheck by a body edit (Edit with
// base rev). The decision is taken from the fresh file inside the write queue: two enters in a row give
// a mark and an uncheck, even if the screen is not reread yet. The criterion text is compared with what
// the user saw; changed - refusal.
func (m *Model) toggleCriterion(idx int) tea.Cmd {
	t := m.byID(m.card.id)
	if t == nil {
		return nil
	}
	crit := t.Criteria()
	if idx < 0 || idx >= len(crit) {
		return nil
	}
	id, text := t.ID, crit[idx].Text
	return m.submit(id, "toggled", nil, func() (core.Result, error) {
		cur, rev, err := m.d.Store.Get(id)
		if err != nil {
			return core.Result{}, err
		}
		now := cur.Criteria()
		if idx >= len(now) || now[idx].Text != text {
			return core.Result{}, errors.New("criterion changed meanwhile, not toggled")
		}
		n := now[idx].N
		if !now[idx].Done {
			return m.d.Store.Check(id, []int{n}, m.d.Actor)
		}
		if _, err := cur.Uncheck(n); err != nil {
			return core.Result{}, err
		}
		res, err := m.d.Store.Edit(id, rev, cur.Body, m.d.Actor)
		res.Info = "unchecked"
		return res, err
	})
}

// cardButton - a press of an actions-line button: same as the key.
func (m *Model) cardButton(k string) tea.Cmd {
	switch k {
	case "del":
		return m.startDelete()
	case "esc":
		m.showing = false
		return nil
	}
	return m.taskKey(k)
}
