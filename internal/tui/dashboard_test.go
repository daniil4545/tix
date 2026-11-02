package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/task"
)

func TestEpicLabel_CountsTaskLinesOnly(t *testing.T) {
	src := "---\nid: T-1\ntitle: E\nproject: p\nstatus: open\nrank: 1\ncreated: 2026-11-02T09:00:00\ntags: [epic]\n---\n" +
		"## Details\n- [x] T-2 first\n- [ ] T-3 second\n- [ ] plain criterion\n"
	tk, err := task.Parse("T-1.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := epicLabel(tk); got != "EPIC 1/2" {
		t.Errorf("epicLabel = %q, expected EPIC 1/2", got)
	}
	tk.Tags = nil
	if got := epicLabel(tk); got != "" {
		t.Errorf("a plain task got the label %q", got)
	}
}

// With a project filter the card line drops the project name: the header names it.
func TestBoardRows_ProjectNameOnlyWithoutFilter(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("A", "open")
	m := newMouseModel(t, e, 140, 30)
	m.project = ""
	m.rebuild()
	if got := m.board.Cols[1].Rows[0].Project; got == "" {
		t.Error("all projects: the project name is missing")
	}
	m.project = "p"
	m.rebuild()
	if got := m.board.Cols[1].Rows[0].Project; got != "" {
		t.Errorf("filtered: project name %q stayed on the card", got)
	}
}

// The card shows the last historyTail rows; enter on an entry shows all, enter again goes back.
func TestCard_HistoryCollapsedToLastRows_EnterTogglesAll(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("A", "open")
	for i := 0; i < 12; i++ {
		e.note(id, "n"+string(rune('a'+i)))
	}
	m := newMouseModel(t, e, 140, 60)
	m.selectTask(id)
	caOpenCard(m)
	if n := len(m.card.rows); n != historyTail {
		t.Fatalf("rows shown %d, expected %d", n, historyTail)
	}
	if h := m.cardView().HistoryHint; !strings.HasPrefix(h, "last 5 of ") || !strings.HasSuffix(h, "enter: all") {
		t.Errorf("hint %q", h)
	}
	last := m.card.rows[len(m.card.rows)-1].entry
	feed(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.card.rows) <= historyTail || !strings.HasPrefix(m.cardView().HistoryHint, "all ") {
		t.Fatalf("enter did not show all: %d rows, hint %q", len(m.card.rows), m.cardView().HistoryHint)
	}
	if got := m.card.rows[m.card.cursor-m.card.nCrit].entry; got.At != last.At || got.Text != last.Text {
		t.Errorf("the cursor left its entry: %+v", got)
	}
	feed(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.card.rows) != historyTail {
		t.Errorf("second enter: %d rows, expected %d", len(m.card.rows), historyTail)
	}
}
