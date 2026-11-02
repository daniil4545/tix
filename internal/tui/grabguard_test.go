package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
)

func spaceKey() tea.Msg { return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "} }

// reload rereads the board the same way as after a file event.
func reload(t *testing.T, m *Model) {
	t.Helper()
	l, ok := m.loadCmd()().(loaded)
	if !ok {
		t.Fatal("loadCmd did not return loaded")
	}
	m.Update(l)
}

// An agent moved the grabbed task: the grab is dropped, a release writes nothing.
func TestGrab_AgentMovedHeldTask_GrabCancelledNoWrite(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("A", "open")
	m := newMouseModel(t, e, 140, 30)
	m.selectTask(id)
	feed(m, spaceKey())
	if _, err := e.store.Move(id, "in_progress", core.MoveOpts{}, "agent"); err != nil {
		t.Fatal(err)
	}
	before := e.commits()
	reload(t, m)
	if m.grab != nil || m.status != id+" moved meanwhile, grab cancelled" {
		t.Fatalf("grab %v, status %q", m.grab, m.status)
	}
	feed(m, spaceKey())
	if e.commits() != before || e.statusOf(t, id) != "in_progress" {
		t.Errorf("agent move was rolled back: status %s", e.statusOf(t, id))
	}
}

// Column "?" vanished on reread while the grab stood on it: keys do not crash.
func TestGrab_UnknownColumnVanishes_ArrowsStayInRange(t *testing.T) {
	e := mvNewEnv(t, "")
	a := e.create("A", "open")
	x := e.create("X", "open")
	m := newMouseModel(t, e, 140, 30)
	setRawStatus(t, e, x, "open", "ghost")
	reload(t, m)
	m.selectTask(a)
	feed(m, spaceKey())
	for range m.colTasks {
		feed(m, tea.KeyPressMsg{Code: tea.KeyRight})
	}
	if m.grab == nil || m.grab.col != len(m.colTasks)-1 {
		t.Fatalf("grab did not reach column ?: %+v", m.grab)
	}
	setRawStatus(t, e, x, "ghost", "idea")
	reload(t, m)
	feed(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	feed(m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if m.grab == nil || m.grab.col >= len(m.colTasks) {
		t.Errorf("grab column %+v is off a board of %d columns", m.grab, len(m.colTasks))
	}
}
