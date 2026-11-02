package tui

// Mouse and sticky windows: a hit uses the same window as drawn, a click does not move the window.
// The model is driven directly through Update: no terminal and no timers.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/tui/view"
)

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

// Risk 1: width 80, window [in_progress blocked closed]. A press keeps the window,
// so a release over the drawn blocked moves to blocked.
func TestMouse_Drag_ColumnWindowStaysAfterPress_MovesToDrawnColumn(t *testing.T) {
	e := mvNewEnv(t, "")
	for _, st := range config.Statuses {
		e.create("task-"+st, st)
	}
	m := newMouseModel(t, e, 80, 30)
	typeKeys(m, "llll")
	if first := firstShownColumn(m); first != 2 {
		t.Fatalf("expected window [in_progress blocked closed], first column %d", first)
	}
	leftClick(m, mvX(0), mvY(0)) // task-in_progress
	if first := firstShownColumn(m); first != 2 {
		t.Fatalf("press moved the column window: first column %d, expected 2", first)
	}
	leftRelease(m, mvX(1), mvY(0)) // over the drawn blocked, which asks for a reason
	if m.input == nil {
		t.Fatalf("no reason line after the release over blocked (status %q)", m.status)
	}
	typeKeys(m, "x")
	feed(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if st := e.statusOf(t, "T-3"); st != "blocked" {
		t.Errorf("T-3 in %q, expected blocked (input line=%v, status %q)", st, m.input != nil, m.status)
	}
}

// The same vertically: a click on the top visible card of a scrolled column does not move the window.
func TestMouse_Click_RowWindowStaysInScrolledColumn(t *testing.T) {
	e := mvNewEnv(t, "")
	for i := 1; i <= 8; i++ {
		e.create(fmt.Sprintf("task-%d", i), "idea")
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

// setRawStatus rewrites the status line of a task file: a status outside the five goes to column "?".
func setRawStatus(t *testing.T, e *mvEnv, id, from, to string) {
	t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tk.Path)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(data), "status: "+from, "status: "+to, 1)
	if out == string(data) {
		t.Fatalf("no status in %s: %s", tk.Path, from)
	}
	if err := os.WriteFile(tk.Path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMouse_Drag_ToUnknownColumn_Refused(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("lost", "idea")
	e.create("kept", "in_progress")
	setRawStatus(t, e, "T-1", "idea", "ghost")
	m := newMouseModel(t, e, 200, 30) // columns idea .. closed, ?
	leftClick(m, mvX(2), mvY(0))      // T-2 in in_progress
	leftRelease(m, mvX(5), mvY(0))
	if m.status != "T-2: unknown column" || !m.isErr {
		t.Errorf("status %q (error=%v), expected T-2: unknown column", m.status, m.isErr)
	}
	if st := e.statusOf(t, "T-2"); m.input != nil || st != "in_progress" {
		t.Errorf("move to ? was not refused: input line=%v, task status %s", m.input != nil, st)
	}
}

// The task changed column between press and release: a release over its new column is only a selection.
func TestMouse_Drag_TaskMovedMeanwhile_ReleaseOverItsColumnIsNoMove(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "in_progress")
	m := newMouseModel(t, e, 140, 30)
	leftClick(m, mvX(2), mvY(0))
	if _, err := e.store.Move("T-1", "blocked", core.MoveOpts{Message: "agent waits"}, "agent"); err != nil {
		t.Fatal(err)
	}
	tasks, errs := e.store.List()
	m.apply(tasks, len(errs))
	leftRelease(m, mvX(3), mvY(0))
	if m.input != nil {
		t.Errorf("release over the task current column opened line %q", m.input.label)
	}
	if st := e.statusOf(t, "T-1"); st != "blocked" || strings.Contains(m.status, "T-1") {
		t.Errorf("T-1: %s, status %q: expected the task in place and an empty status", st, m.status)
	}
}

// The terminal sends a click as a press and a release; a double click is two such pairs in a row.
func TestMouse_DoubleClick_WithReleases_OpensCard(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
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
