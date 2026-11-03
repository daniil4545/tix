package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/tui/view"
)

func pickKey(m *Model, keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			feed(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			feed(m, tea.KeyPressMsg{Code: tea.KeyEscape})
		case "down":
			feed(m, tea.KeyPressMsg{Code: tea.KeyDown})
		default:
			for _, r := range k {
				feed(m, tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
}

// pickModel - a board with a task of project p and one of project beta.
func pickModel(t *testing.T) *Model {
	t.Helper()
	e := mvNewEnv(t, "")
	e.create("A", "open")
	m := newMouseModel(t, e, 80, 20)
	if _, err := e.store.Create("B", "beta", "idea", "seed"); err != nil {
		t.Fatal(err)
	}
	tasks, _ := e.store.List()
	m.apply(tasks, 0)
	return m
}

// S1: p opens the list with all first, typing narrows, enter sets the filter, esc keeps the old one.
func TestPicker_OpenNarrowEnterEsc(t *testing.T) {
	m := pickModel(t)
	pickKey(m, "p")
	if m.pick == nil || m.pick.shown()[0].Name != "all" {
		t.Fatalf("list not open with all first: %+v", m.pick)
	}
	pickKey(m, "BE")
	if got := m.pick.shown(); len(got) != 1 || got[0].Name != "beta" {
		t.Fatalf("query BE: %+v", got)
	}
	pickKey(m, "enter")
	if m.pick != nil || m.project != "beta" || m.board.Project != "beta" {
		t.Fatalf("enter: pick %v project %q", m.pick, m.project)
	}
	pickKey(m, "p", "esc")
	if m.pick != nil || m.project != "beta" {
		t.Fatalf("esc changed the filter: pick %v project %q", m.pick, m.project)
	}
	pickKey(m, "p", "down", "enter") // the cursor starts on beta; one row down is p
	if m.project != "p" {
		t.Fatalf("down: %q", m.project)
	}
	pickKey(m, "p", "a", "l", "enter")
	if m.project != "" {
		t.Fatalf("all: %q", m.project)
	}
}

// S2: a catalog project with no tasks is in the list, with its count.
func TestPicker_CatalogProjectWithoutTasksShowsCount(t *testing.T) {
	m := pickModel(t)
	if _, err := m.d.Store.SetProjectPath("gamma", "", "seed"); err != nil {
		t.Fatal(err)
	}
	pickKey(m, "p", "gam")
	got := m.pick.shown()
	if len(got) != 1 || got[0].Name != "gamma" || got[0].Tasks != 0 || !got[0].Count {
		t.Fatalf("catalog project: %+v", got)
	}
	if out := m.View().Content; !strings.Contains(out, "gamma") || !strings.Contains(out, " 0") {
		t.Errorf("no gamma with count:\n%s", out)
	}
}

// S3: the list fits 80 columns and the frame height, and scrolls to the selected row.
func TestRenderPicker_FitsAndScrolls(t *testing.T) {
	var items []view.PickItem
	for i := 0; i < 30; i++ {
		items = append(items, view.PickItem{Name: strings.Repeat("n", 100) + string(rune('a'+i%26)), Count: true, Tasks: i})
	}
	for _, sel := range []int{0, 29} {
		out := view.RenderPicker(view.Picker{Query: "n", Items: items, Sel: sel, W: 80, H: 10})
		lines := strings.Split(out, "\n")
		if len(lines) > 10 {
			t.Fatalf("sel %d: %d lines > 10", sel, len(lines))
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > 80 {
				t.Fatalf("sel %d: line of %d columns: %q", sel, w, l)
			}
		}
		if !strings.Contains(out, "find:") {
			t.Errorf("sel %d: query line missing", sel)
		}
	}
	items[29].Name = "last"
	out := view.RenderPicker(view.Picker{Items: items, Sel: 29, W: 80, H: 10})
	if !strings.Contains(out, "last") {
		t.Errorf("selected last row not scrolled in:\n%s", out)
	}
}

// S2: the count is of open tasks; closed ones and a differently cased name do not add rows.
func TestPicker_CountsOpenTasksAndMergesCase(t *testing.T) {
	m := pickModel(t)
	r, err := m.d.Store.Create("C", "Beta", "idea", "seed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.d.Store.Move(r.ID, "closed", core.MoveOpts{Message: "gone", Force: true}, "seed"); err != nil {
		t.Fatal(err)
	}
	pickKey(m, "p", "bet")
	got := m.pick.shown()
	if len(got) != 1 || got[0].Tasks != 1 {
		t.Fatalf("beta rows: %+v", got)
	}
}
