package tui

// Mouse and sticky windows: a hit uses the same window as drawn, a click does not move the window.
// The model is driven directly through Update: no terminal and no timers.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/tui/view"
)

// The roles are laid out so that a move between neighbors does not ask for a reason.
const stickyCfg = `[[column]]
name = "c1"
role = "waiting"

[[column]]
name = "c2"
role = "closed"

[[column]]
name = "c3"
role = "inbox"

[[column]]
name = "c4"
role = "active"

[[column]]
name = "c5"
role = "ready"
`

const twoColCfg = `[[column]]
name = "b"
role = "active"

[[column]]
name = "c"
role = "waiting"
`

func newMouseModel(t *testing.T, e *mvEnv, w, h int) *Model {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := New(ctx, Deps{Store: e.store, Root: e.root, Actor: "human"})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// feed gives the model a message and runs the returned write command, if there is one.
func feed(m *Model, msg tea.Msg) {
	_, cmd := m.Update(msg)
	if cmd == nil {
		return
	}
	if done, ok := cmd().(opDone); ok {
		m.Update(done)
	}
}

func typeKeys(m *Model, keys string) {
	for _, r := range keys {
		feed(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func leftClick(m *Model, x, y int) { feed(m, tea.MouseClickMsg(mvM(x, y, tea.MouseLeft))) }
func leftRelease(m *Model, x, y int) {
	feed(m, tea.MouseReleaseMsg(mvM(x, y, tea.MouseLeft)))
}

func (e *mvEnv) statusOf(t *testing.T, id string) string {
	t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return tk.Status
}

func firstShownColumn(m *Model) int {
	first, _ := view.FitWindow(m.layout())
	return first
}

// Risk 1: width 80, the cursor on the far column, window [c3 c4 c5]. A press on the leftmost
// column moves the cursor but the window stays: a release over c4 moves to c4, not to the column
// that would be there after the window jumps to [c1 c2 c3].
func TestMouse_Drag_ColumnWindowStaysAfterPress_MovesToDrawnColumn(t *testing.T) {
	e := mvNewEnv(t, stickyCfg)
	for i := 1; i <= 5; i++ {
		e.create(fmt.Sprintf("task-c%d", i), fmt.Sprintf("c%d", i))
	}
	m := newMouseModel(t, e, 80, 30)
	typeKeys(m, "llll")
	if first := firstShownColumn(m); first != 2 {
		t.Fatalf("expected window [c3 c4 c5], first column %d", first)
	}
	leftClick(m, mvX(0), mvY(0)) // task-c3
	if first := firstShownColumn(m); first != 2 {
		t.Fatalf("press moved the column window: first column %d, expected 2", first)
	}
	leftRelease(m, mvX(1), mvY(0)) // over the drawn c4
	if st := e.statusOf(t, "T-3"); st != "c4" {
		t.Errorf("T-3 in %q, expected c4 (input line=%v, status %q)", st, m.input != nil, m.status)
	}
}

// The same vertically: a click on the top visible card of a scrolled column does not move the window.
func TestMouse_Click_RowWindowStaysInScrolledColumn(t *testing.T) {
	e := mvNewEnv(t, stickyCfg)
	for i := 1; i <= 8; i++ {
		e.create(fmt.Sprintf("task-%d", i), "c1")
	}
	m := newMouseModel(t, e, 80, 24) // body 17 lines: three cards
	typeKeys(m, "jjjjjjj")
	if top := view.HitBoard(m.layout(), mvX(0), mvY(0)); top.Row != 5 {
		t.Fatalf("after scrolling the top card is 5, not %d", top.Row)
	}
	leftClick(m, mvX(0), mvY(0))
	leftRelease(m, mvX(0), mvY(0))
	if got := view.HitBoard(m.layout(), mvX(0), mvY(0)); got.Row != 5 {
		t.Errorf("click moved the row window: top card %d, expected 5", got.Row)
	}
	if sel := m.selected(); sel == nil || sel.ID != "T-6" {
		t.Errorf("selected %v, expected T-6", sel)
	}
	// A key past the window top moves the window.
	typeKeys(m, "kk")
	if got := view.HitBoard(m.layout(), mvX(0), mvY(0)); got.Row != 3 {
		t.Errorf("k past the window top: top card %d, expected 3", got.Row)
	}
}

// reopenWith rewrites config.toml and reopens the core: tasks outside the config go to column "?".
func reopenWith(t *testing.T, e *mvEnv, cfg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.root, ".tix", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := core.Open(e.root, core.Options{GitBin: mvGit(t)})
	if err != nil {
		t.Fatal(err)
	}
	e.store = s
}

func TestMouse_Drag_ToUnknownColumn_Refused(t *testing.T) {
	e := mvNewEnv(t, "[[column]]\nname = \"a\"\nrole = \"inbox\"\n\n"+twoColCfg)
	e.create("lost", "a")
	e.create("kept", "b")
	reopenWith(t, e, twoColCfg)
	m := newMouseModel(t, e, 140, 30) // columns b, c, ?
	leftClick(m, mvX(0), mvY(0))      // T-2 in b
	leftRelease(m, mvX(2), mvY(0))
	if m.status != "T-2: unknown column" || !m.isErr {
		t.Errorf("status %q (error=%v), expected T-2: unknown column", m.status, m.isErr)
	}
	if st := e.statusOf(t, "T-2"); m.input != nil || st != "b" {
		t.Errorf("move to ? was not refused: input line=%v, task status %s", m.input != nil, st)
	}
}

// The task changed column between press and release: a release over its new column is only a selection.
func TestMouse_Drag_TaskMovedMeanwhile_ReleaseOverItsColumnIsNoMove(t *testing.T) {
	e := mvNewEnv(t, twoColCfg)
	e.create("Alpha", "b")
	m := newMouseModel(t, e, 140, 30)
	leftClick(m, mvX(0), mvY(0))
	if _, err := e.store.Move("T-1", "c", core.MoveOpts{Message: "agent waits"}, "agent"); err != nil {
		t.Fatal(err)
	}
	tasks, errs := e.store.List()
	m.apply(tasks, len(errs))
	leftRelease(m, mvX(1), mvY(0))
	if m.input != nil {
		t.Errorf("release over the task current column opened line %q", m.input.label)
	}
	if st := e.statusOf(t, "T-1"); st != "c" || strings.Contains(m.status, "T-1") {
		t.Errorf("T-1: %s, status %q: expected the task in place and an empty status", st, m.status)
	}
}

// The terminal sends a click as a press and a release; a double click is two such pairs in a row.
func TestMouse_DoubleClick_WithReleases_OpensCard(t *testing.T) {
	e := mvNewEnv(t, twoColCfg)
	e.create("Alpha", "b")
	m := newMouseModel(t, e, 80, 24)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	leftClick(m, mvX(0), mvY(0))
	leftRelease(m, mvX(0), mvY(0))
	now = now.Add(150 * time.Millisecond)
	leftClick(m, mvX(0), mvY(0))
	leftRelease(m, mvX(0), mvY(0))
	if !m.showing {
		t.Fatal("double click with releases did not open the card")
	}
}
