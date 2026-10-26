package tui

// Tests of the MVP finishing work.
// The package is internal: the model clock (field now) is not reachable from outside. A separate mini-harness with prefix mv:
// the shared harness lives in package tui_test. The environment (HOME, git) comes from TestMain in harness_test.go.
// Written from the spec, without reading the code of zones A and B.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/daniil4545/tix/internal/core"
)

var mvANSI = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

// layout
const (
	mvColStep  = 24 + 2
	mvFirstRow = 4
	mvCardRows = 5
)

func mvX(slot int) int { return slot*mvColStep + 1 }
func mvY(row int) int  { return mvFirstRow + row*mvCardRows + 1 }

const (
	mvCfgWork = `[[column]]
name = "work"
role = "active"

[[column]]
name = "wait"
role = "waiting"

[[column]]
name = "fin"
role = "closed"
`
)

type mvPing struct{}

type mvProbe struct {
	tea.Model
	e *mvEnv
}

func (p mvProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := p.Model.Update(msg)
	p.Model = m
	switch msg.(type) {
	case tea.MouseMsg:
		p.e.mouseSeen.Add(1)
	case mvPing:
		p.e.pingSeen.Add(1)
		p.e.viewsAtPing.Store(p.e.views.Load())
	}
	return p, cmd
}

func (p mvProbe) View() tea.View {
	v := p.Model.View()
	p.e.mu.Lock()
	p.e.screen = mvANSI.ReplaceAllString(v.Content, "")
	p.e.mu.Unlock()
	p.e.views.Add(1)
	return v
}

type mvEnv struct {
	t     *testing.T
	root  string
	store *core.Store
	model *Model
	tm    *teatest.TestModel

	mu     sync.Mutex
	screen string

	clock                                             atomic.Int64
	mouseSent                                         int
	mouseSeen, pingSeen, pingSent, views, viewsAtPing atomic.Int64
}

func mvGit(t *testing.T) string {
	t.Helper()
	g, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("git not found")
	}
	return g
}

func mvNewEnv(t *testing.T, config string) *mvEnv {
	t.Helper()
	return mvNewEnvGit(t, config, mvGit(t))
}

func mvNewEnvGit(t *testing.T, config, gitBin string) *mvEnv {
	t.Helper()
	base, err := os.MkdirTemp("", "tix-mvp-")
	if err != nil {
		t.Fatal(err)
	}
	// the write queue may still add a commit after the test has exited: wait until git releases
	// the index lock, and remove with retries until a deadline
	t.Cleanup(func() {
		lock := filepath.Join(base, "tasks", ".git", "index.lock")
		for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(lock); err == nil {
				continue
			}
			if os.RemoveAll(base) == nil {
				return
			}
		}
		t.Errorf("could not remove %s", base)
	})
	root := filepath.Join(base, "tasks")
	if config != "" {
		if err := os.MkdirAll(filepath.Join(root, ".tix"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".tix", "config.toml"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := core.Open(root, core.Options{GitBin: gitBin})
	if err != nil {
		t.Fatal(err)
	}
	e := &mvEnv{t: t, root: root, store: s}
	e.clock.Store(time.Now().UnixNano())
	return e
}

func (e *mvEnv) create(title, column string) string {
	e.t.Helper()
	return e.createIn(title, "p", column)
}

func (e *mvEnv) createIn(title, project, column string) string {
	e.t.Helper()
	return SeedTask(e.t, e.store, e.root, title, project, column)
}

func (e *mvEnv) set(id, name, value string) {
	e.t.Helper()
	if _, err := e.store.Set(id, []core.Field{{Name: name, Value: value}}, "seed"); err != nil {
		e.t.Fatalf("Set %s %s: %v", id, name, err)
	}
}

func (e *mvEnv) summary(id, text string) {
	e.t.Helper()
	_, rev, err := e.store.Get(id)
	if err == nil {
		_, err = e.store.Edit(id, rev, "## Summary\n"+text+"\n", "seed")
	}
	if err != nil {
		e.t.Fatalf("summary %s: %v", id, err)
	}
}

func (e *mvEnv) note(id, text string) {
	e.t.Helper()
	if _, err := e.store.Note(id, text, "agent"); err != nil {
		e.t.Fatalf("Note %s: %v", id, err)
	}
}

// setClock sets the model time. Call it when all sent events are handled (mouse and settle wait for that).
func (e *mvEnv) setClock(t time.Time) { e.clock.Store(t.UnixNano()) }

func (e *mvEnv) start(w, h int) {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.t.Cleanup(cancel)
	e.model = New(ctx, Deps{Store: e.store, Root: e.root, Actor: "human"})
	e.model.now = func() time.Time { return time.Unix(0, e.clock.Load()) }
	e.tm = teatest.NewTestModel(e.t, mvProbe{Model: e.model, e: e}, teatest.WithInitialTermSize(w, h))
	e.t.Cleanup(func() {
		_ = e.tm.Quit()
		e.tm.WaitFinished(e.t, teatest.WithFinalTimeout(5*time.Second))
		_, _ = e.store.Note("T-0", "drain", "test") // waits for a commit the board writer still runs
	})
}

func (e *mvEnv) text() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.screen
}

func (e *mvEnv) lines() []string { return strings.Split(e.text(), "\n") }

// yOf - the screen line with the first occurrence of sub; -1 if none.
func (e *mvEnv) yOf(sub string) int {
	for y, ln := range e.lines() {
		if strings.Contains(ln, sub) {
			return y
		}
	}
	return -1
}

func (e *mvEnv) until(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("did not wait for: %s\nscreen:\n%s", what, e.text())
}

func (e *mvEnv) screenHas(subs ...string) {
	e.t.Helper()
	e.until(fmt.Sprint("on screen ", subs), func() bool {
		s := e.text()
		for _, sub := range subs {
			if !strings.Contains(s, sub) {
				return false
			}
		}
		return true
	})
}

// settle waits until the model handles everything sent earlier and the screen is redrawn after that.
func (e *mvEnv) settle() {
	e.t.Helper()
	want := e.pingSent.Add(1)
	e.tm.Send(mvPing{})
	e.until("ping handled", func() bool { return e.pingSeen.Load() >= want })
	base := e.viewsAtPing.Load()
	e.until("frame after the ping", func() bool { return e.views.Load() > base })
}

func (e *mvEnv) key(keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			e.tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			e.tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
		default:
			for _, r := range k {
				e.tm.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
}

// mouse sends a mouse event and waits until the model handles it (the clock can be changed after it returns).
func (e *mvEnv) mouse(msg tea.MouseMsg) {
	e.t.Helper()
	e.mouseSent++
	e.tm.Send(msg)
	want := int64(e.mouseSent)
	e.until("mouse event handled", func() bool { return e.mouseSeen.Load() >= want })
}

func mvM(x, y int, b tea.MouseButton) tea.Mouse { return tea.Mouse{X: x, Y: y, Button: b} }

// click - a left press and release at one point.
func (e *mvEnv) click(x, y int) {
	e.t.Helper()
	e.mouse(tea.MouseClickMsg(mvM(x, y, tea.MouseLeft)))
	e.mouse(tea.MouseReleaseMsg(mvM(x, y, tea.MouseLeft)))
}

func (e *mvEnv) wheel(x, y int, down bool) {
	e.t.Helper()
	b := tea.MouseWheelUp
	if down {
		b = tea.MouseWheelDown
	}
	e.mouse(tea.MouseWheelMsg(mvM(x, y, b)))
}

// drag - a left press at (x1,y1), a move and a release at (x2,y2).
func (e *mvEnv) press(x, y int) { e.t.Helper(); e.mouse(tea.MouseClickMsg(mvM(x, y, tea.MouseLeft))) }
func (e *mvEnv) release(x, y int) {
	e.t.Helper()
	e.mouse(tea.MouseMotionMsg(mvM(x, y, tea.MouseLeft)))
	e.mouse(tea.MouseReleaseMsg(mvM(x, y, tea.MouseLeft)))
}

func (e *mvEnv) status(id string) (status, waiting string) {
	e.t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		e.t.Fatalf("Get %s: %v", id, err)
	}
	return tk.Status, tk.Waiting
}

func (e *mvEnv) git(args ...string) string {
	e.t.Helper()
	out, err := exec.CommandContext(e.t.Context(), mvGit(e.t), append([]string{"-C", e.root}, args...)...).CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (e *mvEnv) commits() string { return e.git("rev-list", "--count", "HEAD") }

// opened: the card of task title is open, no board.
func (e *mvEnv) opened(title string, others ...string) {
	e.t.Helper()
	e.until("card open: "+title, func() bool {
		s := e.text()
		if !strings.Contains(s, title) {
			return false
		}
		for _, o := range others {
			if strings.Contains(s, o) {
				return false
			}
		}
		return true
	})
}

// onBoard: the board is on the screen with all the listed cards (the check "the card did not open").
func (e *mvEnv) onBoard(titles ...string) {
	e.t.Helper()
	e.settle()
	s := e.text()
	for _, ti := range append([]string{"enter open"}, titles...) {
		if !strings.Contains(s, ti) {
			e.t.Fatalf("expected the board with %q, screen:\n%s", ti, s)
		}
	}
}

// selectedIs opens the selected card with the enter key and checks it, then returns to the board.
func (e *mvEnv) selectedIs(title string, others ...string) {
	e.t.Helper()
	e.settle()
	e.key("enter")
	e.opened(title, others...)
	e.key("esc")
	e.screenHas("enter open")
}

// awaitWatcher waits until the watcher is running (it starts after the first frame): edits the title
// of a service task until the board shows the new one. Without it an external edit right after start is lost.
func (e *mvEnv) awaitWatcher(canary string) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for i := 0; time.Now().Before(deadline); i++ {
		title := fmt.Sprintf("canary%d", i)
		e.set(canary, "title", title)
		for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if strings.Contains(e.text(), title) {
				return
			}
		}
	}
	e.t.Fatalf("watcher did not notice the file edit\nscreen:\n%s", e.text())
}

// ---------------------------------------------------------------------------------------------
// Mouse mode
// ---------------------------------------------------------------------------------------------

// Mouse mode - CellMotion on the board and in the card.
func TestMouse_BoardAndCard_CellMotionMode(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := New(ctx, Deps{Store: e.store, Root: e.root, Actor: "human"})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("board: MouseMode=%v, expected CellMotion", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.showing {
		t.Fatal("enter did not open the card")
	}
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("card: MouseMode=%v, expected CellMotion", got)
	}
}

// A click selects a card (column and row); any of the five card lines and both column edges - the same card.
func TestMouse_Click_SelectsCardByGrid(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	e.create("Charlie", "todo")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo", "Charlie")

	cases := []struct {
		name, want string
		x, y       int
	}{
		{"other column, title line", "Charlie", mvX(1), mvY(0)},
		{"second card, fifth (empty) line", "Bravo", 3, mvFirstRow + mvCardRows + 4},
		{"first card, right column edge", "Alpha", mvColStep*0 + 23, mvFirstRow},
		{"left edge of the second column", "Charlie", mvColStep, mvFirstRow + 2},
	}
	for _, c := range cases {
		e.click(c.x, c.y)
		e.selectedIs(c.want)
		if t.Failed() {
			t.Fatalf("case %q: click (%d,%d)", c.name, c.x, c.y)
		}
	}
}

// A click on an empty place, a gap, the header; right and middle buttons; a release without a press - nothing.
func TestMouse_NonActions_KeepSelectionAndCommits(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.key("j") // Bravo selected
	e.settle()
	before := e.commits()

	steps := []struct {
		name string
		do   func()
	}{
		{"gap between columns", func() { e.click(24, mvY(0)) }},
		{"gap, second character", func() { e.click(25, mvY(0)) }},
		{"header", func() { e.click(5, 0) }},
		{"line under the header", func() { e.click(5, 1) }},
		{"empty under the cards", func() { e.click(3, mvY(2)) }},
		{"empty column", func() { e.click(mvX(4), mvY(0)) }},
		{"right button on Alpha", func() {
			e.mouse(tea.MouseClickMsg(mvM(3, mvY(0), tea.MouseRight)))
			e.mouse(tea.MouseReleaseMsg(mvM(3, mvY(0), tea.MouseRight)))
		}},
		{"middle button on Alpha", func() {
			e.mouse(tea.MouseClickMsg(mvM(3, mvY(0), tea.MouseMiddle)))
			e.mouse(tea.MouseReleaseMsg(mvM(3, mvY(0), tea.MouseMiddle)))
		}},
		{"release without a press on Alpha", func() { e.mouse(tea.MouseReleaseMsg(mvM(3, mvY(0), tea.MouseLeft))) }},
	}
	for _, s := range steps {
		s.do()
		e.settle()
		if !strings.Contains(e.text(), "enter open") {
			t.Fatalf("%s: the board vanished, screen:\n%s", s.name, e.text())
		}
	}
	e.selectedIs("Bravo", "Alpha")
	if after := e.commits(); after != before {
		t.Errorf("mouse without an action wrote commits: %s -> %s", before, after)
	}
}

// Risk 2: a double click opens only the same card and only within 400 ms by the model clock.
func TestMouse_DoubleClick_ClockAndSameCard(t *testing.T) {
	type click struct {
		after time.Duration // pause after the previous click
		slot  int           // column
		row   int
	}
	cases := []struct {
		name   string
		clicks []click
		open   string // "" - the card did not open
		sel    string // finally selected
	}{
		{"same clicks within 100 ms", []click{{0, 0, 0}, {100 * time.Millisecond, 0, 0}}, "Alpha", "Alpha"},
		{"pause 500 ms", []click{{0, 0, 0}, {500 * time.Millisecond, 0, 0}}, "", "Alpha"},
		{"pause 3 s", []click{{0, 0, 0}, {3 * time.Second, 0, 0}}, "", "Alpha"},
		{"two different cards within 50 ms", []click{{0, 0, 0}, {50 * time.Millisecond, 0, 1}}, "", "Bravo"},
		{"cards in different columns within 50 ms", []click{{0, 0, 0}, {50 * time.Millisecond, 1, 0}}, "", "Charlie"},
		{"A, B, A fast", []click{{0, 0, 0}, {50 * time.Millisecond, 0, 1}, {50 * time.Millisecond, 0, 0}}, "", "Alpha"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := mvNewEnv(t, "")
			e.create("Alpha", "idea")
			e.create("Bravo", "idea")
			e.create("Charlie", "todo")
			e.start(140, 30)
			e.screenHas("Alpha", "Bravo", "Charlie")
			now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
			e.setClock(now)
			for _, k := range c.clicks {
				now = now.Add(k.after)
				e.setClock(now)
				e.click(mvX(k.slot), mvY(k.row))
			}
			if c.open != "" {
				e.opened(c.open)
				return
			}
			e.onBoard("Alpha", "Bravo", "Charlie")
			others := []string{"Alpha", "Bravo", "Charlie"}
			e.selectedIs(c.sel, func() []string {
				var o []string
				for _, x := range others {
					if x != c.sel {
						o = append(o, x)
					}
				}
				return o
			}()...)
		})
	}
}

// Risk 1: the wheel in a scrolled column moves the selection by one card, not by the pointer position.
func TestMouse_Wheel_ScrolledColumn_StepsOneCard(t *testing.T) {
	e := mvNewEnv(t, "")
	for i := 1; i <= 12; i++ {
		e.create(fmt.Sprintf("Card%02d", i), "idea")
	}
	e.start(140, 24)
	e.screenHas("Card01")
	e.key("j", "j", "j", "j") // Card05: the column window is scrolled
	e.settle()
	e.selectedIs("Card05")

	steps := []struct {
		down bool
		want string
	}{{true, "Card06"}, {false, "Card05"}, {false, "Card04"}, {true, "Card05"}}
	for _, s := range steps {
		e.wheel(3, mvFirstRow+1, s.down)
		e.selectedIs(s.want)
		if t.Failed() {
			t.Fatalf("after the wheel down=%v expected selection %s", s.down, s.want)
		}
	}
}

// The wheel over a non-selected column - first the column choice, then a step.
func TestMouse_Wheel_OtherColumn_SelectsColumnThenSteps(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Beta1", "todo")
	e.create("Beta2", "todo")
	e.create("Beta3", "todo")
	e.start(140, 30)
	e.screenHas("Alpha", "Beta3")
	e.wheel(mvX(1), mvY(0), true)
	e.selectedIs("Beta2", "Alpha", "Beta1", "Beta3")
}

// On the card screen the wheel down and up = j and k; a click does nothing.
func TestMouse_CardScreen_WheelScrollsLikeJK_ClickIgnored(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	for i := 1; i <= 30; i++ {
		if _, err := e.store.Comment("T-1", fmt.Sprintf("remark-%02d", i), "agent"); err != nil {
			t.Fatal(err)
		}
	}
	e.start(100, 14)
	e.screenHas("Alpha")
	e.key("enter")
	e.opened("Alpha")
	e.screenHas("remark-")
	e.settle()
	s0 := e.text()

	e.key(strings.Repeat("k", 15)) // the history opens on the newest entry: k goes up
	e.settle()
	s1 := e.text()
	if s1 == s0 {
		t.Fatal("15 presses of k did not move the history: the scenario does not check scrolling")
	}
	for i := 0; i < 15; i++ {
		e.wheel(10, 6, true) // down = j
	}
	e.settle()
	if got := e.text(); got != s0 {
		t.Errorf("15 wheel-downs did not return the screen to the start (like 15 j):\n%s\nexpected:\n%s", got, s0)
	}
	for i := 0; i < 15; i++ {
		e.wheel(10, 6, false) // up = k
	}
	e.settle()
	if got := e.text(); got != s1 {
		t.Errorf("15 wheel-ups did not repeat 15 presses of k:\n%s\nexpected:\n%s", got, s1)
	}
	e.click(10, 6)
	e.settle()
	if got := e.text(); got != s1 {
		t.Errorf("click in the card changed the screen:\n%s", got)
	}
}

// In the open card the history window shifts only when the cursor moves over the top or bottom
// edge, as a board column does (RowTop). The card opens with the cursor on the newest entry.
func TestCard_StepUpFromBottom_KeepsWindow(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	for i := 1; i <= 30; i++ {
		if _, err := e.store.Comment("T-1", fmt.Sprintf("remark-%02d", i), "agent"); err != nil {
			t.Fatal(err)
		}
	}
	e.start(100, 14)
	e.screenHas("Alpha")
	e.key("enter")
	e.opened("Alpha")
	e.screenHas("remark-")
	e.settle()
	s0 := e.text()
	first0 := e.lines()[3:5] // the first body entry: heading and text

	e.key("k") // one step up: the cursor is still inside the window
	e.settle()
	if got := e.lines()[3:5]; got[0] != first0[0] || got[1] != first0[1] {
		t.Errorf("one step up from the bottom shifted the first visible entry from %q, %q to %q, %q:\n%s",
			first0[0], first0[1], got[0], got[1], e.text())
	}

	e.key(strings.Repeat("k", 30)) // up to the top: the window follows
	e.settle()
	if got := e.lines()[4]; got == first0[1] {
		t.Errorf("30 steps up did not move the window from the bottom edge fit: the scenario does not exercise the other view:\n%s", e.text())
	}

	e.key(strings.Repeat("j", 30)) // down again: the window shifts back at the edge, cursor on the bottom edge
	e.settle()
	if got := e.text(); got != s0 {
		t.Errorf("30 steps down did not restore the bottom fit:\n%s\nexpected:\n%s", got, s0)
	}
}

// A drag to waiting asks for a reason; esc - the task stays; input - the task is moved.
func TestMouse_Drag_ToWaiting_AsksReason(t *testing.T) {
	t.Run("esc leaves the task in place", func(t *testing.T) {
		e := mvNewEnv(t, mvCfgWork)
		e.create("Alpha", "work")
		e.create("Bravo", "work")
		e.start(140, 30)
		e.screenHas("Alpha", "Bravo")
		before := e.commits()
		e.press(3, mvY(0))
		e.release(mvX(1)+2, mvY(0))
		e.screenHas("waiting for:")
		e.key("esc")
		e.until("input line closed", func() bool { return !strings.Contains(e.text(), "waiting for:") })
		if st, _ := e.status("T-1"); st != "work" || e.commits() != before {
			t.Errorf("cancel changed the task: status %s, commits %s -> %s", st, before, e.commits())
		}
	})
	t.Run("reason and enter move", func(t *testing.T) {
		e := mvNewEnv(t, mvCfgWork)
		e.create("Alpha", "work")
		e.create("Bravo", "work")
		e.start(140, 30)
		e.screenHas("Alpha", "Bravo")
		e.press(3, mvY(1)) // Bravo
		e.release(mvX(1), mvY(0))
		e.screenHas("waiting for:")
		e.tm.Type("vendor reply")
		e.key("enter")
		e.until("T-2 in wait with a reason", func() bool { st, w := e.status("T-2"); return st == "wait" && w == "vendor reply" })
		if st, _ := e.status("T-1"); st != "work" {
			t.Errorf("T-1 moved together with T-2: %s", st)
		}
	})
}

// A release over the same column or outside columns - no move; the right button does not drag.
func TestMouse_Drag_NoTarget_NoCommit(t *testing.T) {
	cases := []struct {
		name               string
		pressX             int
		pressY             int
		releaseX, releaseY int
		btn                tea.MouseButton
	}{
		{"same column, other card", 3, mvY(0), 3, mvY(1), tea.MouseLeft},
		{"gap between columns", 3, mvY(0), 25, mvY(0), tea.MouseLeft},
		{"header", 3, mvY(0), mvX(1), 0, tea.MouseLeft},
		{"status line under the columns", 3, mvY(0), mvX(1), 28, tea.MouseLeft},
		{"right button to another column", 3, mvY(0), mvX(1), mvY(0), tea.MouseRight},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := mvNewEnv(t, mvCfgWork)
			e.create("Alpha", "work")
			e.create("Bravo", "work")
			e.start(140, 30)
			e.screenHas("Alpha", "Bravo")
			before := e.commits()
			e.mouse(tea.MouseClickMsg(mvM(c.pressX, c.pressY, c.btn)))
			e.mouse(tea.MouseReleaseMsg(mvM(c.releaseX, c.releaseY, c.btn)))
			e.settle()
			if strings.Contains(e.text(), "waiting for:") || strings.Contains(e.text(), "close reason:") {
				t.Errorf("the move input line appeared:\n%s", e.text())
			}
			for _, id := range []string{"T-1", "T-2"} {
				if st, _ := e.status(id); st != "work" {
					t.Errorf("%s moved to %s", id, st)
				}
			}
			if after := e.commits(); after != before {
				t.Errorf("commits %s -> %s", before, after)
			}
		})
	}
}

// The task is gone by the time of release - T-N removed, no commits from us.
func TestMouse_Drag_TaskRemovedMeanwhile_ReportsRemoved(t *testing.T) {
	e := mvNewEnv(t, mvCfgWork)
	e.create("Alpha", "work")
	e.create("Bravo", "work")
	canary := e.create("canary", "fin")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.awaitWatcher(canary)
	e.press(3, mvY(0))
	if _, err := e.store.Remove("T-1", "agent"); err != nil {
		t.Fatal(err)
	}
	e.until("Alpha left the board", func() bool { return !strings.Contains(e.text(), "Alpha") })
	after := e.commits()
	e.release(mvX(1), mvY(0))
	e.screenHas("T-1 removed")
	if e.commits() != after {
		t.Errorf("moving a deleted task wrote a commit")
	}
	if st, _ := e.status("T-2"); st != "work" {
		t.Errorf("T-2 moved instead of the deleted one: %s", st)
	}
}

// The task remembered at the press is moved, not the one that ended up in that place by the release.
func TestMouse_Drag_BoardShiftsBetweenPressAndRelease_MovesPressedTask(t *testing.T) {
	e := mvNewEnv(t, mvCfgWork)
	e.create("Alpha", "work")
	e.create("Bravo", "work")
	canary := e.create("canary", "fin")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.awaitWatcher(canary)
	e.press(3, mvY(1)) // Bravo
	if _, err := e.store.Move("T-1", "fin", core.MoveOpts{Message: "done by agent", Force: true}, "agent"); err != nil {
		t.Fatal(err)
	}
	e.until("Bravo rose to the first place", func() bool {
		ls := e.lines()
		return len(ls) > mvY(0) && strings.Contains(ls[mvY(0)], "Bravo")
	})
	e.release(mvX(1), mvY(0))
	e.screenHas("waiting for:")
	e.tm.Type("why")
	e.key("enter")
	e.until("T-2 in wait", func() bool { st, _ := e.status("T-2"); return st == "wait" })
	if st, _ := e.status("T-1"); st != "fin" {
		t.Errorf("T-1: %s, expected fin (must not move)", st)
	}
}

var mvDefaultCols = []string{"idea", "todo", "doing", "blocked", "done"}

// visibleColumns - the default column names from the header (line 2), in screen order.
func (e *mvEnv) visibleColumns() []string {
	hdr := e.lines()[2]
	var names []string
	for x := 0; x < len(hdr); x++ {
		for _, n := range mvDefaultCols {
			if strings.HasPrefix(hdr[x:], n+" ") && (x == 0 || hdr[x-1] == ' ') {
				names = append(names, n)
			}
		}
	}
	return names
}

// Risk 1: the column window is shifted (width 80, the cursor on the far column) - a hit goes by the window slot.
// The window depends on the cursor, so the header is reread after each action: wait for what is drawn.
func TestMouse_ColumnWindowShifted_ClickAndDragUseVisibleSlots(t *testing.T) {
	ids := map[string]string{}
	setup := func(t *testing.T) *mvEnv {
		e := mvNewEnv(t, "")
		for _, n := range mvDefaultCols {
			ids[n] = e.create("task-"+n, n)
		}
		e.start(80, 24)
		e.screenHas("task-idea")
		e.key("l", "l", "l", "l") // cursor on done
		e.settle()
		e.until("column done visible", func() bool { return len(e.visibleColumns()) > 0 && strings.Contains(e.lines()[2], "done") })
		if names := e.visibleColumns(); len(names) < 2 || names[0] == "idea" {
			t.Fatalf("window did not shift: visible %v, header %q", names, e.lines()[2])
		}
		return e
	}

	t.Run("click selects the drawn column", func(t *testing.T) {
		e := setup(t)
		for slot := 0; slot < 3; slot++ {
			names := e.visibleColumns()
			if slot >= len(names) {
				break
			}
			want := "task-" + names[slot]
			e.click(mvX(slot), mvY(0))
			e.settle()
			e.key("enter")
			e.opened(want)
			e.key("esc")
			e.screenHas("enter open")
		}
	})
	t.Run("move from the drawn column to the drawn column", func(t *testing.T) {
		e := setup(t)
		before := e.visibleColumns()
		src, dst := before[0], before[1]
		e.press(mvX(0), mvY(0))
		e.settle()
		// the window is sticky: a press does not move it, the target is the column drawn in slot 1
		if now := e.visibleColumns(); strings.Join(now, " ") != strings.Join(before, " ") {
			t.Fatalf("press moved the column window: was %v, now %v", before, now)
		}
		e.release(mvX(1), mvY(0))
		e.screenHas("waiting for:") // dst - blocked, role waiting: asks for a reason
		e.tm.Type("why")
		e.key("enter")
		e.until(ids[src]+" in "+dst, func() bool { st, _ := e.status(ids[src]); return st == dst })
	})
}

// With an input line the mouse is ignored, input reaches the task where it was started.
func TestMouse_InputLineOpen_MouseIgnored(t *testing.T) {
	e := mvNewEnv(t, mvCfgWork)
	e.create("Alpha", "work")
	e.create("Bravo", "work")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.key(" ", "l", " ") // Alpha -> wait: reason line
	e.screenHas("waiting for:")
	before := e.commits()
	e.click(3, mvY(1)) // click on Bravo
	e.press(3, mvY(1)) // and a drag of Bravo to fin
	e.release(mvX(2), mvY(0))
	e.settle()
	if !strings.Contains(e.text(), "waiting for:") {
		t.Fatalf("mouse closed the input line:\n%s", e.text())
	}
	if e.commits() != before {
		t.Error("mouse with an input line wrote a commit")
	}
	e.tm.Type("reason")
	e.key("enter")
	e.until("T-1 in wait with a reason", func() bool { st, w := e.status("T-1"); return st == "wait" && w == "reason" })
	if st, _ := e.status("T-2"); st != "work" {
		t.Errorf("T-2 moved by the mouse with an input line: %s", st)
	}
}

// ---------------------------------------------------------------------------------------------
// Pulse, order
// ---------------------------------------------------------------------------------------------

// ask outranks a note; a note only in the active column; without notes - summary.
func TestPulse_AskBeatsNote_NoteOnlyInActive_ElseSummary(t *testing.T) {
	e := mvNewEnv(t, "")
	asked := e.create("Asked", "doing")
	noted := e.create("Noted", "doing")
	plain := e.create("Plain", "doing")
	parked := e.create("Parked", "todo")
	for id, s := range map[string]string{asked: "sum-asked", noted: "sum-noted", plain: "sum-plain", parked: "sum-parked"} {
		e.summary(id, s)
	}
	e.set(asked, "ask", "pick A or B")
	e.note(asked, "note-asked")
	e.note(noted, "note-noted")
	e.note(parked, "note-parked")
	e.start(140, 40)

	e.screenHas("note-noted", "? pick A or B", "sum-plain", "sum-parked")
	s := e.text()
	for _, bad := range []string{"note-asked", "sum-asked", "sum-noted", "note-parked"} {
		if strings.Contains(s, bad) {
			t.Errorf("extra %q on the board:\n%s", bad, s)
		}
	}
}

// Note age 5m, 3h, 2d by the model clock; the newest note is visible for a task.
func TestPulse_NoteAge_UsesModelClock_LatestNoteWins(t *testing.T) {
	cases := []struct {
		offset time.Duration
		want   string
	}{
		{5*time.Minute + 20*time.Second, "5m"},
		{3*time.Hour + 5*time.Minute, "3h"},
		{50*time.Hour + 5*time.Minute, "2d"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			e := mvNewEnv(t, "")
			id := e.create("Noted", "doing")
			e.note(id, "old-note")
			e.note(id, "new-note")
			sec, err := strconv.ParseInt(e.git("log", "-1", "--format=%ct"), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			e.setClock(time.Unix(sec, 0).Add(c.offset))
			e.start(140, 30)
			e.screenHas(c.want + " new-note")
			if strings.Contains(e.text(), "old-note") {
				t.Errorf("not the last note is shown:\n%s", e.text())
			}
		})
	}
}

// tix note - an empty commit without a file edit; the watcher notices it by .git/logs/HEAD.
func TestPulse_NoteWrittenWhileOpen_AppearsWithoutKeypress(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("Noted", "doing")
	e.summary(id, "sum-noted")
	e.start(140, 30)
	e.screenHas("sum-noted")
	// the watcher starts after the first frame: repeat the note until it turns on
	deadline := time.Now().Add(5 * time.Second)
	for i := 0; time.Now().Before(deadline) && !strings.Contains(e.text(), "fresh-news"); i++ {
		e.note(id, "fresh-news")
		for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end) && !strings.Contains(e.text(), "fresh-news"); {
			time.Sleep(10 * time.Millisecond)
		}
	}
	e.screenHas("fresh-news")
}

// Risk 3: the number of git calls per pulse reread does not grow with the number of tasks.
func TestPulse_GitCalls_DoNotGrowWithTaskCount(t *testing.T) {
	realGit := mvGit(t)
	calls := func(tasks int) int {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "calls.log")
		wrapper := filepath.Join(dir, "git")
		script := "#!/bin/sh\necho \"$@\" >> \"" + logPath + "\"\nexec " + realGit + " \"$@\"\n"
		if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil { //nolint:gosec // test script
			t.Fatal(err)
		}
		e := mvNewEnvGit(t, "", wrapper)
		for i := 1; i <= tasks; i++ {
			id := e.create(fmt.Sprintf("Task%02d", i), "doing")
			e.note(id, fmt.Sprintf("note-of-%02d", i))
		}
		if err := os.WriteFile(logPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		e.start(140, 40)
		e.screenHas("note-of-01")
		e.settle()
		data, err := os.ReadFile(logPath) //nolint:gosec // test path
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, ln := range strings.Split(string(data), "\n") {
			for _, f := range strings.Fields(ln) {
				if f == "log" {
					n++
					break
				}
			}
		}
		return n
	}
	small, large := calls(3), calls(40)
	if large > small {
		t.Errorf("git log calls: %d on 3 tasks, %d on 40 - grow with the number of tasks", small, large)
	}
}

// o toggles due order (then no-due by rank); J in this mode does not write a commit.
func TestOrder_O_ByDue_JDoesNotWrite(t *testing.T) {
	e := mvNewEnv(t, "")
	a := e.create("task-aaa", "idea")
	e.create("task-bbb", "idea")
	c := e.create("task-ccc", "idea")
	e.set(a, "due", "2026-12-01")
	e.set(c, "due", "2026-11-01")
	e.start(140, 30)
	e.screenHas("task-aaa", "task-bbb", "task-ccc")
	order := func() []string {
		type p struct {
			y    int
			name string
		}
		var ps []p
		for _, n := range []string{"task-aaa", "task-bbb", "task-ccc"} {
			ps = append(ps, p{e.yOf(n), n})
		}
		for i := range ps {
			for j := i + 1; j < len(ps); j++ {
				if ps[j].y < ps[i].y {
					ps[i], ps[j] = ps[j], ps[i]
				}
			}
		}
		var out []string
		for _, x := range ps {
			out = append(out, x.name)
		}
		return out
	}
	if got := strings.Join(order(), " "); got != "task-aaa task-bbb task-ccc" {
		t.Fatalf("order by rank: %s", got)
	}
	e.key("o")
	e.screenHas("order: due")
	e.settle()
	if got := strings.Join(order(), " "); got != "task-ccc task-aaa task-bbb" {
		t.Errorf("due order: %s, expected ccc (11-01), aaa (12-01), bbb (no due)", got)
	}
	before := e.commits()
	e.key("J")
	e.screenHas("order by due: J K off")
	e.settle()
	if e.commits() != before {
		t.Errorf("J in due mode wrote a commit: %s -> %s", before, e.commits())
	}
	if got := strings.Join(order(), " "); got != "task-ccc task-aaa task-bbb" {
		t.Errorf("J changed the order in due mode: %s", got)
	}
	e.key("o")
	e.until("rank mode", func() bool {
		return !strings.Contains(e.text(), "order: due") && strings.Join(order(), " ") == "task-aaa task-bbb task-ccc"
	})
}
