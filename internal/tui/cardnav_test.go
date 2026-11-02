package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// In a card left/right and h/l open the neighbor task of the column; at the edge nothing.
func TestCard_LeftRight_OpensSiblingInColumn(t *testing.T) {
	e := mvNewEnv(t, "")
	a := e.create("A", "open")
	b := e.create("B", "open")
	c := e.create("C", "open")
	e.create("Other", "in_progress")
	m := newMouseModel(t, e, 140, 30)
	m.selectTask(b)
	feed(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	steps := []struct {
		key  tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: tea.KeyRight}, c},
		{tea.KeyPressMsg{Code: tea.KeyRight}, c}, // edge
		{tea.KeyPressMsg{Code: 'h', Text: "h"}, b},
		{tea.KeyPressMsg{Code: tea.KeyLeft}, a},
		{tea.KeyPressMsg{Code: tea.KeyLeft}, a}, // edge
		{tea.KeyPressMsg{Code: 'l', Text: "l"}, b},
	}
	for i, s := range steps {
		feed(m, s.key)
		if !m.showing || m.card.id != s.want || m.selID != s.want {
			t.Fatalf("step %d: card %q (open %v), expected %s", i, m.card.id, m.showing, s.want)
		}
	}
}
