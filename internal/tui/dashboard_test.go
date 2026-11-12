package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
)

func TestEpicLabel_FromChildren(t *testing.T) {
	if got := epicLabel([2]int{1, 2}); got != "EPIC 1/2" {
		t.Errorf("epicLabel = %q, expected EPIC 1/2", got)
	}
	if got := epicLabel([2]int{}); got != "" {
		t.Errorf("a task without children got the label %q", got)
	}
}

// A parent shows EPIC n/m by its children; del on it shows the refusal of the core in the status line.
func TestBoard_ParentCardShowsProgress_DeleteRefused(t *testing.T) {
	e := mvNewEnv(t, "")
	parent := e.create("Parent", "open")
	c1 := e.create("Child one", "closed")
	c2 := e.create("Child two", "in_progress")
	e.set(c1, "parent", parent)
	e.set(c2, "parent", parent)
	e.acStartIn("", 160, 30)
	e.screenHas("EPIC 1/2")
	e.tm.Send(tea.KeyPressMsg{Code: tea.KeyRight}) // the cursor starts on the empty idea column
	e.acPressDelete("delete")
	e.screenHas("delete " + parent + "? y/n")
	e.key("y")
	e.screenHas(parent+": ", "children")
	if _, _, err := e.store.Get(parent); err != nil {
		t.Fatalf("the parent was removed: %v", err)
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

// A core notice (a child reopened under a closed parent) goes to the status line.
func TestReport_NoticeInStatusLine(t *testing.T) {
	m := &Model{}
	m.report(opDone{id: "T-2", res: core.Result{Notices: []string{"warning: parent T-1 is closed"}}})
	if got, _ := m.statusText(); got != "T-2: warning: parent T-1 is closed" {
		t.Errorf("status = %q", got)
	}
}
