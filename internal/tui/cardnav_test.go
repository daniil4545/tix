package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// В карточке left/right и h/l открывают соседнюю задачу колонки; на краю ничего.
func TestCard_LeftRight_OpensSiblingInColumn(t *testing.T) {
	e := mvNewEnv(t, stickyCfg)
	a := e.create("A", "c3")
	b := e.create("B", "c3")
	c := e.create("C", "c3")
	e.create("Other", "c4")
	m := newMouseModel(t, e, 140, 30)
	m.selectTask(b)
	feed(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	steps := []struct {
		key  tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: tea.KeyRight}, c},
		{tea.KeyPressMsg{Code: tea.KeyRight}, c}, // край
		{tea.KeyPressMsg{Code: 'h', Text: "h"}, b},
		{tea.KeyPressMsg{Code: tea.KeyLeft}, a},
		{tea.KeyPressMsg{Code: tea.KeyLeft}, a}, // край
		{tea.KeyPressMsg{Code: 'l', Text: "l"}, b},
	}
	for i, s := range steps {
		feed(m, s.key)
		if !m.showing || m.card.id != s.want || m.selID != s.want {
			t.Fatalf("шаг %d: карточка %q (открыта %v), ждали %s", i, m.card.id, m.showing, s.want)
		}
	}
}
