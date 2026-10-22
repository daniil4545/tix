package tui

// Tests of the human main path on the board.
// The package is internal for the mv* harness from mvp_test.go; own helpers have prefix ac. Written from the spec,
// without reading the code of zones F, G, H. The environment (HOME, git) comes from TestMain in harness_test.go.

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
)

const (
	acHint     = "arrows select  space grab  enter open  n new  a answer  e edit  c comment  o order  p project  ? help  q quit"
	acFieldRow = "t title  d due  g tags  a ask  w waiting  s summary  x external"
	acFocusURL = "warp://session/0123456789abcdef0123456789abcdef"
	acMoving   = "moving T-1: arrows move, space drop, esc cancel"
)

// ---------------------------------------------------------------------------------------------
// Harness helpers
// ---------------------------------------------------------------------------------------------

// acPress sends a key: arrow and backspace names by code, others as e.key.
func (e *mvEnv) acPress(keys ...string) {
	for _, k := range keys {
		codes := map[string]rune{"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown, "backspace": tea.KeyBackspace}
		if c, ok := codes[k]; ok {
			e.tm.Send(tea.KeyPressMsg{Code: c})
			continue
		}
		e.key(k)
	}
}

func (e *mvEnv) acCommitCount() int {
	e.t.Helper()
	n, err := strconv.Atoi(e.commits())
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// acCommitsSettled waits (until a deadline) for the commit count to reach want, then lets the queue add extras
// and returns the final count: the file write goes before the commit.
func (e *mvEnv) acCommitsSettled(want int) int {
	e.t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end) && e.acCommitCount() < want; time.Sleep(10 * time.Millisecond) {
	}
	e.settle()
	return e.acCommitCount()
}

func (e *mvEnv) acTask(id string) *task.Task {
	e.t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		e.t.Fatalf("Get %s: %v", id, err)
	}
	return tk
}

func (e *mvEnv) acByTitle(title string) *task.Task {
	tasks, _ := e.store.List()
	for _, tk := range tasks {
		if tk.Title == title {
			return tk
		}
	}
	return nil
}

// acOrder - the task ids of a column by rank, as the core stores them.
func (e *mvEnv) acOrder(column string) string {
	tasks, _ := e.store.List()
	var in []*task.Task
	for _, tk := range tasks {
		if tk.Status == column {
			in = append(in, tk)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Rank < in[j].Rank })
	ids := make([]string, len(in))
	for i, tk := range in {
		ids[i] = tk.ID
	}
	return strings.Join(ids, " ")
}

// acFind - the cell of the first character of sub in the lowest screen line that has it.
func (e *mvEnv) acFind(sub string) (x, y int) {
	e.t.Helper()
	ls := e.lines()
	for y = len(ls) - 1; y >= 0; y-- {
		if i := strings.Index(ls[y], sub); i >= 0 {
			return utf8.RuneCountInString(ls[y][:i]), y
		}
	}
	e.t.Fatalf("%q is not on the screen:\n%s", sub, e.text())
	return 0, 0
}

// acClickOn waits for sub on the screen and clicks in its middle.
func (e *mvEnv) acClickOn(sub string) {
	e.t.Helper()
	e.screenHas(sub)
	x, y := e.acFind(sub)
	e.click(x+utf8.RuneCountInString(sub)/2, y)
}

// acColX - the screen column where title starts in the first line with it.
func (e *mvEnv) acColX(title string) int {
	for _, ln := range e.lines() {
		if i := strings.Index(ln, title); i >= 0 {
			return utf8.RuneCountInString(ln[:i])
		}
	}
	return -1
}

// acWaitStatus waits until the task gets the status.
func (e *mvEnv) acWaitStatus(id, status string) {
	e.t.Helper()
	e.until(id+" in "+status, func() bool { st, _ := e.status(id); return st == status })
}

// acStillOn: after everything sent is handled, the screen has sub (and nothing extra).
func (e *mvEnv) acStillOn(sub string) {
	e.t.Helper()
	e.settle()
	if !strings.Contains(e.text(), sub) {
		e.t.Fatalf("%q is not on the screen:\n%s", sub, e.text())
	}
}

// acStartIn starts the TUI with the initial project (filter) project.
func (e *mvEnv) acStartIn(project string, w, h int) {
	e.t.Helper()
	ctx := e.t.Context()
	e.model = New(ctx, Deps{Store: e.store, Root: e.root, Actor: "human", Project: project})
	e.model.now = func() time.Time { return time.Unix(0, e.clock.Load()) }
	e.tm = teatest.NewTestModel(e.t, mvProbe{Model: e.model, e: e}, teatest.WithInitialTermSize(w, h))
	e.t.Cleanup(func() {
		_ = e.tm.Quit()
		e.tm.WaitFinished(e.t, teatest.WithFinalTimeout(5*time.Second))
	})
}

// acOpenCard opens the card of the selected task.
func (e *mvEnv) acOpenCard(title string) {
	e.t.Helper()
	e.key("enter")
	e.opened(title)
	e.settle() // the card frame already has the window size: clicks are computed by it
}

// acEditor - an $EDITOR stub: counts calls, copies the file at start, runs script over the file.
type acEditor struct {
	t   *testing.T
	dir string
}

const acEditorScript = `#!/bin/sh
d="$TIX_AC_DIR"
n=$(( $(cat "$d/count" 2>/dev/null || echo 0) + 1 )); echo $n > "$d/count"
cp "$1" "$d/seen.$n"
if [ -f "$d/script" ]; then sh "$d/script" "$1"; fi
exit 0
`

func acStubEditor(t *testing.T, script string) *acEditor {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(bin, []byte(acEditorScript), 0o700); err != nil { //nolint:gosec // test stub
		t.Fatal(err)
	}
	if script != "" {
		if err := os.WriteFile(filepath.Join(dir, "script"), []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("EDITOR", bin)
	t.Setenv("VISUAL", "")
	t.Setenv("TIX_AC_DIR", dir)
	return &acEditor{t: t, dir: dir}
}

func (s *acEditor) calls() int {
	b, _ := os.ReadFile(filepath.Join(s.dir, "count")) //nolint:gosec // test path
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func (s *acEditor) seen(n int) string {
	b, _ := os.ReadFile(filepath.Join(s.dir, "seen."+strconv.Itoa(n))) //nolint:gosec // test path
	return string(b)
}

// acFakeOpen replaces the open command with a script in PATH; returns the call log path (one line per call).
func acFakeOpen(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "open.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TIX_AC_OPEN_LOG\"\n"
	if err := os.WriteFile(filepath.Join(dir, "open"), []byte(script), 0o700); err != nil { //nolint:gosec // test stub
		t.Fatal(err)
	}
	t.Setenv("TIX_AC_OPEN_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func acRead(path string) string {
	b, _ := os.ReadFile(path) //nolint:gosec // test path
	return string(b)
}

// ---------------------------------------------------------------------------------------------
// Scenario 1: hint buttons
// ---------------------------------------------------------------------------------------------

func acBoard(t *testing.T, w, h int) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.start(w, h)
	e.screenHas("Alpha")
	e.settle() // the first frame is drawn before the window size: clicks are computed by the frame after it
	return e
}

// The last board line is the new hint.
func TestHint_BoardLastLine_IsNewHint(t *testing.T) {
	e := acBoard(t, 140, 30)
	e.screenHas("q quit")
	ls := e.lines()
	var last string
	for _, ln := range ls {
		if strings.TrimSpace(ln) != "" {
			last = strings.TrimSpace(ln)
		}
	}
	if last != acHint {
		t.Errorf("last board line %q, expected %q", last, acHint)
	}
}

// A click on a pair equals a key press.
func TestHint_ClickButton_ActsAsKey(t *testing.T) {
	cases := []struct{ pair, want string }{
		{"n new", "title:"},
		{"c comment", "comment:"},
		{"a answer", "T-1: no question"},
		{"o order", "order: due"},
	}
	for _, c := range cases {
		t.Run(c.pair, func(t *testing.T) {
			e := acBoard(t, 140, 30)
			e.acClickOn(c.pair)
			e.screenHas(c.want)
		})
	}
}

// A click on q quit exits.
func TestHint_ClickQuit_ExitsProgram(t *testing.T) {
	e := acBoard(t, 140, 30)
	e.screenHas("q quit")
	x, y := e.acFind("q quit")
	e.mouse(tea.MouseClickMsg(mvM(x+2, y, tea.MouseLeft))) // exit on the press: the release will not come
	e.tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// arrows select is not a button; a click on the gap between pairs does nothing either.
func TestHint_ClickNonButton_DoesNothing(t *testing.T) {
	e := acBoard(t, 140, 30)
	e.screenHas("arrows select")
	before := e.acCommitCount()
	e.acClickOn("arrows select")
	gap := strings.Index(acHint, "enter open  n new") + len("enter open") + 1 // the second space between pairs
	_, y := e.acFind("p project")
	e.click(gap, y)
	e.settle()
	for _, bad := range []string{"title:", "comment:", "answer:", "no question", "order: due"} {
		if strings.Contains(e.text(), bad) {
			t.Errorf("a click off a button triggered %q:\n%s", bad, e.text())
		}
	}
	if !strings.Contains(e.text(), "enter open") || e.acCommitCount() != before {
		t.Errorf("a click off a button changed the state:\n%s", e.text())
	}
}

// A pair cut by the width is not clickable; a whole one next to it works.
func TestHint_TruncatedPair_NotClickable(t *testing.T) {
	e := acBoard(t, len(acHint)-2, 30) // "q quit" cut to "q qu"
	e.until("hint cut by the width", func() bool {
		return strings.Contains(e.text(), "p project") && !strings.Contains(e.text(), "q quit")
	})
	_, y := e.acFind("p project")
	e.click(strings.Index(acHint, "q quit"), y) // on the letter q
	e.click(strings.Index(acHint, "q quit")+2, y)
	e.settle() // hangs with an error if the program quit
	if !strings.Contains(e.text(), "enter open") {
		t.Errorf("a click on a cut pair changed the screen:\n%s", e.text())
	}
	e.acClickOn("n new")
	e.screenHas("title:")
}

// ---------------------------------------------------------------------------------------------
// Scenarios 2 and 3: grab
// ---------------------------------------------------------------------------------------------

// acCriterion adds one criterion to a task: column ready accepts only tasks with a criterion.
func (e *mvEnv) acCriterion(ids ...string) {
	e.t.Helper()
	for _, id := range ids {
		_, rev, err := e.store.Get(id)
		if err == nil {
			_, err = e.store.Edit(id, rev, "## Details\n- [ ] one\n", "seed")
		}
		if err != nil {
			e.t.Fatalf("criterion %s: %v", id, err)
		}
	}
}

func acGrabEnv(t *testing.T) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	e.create("Charlie", "idea")
	e.acCriterion("T-1", "T-2", "T-3")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo", "Charlie")
	return e
}

// space, right, space and enter - the card in the neighbor column, one commit, no write before the release.
func TestGrab_RightThenDrop_MovesToNeighborWithOneCommit(t *testing.T) {
	for _, drop := range []string{" ", "enter"} {
		t.Run("drop="+strconv.Quote(drop), func(t *testing.T) {
			e := acGrabEnv(t)
			before := e.acCommitCount()
			e.key(" ")
			e.screenHas(acMoving)
			e.until("marker '=' on the grabbed card", func() bool {
				y := e.yOf("T-1 p")
				return y >= 0 && strings.Contains(e.lines()[y], "=T-1")
			})
			e.acPress("right")
			e.until("Alpha on the screen in the neighbor column", func() bool { x := e.acColX("Alpha"); return x >= 26 && x <= 49 })
			if st, _ := e.status("T-1"); st != "idea" || e.acCommitCount() != before {
				t.Fatalf("written before the release: status %s, commits %d -> %d", st, before, e.acCommitCount())
			}
			e.key(drop)
			e.acWaitStatus("T-1", "todo")
			e.settle()
			if after := e.acCommitsSettled(before + 1); after != before+1 {
				t.Errorf("commits %d -> %d, expected exactly one", before, after)
			}
			if strings.Contains(e.text(), "moving T-1") {
				t.Errorf("grab not dropped after the release:\n%s", e.text())
			}
		})
	}
}

// The place in a column; arrows and h j k l are equal; the place is written as First or After of the neighbor above.
func TestGrab_UpDown_ChoosesPlaceInColumn(t *testing.T) {
	cases := []struct {
		name   string
		pick   int // how many j before space
		keys   []string
		wanted string
	}{
		{"down", 0, []string{"down"}, "T-2 T-1 T-3"},
		{"j j to the end", 0, []string{"j", "j"}, "T-2 T-3 T-1"},
		{"up up to the top", 2, []string{"up", "up"}, "T-3 T-1 T-2"},
		{"k once", 2, []string{"k"}, "T-1 T-3 T-2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := acGrabEnv(t)
			e.key(strings.Repeat("j", c.pick))
			e.settle()
			before := e.acCommitCount()
			e.key(" ")
			e.screenHas("moving T-")
			e.acPress(c.keys...)
			e.settle()
			if got := e.acOrder("idea"); got != "T-1 T-2 T-3" || e.acCommitCount() != before {
				t.Fatalf("written before the release: order %s, commits %d -> %d", got, before, e.acCommitCount())
			}
			e.key(" ")
			e.until("order "+c.wanted, func() bool { return e.acOrder("idea") == c.wanted })
			e.settle()
			if after := e.acCommitsSettled(before + 1); after != before+1 {
				t.Errorf("commits %d -> %d, expected exactly one", before, after)
			}
		})
	}
}

// The same column and the same place - nothing is written; the grab is dropped.
func TestGrab_DropAtOriginalPlace_NoCommit(t *testing.T) {
	cases := []struct {
		name string
		keys []string
	}{
		{"release at once", nil},
		{"right and back", []string{"right", "left"}},
		{"down and back", []string{"down", "up"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := acGrabEnv(t)
			before := e.acCommitCount()
			e.key(" ")
			e.screenHas("moving T-1")
			e.acPress(c.keys...)
			e.key(" ")
			e.until("grab dropped", func() bool { return !strings.Contains(e.text(), "moving T-1") })
			e.settle()
			if after := e.acCommitCount(); after != before {
				t.Errorf("commits %d -> %d, expected no write", before, after)
			}
			if got := e.acOrder("idea"); got != "T-1 T-2 T-3" {
				t.Errorf("order changed: %s", got)
			}
		})
	}
}

// esc - cancel, the card at the previous place, no commit.
func TestGrab_Esc_CancelsWithoutCommit(t *testing.T) {
	e := acGrabEnv(t)
	before := e.acCommitCount()
	e.key(" ")
	e.screenHas("moving T-1")
	e.acPress("right")
	e.until("Alpha went to the neighbor column", func() bool { return e.acColX("Alpha") >= 26 })
	e.key("esc")
	e.until("Alpha returned to its column", func() bool { x := e.acColX("Alpha"); return x >= 0 && x < 26 })
	e.settle()
	if strings.Contains(e.text(), "moving T-1") {
		t.Errorf("the grab line stayed after esc:\n%s", e.text())
	}
	if st, _ := e.status("T-1"); st != "idea" || e.acCommitCount() != before {
		t.Errorf("cancel wrote: status %s, commits %d -> %d", st, before, e.acCommitCount())
	}
}

// During a grab other keys and the mouse are ignored.
func TestGrab_OtherKeysAndMouse_Ignored(t *testing.T) {
	e := acGrabEnv(t)
	before := e.acCommitCount()
	e.key(" ")
	e.screenHas("moving T-1")
	e.key("n", "a", "c", "o", "p", "q")
	e.click(mvX(0)+3, mvY(1)) // click on Bravo
	e.click(mvX(1)+22, 2)     // and on "+" of column todo
	e.settle()
	for _, bad := range []string{"title:", "comment:", "answer:", "no question", "order: due"} {
		if strings.Contains(e.text(), bad) {
			t.Fatalf("%q fired during the grab:\n%s", bad, e.text())
		}
	}
	if !strings.Contains(e.text(), "moving T-1") || e.acCommitCount() != before {
		t.Errorf("grab broken by other events, commits %d -> %d:\n%s", before, e.acCommitCount(), e.text())
	}
	e.key("esc")
}

// A move to waiting and closed asks for a reason, like m; esc of the input line leaves the task.
func TestGrab_ToWaitingAndClosed_AsksReason(t *testing.T) {
	setup := func(t *testing.T) *mvEnv {
		e := mvNewEnv(t, mvCfgWork)
		e.create("Alpha", "work")
		e.create("Bravo", "work")
		e.start(140, 30)
		e.screenHas("Alpha", "Bravo")
		return e
	}
	t.Run("waiting: a reason moves", func(t *testing.T) {
		e := setup(t)
		before := e.acCommitCount()
		e.key(" ")
		e.acPress("right")
		e.key(" ")
		e.screenHas("waiting for:")
		if st, _ := e.status("T-1"); st != "work" || e.acCommitCount() != before {
			t.Fatalf("written before the answer: %s, commits %d -> %d", st, before, e.acCommitCount())
		}
		e.tm.Type("vendor reply")
		e.key("enter")
		e.until("T-1 in wait with a reason", func() bool { st, w := e.status("T-1"); return st == "wait" && w == "vendor reply" })
		e.settle()
		if after := e.acCommitsSettled(before + 1); after != before+1 {
			t.Errorf("commits %d -> %d, expected one", before, after)
		}
	})
	t.Run("waiting: esc leaves the task", func(t *testing.T) {
		e := setup(t)
		before := e.acCommitCount()
		e.key(" ")
		e.acPress("right")
		e.key(" ")
		e.screenHas("waiting for:")
		e.key("esc")
		e.until("line closed", func() bool { return !strings.Contains(e.text(), "waiting for:") })
		e.settle()
		if st, _ := e.status("T-1"); st != "work" || e.acCommitCount() != before {
			t.Errorf("esc wrote: %s, commits %d -> %d", st, before, e.acCommitCount())
		}
	})
	t.Run("closed: close reason", func(t *testing.T) {
		e := setup(t)
		e.key(" ")
		e.acPress("right", "right")
		e.key(" ")
		e.screenHas("close reason:")
		e.tm.Type("shipped")
		e.key("enter")
		e.acWaitStatus("T-1", "fin")
	})
}

// The task was deleted in another window - cancel and "T-1 removed", the grab is dropped (space does not fall into a write).
func TestGrab_TaskRemovedMeanwhile_ReportsRemovedAndReleases(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	canary := e.create("canary", "done")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.awaitWatcher(canary)
	e.key(" ")
	e.screenHas("moving T-1")
	if _, err := e.store.Remove("T-1", "agent"); err != nil {
		t.Fatal(err)
	}
	e.screenHas("T-1 removed")
	if strings.Contains(e.text(), "moving T-1") {
		t.Errorf("grab of the deleted task not dropped:\n%s", e.text())
	}
	after := e.acCommitCount()
	e.key(" ") // no grab: this is a new grab of the selected Bravo, not a release
	e.screenHas("moving T-2")
	e.key("esc")
	e.settle()
	if e.acCommitCount() != after {
		t.Error("something was written after the grabbed task was deleted")
	}
	if st, _ := e.status("T-2"); st != "idea" {
		t.Errorf("T-2 moved: %s", st)
	}
}

// Another store change does not drop the grab: it holds by id and finishes the move.
func TestGrab_OtherChangeMeanwhile_HoldsById(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	canary := e.create("canary", "done")
	e.acCriterion("T-1")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.awaitWatcher(canary)
	e.key(" ")
	e.screenHas("moving T-1")
	e.create("Delta", "idea") // the agent added a task that does not rank above ours in the column, but the store is reread
	e.screenHas("Delta")
	e.acStillOn("moving T-1")
	e.acPress("right")
	e.key(" ")
	e.acWaitStatus("T-1", "todo")
}

// Due order is on - the grab changes only the column, the status line says so.
func TestGrab_OrderByDue_ColumnOnly(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "todo")
	e.acCriterion("T-1")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.key("o")
	e.screenHas("order: due")
	e.key(" ")
	e.screenHas("order by due: place off")
	before := e.acCommitCount()
	e.acPress("right")
	e.key(" ")
	e.acWaitStatus("T-1", "todo")
	e.settle()
	if after := e.acCommitsSettled(before + 1); after != before+1 {
		t.Errorf("commits %d -> %d, expected one", before, after)
	}
}

// ---------------------------------------------------------------------------------------------
// Scenario 4: new task
// ---------------------------------------------------------------------------------------------

// New task: a click on "+" of column doing, title, editor; the cursor on the new task.
func TestNewTask_ClickPlus_CreatesInColumnAndOpensEditor(t *testing.T) {
	cases := []struct{ name, script, wantBody string }{
		{"editor without an edit", "", ""},
		{"editor with an edit", `printf '## Abstract\nedited-abstract\n' > "$1"`, "edited-abstract"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := acStubEditor(t, c.script)
			e := mvNewEnv(t, "")
			e.create("Alpha", "idea")
			e.create("Beta", "doing")
			e.start(140, 30)
			e.screenHas("Alpha", "Beta")
			plusX := 2*mvColStep + 23 // the last character of the third column cell
			if r := []rune(e.lines()[2]); len(r) <= plusX || r[plusX] != '+' {
				t.Fatalf("no '+' at x=%d in the column names line: %q", plusX, e.lines()[2])
			}
			e.click(plusX, 2)
			e.screenHas("title:")
			e.tm.Type("Brand new")
			e.key("enter")
			e.until("editor called", func() bool { return st.calls() == 1 && st.seen(1) != "" })
			e.until("task created", func() bool { return e.acByTitle("Brand new") != nil })
			tk := e.acByTitle("Brand new")
			if tk.Status != "doing" || tk.Project != "personal" {
				t.Errorf("task: column %q, project %q, expected doing and personal", tk.Status, tk.Project)
			}
			acCheckTemplate(t, st.seen(1))
			e.settle()
			e.acCheckBody(tk.ID, c.wantBody)
			e.selectedIs("Brand new")
		})
	}
}

func acCheckTemplate(t *testing.T, seen string) {
	t.Helper()
	for _, part := range []string{"## Abstract", "## Summary", "## Details", "- [ ] "} {
		if !strings.Contains(seen, part) {
			t.Errorf("body template in the editor without %q:\n%s", part, seen)
		}
	}
}

// acCheckBody: an empty want - the body is empty (exit without an edit), otherwise the body gets want.
func (e *mvEnv) acCheckBody(id, want string) {
	e.t.Helper()
	if want == "" {
		if body := e.acTask(id).Body; strings.TrimSpace(body) != "" {
			e.t.Errorf("exit without an edit left the body %q, expected empty", body)
		}
		return
	}
	e.until("editor edit in the body", func() bool { return strings.Contains(e.acTask(id).Body, want) })
}

// New task: n - in the selected column; project - the launch filter, otherwise personal.
func TestNewTask_N_UsesSelectedColumnAndProject(t *testing.T) {
	cases := []struct{ name, launch, seed, want string }{
		{"no filter", "", "p", "personal"},
		{"launch project", "launch", "launch", "launch"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			acStubEditor(t, "")
			e := mvNewEnv(t, "")
			if _, err := e.store.Create("Alpha", c.seed, "idea", "seed"); err != nil {
				t.Fatal(err)
			}
			e.createIn("Beta", c.seed, "doing")
			e.acStartIn(c.launch, 140, 30)
			e.screenHas("Alpha", "Beta")
			e.key("l", "l", "n") // column doing
			e.screenHas("title:")
			e.tm.Type("From key")
			e.key("enter")
			e.until("task created", func() bool { return e.acByTitle("From key") != nil })
			tk := e.acByTitle("From key")
			if tk.Status != "doing" || tk.Project != c.want {
				t.Errorf("task: column %q, project %q, expected doing and %q", tk.Status, tk.Project, c.want)
			}
		})
	}
}

// New task in a column with an entry check: the core refuses, the status line says why, nothing is written.
func TestNewTask_N_InReadyColumn_RefusedByEntryCheck(t *testing.T) {
	acStubEditor(t, "")
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.start(140, 30)
	e.screenHas("Alpha")
	before := e.acCommitCount()
	e.key("l", "n") // column todo, ready role
	e.screenHas("title:")
	e.tm.Type("Refused")
	e.key("enter")
	e.screenHas("a new task has no criteria")
	if e.acByTitle("Refused") != nil || e.acCommitCount() != before {
		t.Errorf("the refused task left a trace: task %v, commits %d -> %d", e.acByTitle("Refused"), before, e.acCommitCount())
	}
}

// New task: an empty title or esc - cancel: no task, no commit, no editor.
func TestNewTask_EmptyTitleOrEsc_Cancels(t *testing.T) {
	for _, cancel := range []string{"enter", "esc"} {
		t.Run(cancel, func(t *testing.T) {
			st := acStubEditor(t, "")
			e := acBoard(t, 140, 30)
			before := e.acCommitCount()
			e.key("n")
			e.screenHas("title:")
			e.key(cancel)
			e.until("line closed", func() bool { return !strings.Contains(e.text(), "title:") })
			e.settle()
			if tasks, _ := e.store.List(); len(tasks) != 1 || e.acCommitCount() != before || st.calls() != 0 {
				t.Errorf("cancel left a trace: tasks %d, commits %d -> %d, editor calls %d",
					len(tasks), before, e.acCommitCount(), st.calls())
			}
		})
	}
}

// ---------------------------------------------------------------------------------------------
// Scenario 5: answer to an agent question
// ---------------------------------------------------------------------------------------------

func acAsked(t *testing.T, ask string) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	if ask != "" {
		e.set("T-1", "ask", ask)
	}
	e.start(140, 30)
	e.screenHas("Alpha")
	e.settle()
	return e
}

// Without a question - "no question", nothing is written (board and card).
func TestAnswer_NoQuestion_SaysNoQuestion(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		t.Run(where, func(t *testing.T) {
			e := acAsked(t, "")
			if where == "card" {
				e.acOpenCard("Alpha")
			}
			before := e.acCommitCount()
			e.key("a")
			e.screenHas("T-1: no question")
			e.settle()
			if strings.Contains(e.text(), "answer:") || e.acCommitCount() != before {
				t.Errorf("an answer without a question opened a line or wrote, commits %d -> %d", before, e.acCommitCount())
			}
		})
	}
}

// With a question - a comment "Answer: ..." from the human, ask is cleared (board and card).
func TestAnswer_WithQuestion_CommentsAndClearsAsk(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		t.Run(where, func(t *testing.T) {
			e := acAsked(t, "pick A or B")
			if where == "card" {
				e.acOpenCard("Alpha")
			}
			e.key("a")
			e.screenHas("answer:")
			e.tm.Type("use B")
			e.key("enter")
			e.until("comment and empty ask", func() bool {
				tk := e.acTask("T-1")
				return tk.Ask == "" && len(tk.Comments) == 1
			})
			c := e.acTask("T-1").Comments[0]
			if c.Text != "Answer: use B" || c.Actor != "human" {
				t.Errorf("comment %q from %q, expected %q from human", c.Text, c.Actor, "Answer: use B")
			}
		})
	}
}

// The agent managed to ask a new question - the answer is written, the new question stays, line "new question".
func TestAnswer_AgentAskedAgain_KeepsNewQuestion(t *testing.T) {
	e := acAsked(t, "pick A or B")
	e.key("a")
	e.screenHas("answer:")
	e.tm.Type("use B")
	e.set("T-1", "ask", "second question")
	e.key("enter")
	e.screenHas("T-1: new question")
	e.until("comment written", func() bool { return len(e.acTask("T-1").Comments) == 1 })
	if tk := e.acTask("T-1"); tk.Ask != "second question" || tk.Comments[0].Text != "Answer: use B" {
		t.Errorf("ask %q, comment %q: expected the new question and the written answer", tk.Ask, tk.Comments[0].Text)
	}
}

// Empty or esc - cancel without a write.
func TestAnswer_EmptyOrEsc_Cancels(t *testing.T) {
	for _, cancel := range []string{"enter", "esc"} {
		t.Run(cancel, func(t *testing.T) {
			e := acAsked(t, "pick A or B")
			before := e.acCommitCount()
			e.key("a")
			e.screenHas("answer:")
			e.key(cancel)
			e.until("line closed", func() bool { return !strings.Contains(e.text(), "answer:") })
			e.settle()
			if tk := e.acTask("T-1"); tk.Ask != "pick A or B" || len(tk.Comments) != 0 || e.acCommitCount() != before {
				t.Errorf("cancel wrote: ask %q, comments %d, commits %d -> %d",
					tk.Ask, len(tk.Comments), before, e.acCommitCount())
			}
		})
	}
}

// A click on the line "* needs you:" in the card opens "answer:".
func TestAnswer_ClickNeedsYouLine_OpensAnswerLine(t *testing.T) {
	e := acAsked(t, "pick A or B")
	e.acOpenCard("Alpha")
	e.screenHas("needs you")
	_, y := e.acFind("needs you")
	e.click(4, y)
	e.screenHas("answer:")
}

// ---------------------------------------------------------------------------------------------
// Scenario 6: fields
// ---------------------------------------------------------------------------------------------

// acFieldCard: the Alpha card (project projx) is open; due - the initial due.
func acFieldCard(t *testing.T, due string) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	if _, err := e.store.Create("Alpha", "projx", "idea", "seed"); err != nil {
		t.Fatal(err)
	}
	if due != "" {
		e.set("T-1", "due", due)
	}
	e.start(140, 30)
	e.screenHas("Alpha")
	e.acOpenCard("Alpha")
	return e
}

type acDueCase struct {
	name, initial, typed, wantDue, wantStatus string
	erase, wantCommit                         bool
}

// enterDue goes through f, d, the value edit and enter.
func (e *mvEnv) enterDue(c acDueCase) {
	e.t.Helper()
	e.key("f")
	e.screenHas(acFieldRow)
	e.key("d")
	e.screenHas("due:")
	if c.erase {
		e.acPress(strings.Split(strings.Repeat("backspace,", 30), ",")[:30]...)
	}
	if c.typed != "" {
		e.tm.Type(c.typed)
	}
	e.key("enter")
}

// Due is written; an empty value clears; a core refusal is visible; without a change there is no write.
func TestField_Due_SetClearRefusedUnchanged(t *testing.T) {
	cases := []acDueCase{
		{"write", "", "2026-11-01", "2026-11-01", "", false, true},
		{"clear with empty", "2026-10-01", "", "", "", true, true},
		{"core refusal", "", "not-a-date", "", "due must be", false, false},
		{"no change", "2026-10-01", "", "2026-10-01", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := acFieldCard(t, c.initial)
			before := e.acCommitCount()
			e.enterDue(c)
			if c.wantStatus != "" {
				e.screenHas(c.wantStatus)
			}
			want := before
			if c.wantCommit {
				want++
			}
			if after := e.acCommitsSettled(want); after != want {
				t.Errorf("commits %d -> %d, expected %d", before, after, want)
			}
			if tk := e.acTask("T-1"); tk.Due != c.wantDue {
				t.Errorf("due %q, expected %q", tk.Due, c.wantDue)
			}
		})
	}
}

// The field line opens with the current value (an edit appends to it).
func TestField_Title_PrefilledWithCurrentValue(t *testing.T) {
	e := acFieldCard(t, "")
	e.key("f")
	e.screenHas(acFieldRow)
	e.key("t")
	e.screenHas("title:")
	e.tm.Type("X")
	e.key("enter")
	e.until("title AlphaX", func() bool { return e.acTask("T-1").Title == "AlphaX" })
}

// A multi-line field is not edited through the line; a one-line one is.
func TestField_Summary_MultiLineRefusedSingleLineEdits(t *testing.T) {
	t.Run("several lines", func(t *testing.T) {
		e := mvNewEnv(t, "")
		e.create("Alpha", "idea")
		e.summary("T-1", "first line\nsecond line")
		e.start(140, 30)
		e.screenHas("Alpha")
		e.acOpenCard("Alpha")
		before := e.acCommitCount()
		e.key("f", "s")
		e.screenHas("T-1: summary has several lines, use e")
		e.settle()
		if strings.Contains(e.text(), "summary:") || e.acCommitCount() != before {
			t.Errorf("the line opened or wrote, commits %d -> %d", before, e.acCommitCount())
		}
	})
	t.Run("one line", func(t *testing.T) {
		e := mvNewEnv(t, "")
		e.create("Alpha", "idea")
		e.summary("T-1", "only line")
		e.start(140, 30)
		e.screenHas("Alpha")
		e.acOpenCard("Alpha")
		e.key("f", "s")
		e.screenHas("summary:")
	})
}

// Field choice - by a click on a pair; a click on the card fields line opens the same choice.
func TestField_SelectByClick_AndFieldsRowOpensSelect(t *testing.T) {
	e := acFieldCard(t, "")
	e.screenHas("projx")
	e.settle()
	_, y := e.acFind("projx") // fields line: project first
	e.click(2, y)
	e.screenHas(acFieldRow)
	e.acClickOn("d due")
	e.screenHas("due:")
	e.tm.Type("2026-12-24")
	e.key("enter")
	e.until("due written", func() bool { return e.acTask("T-1").Due == "2026-12-24" })
}

// ---------------------------------------------------------------------------------------------
// Scenario 7: the card actions line
// ---------------------------------------------------------------------------------------------

// lastNonEmpty - the index of the last non-empty screen line.
func (e *mvEnv) acLastLine() int {
	ls := e.lines()
	for y := len(ls) - 1; y >= 0; y-- {
		if strings.TrimSpace(ls[y]) != "" {
			return y
		}
	}
	return -1
}

// The actions line is the last card line and is visible with a non-empty status line;
// the status line is above it. Checked on a roomy and on a low screen with a long history.
func TestCardActions_RowVisibleWithStatusAbove(t *testing.T) {
	const row = "e edit  c comment  a answer  f field  g session  del delete  j k history  esc back"
	for _, tc := range []struct {
		name string
		w, h int
		n    int // comments in the history
	}{
		{"roomy screen", 140, 30, 0},
		{"low screen, long history", 100, 14, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := mvNewEnv(t, "")
			e.create("Alpha", "idea")
			for i := 0; i < tc.n; i++ {
				if _, err := e.store.Comment("T-1", "remark-"+strconv.Itoa(i), "agent"); err != nil {
					t.Fatal(err)
				}
			}
			e.start(tc.w, tc.h)
			e.screenHas("Alpha")
			e.acOpenCard("Alpha")
			e.key("a")
			e.screenHas("T-1: no question")
			e.settle()
			last := e.acLastLine()
			if last < 1 || strings.TrimSpace(e.lines()[last]) != row {
				t.Fatalf("last card line %q, expected %q:\n%s", e.lines()[max(last, 0)], row, e.text())
			}
			if !strings.Contains(e.lines()[last-1], "T-1: no question") {
				t.Errorf("above the actions line %q, expected the status line", e.lines()[last-1])
			}
		})
	}
}

// A click on an actions-line pair equals the key.
func TestCardActions_ClickButton_ActsAsKey(t *testing.T) {
	cases := []struct{ pair, want string }{
		{"c comment", "comment:"},
		{"a answer", "T-1: no question"},
		{"f field", acFieldRow},
		{"g session", "T-1: no session to open"},
		{"esc back", "arrows select"},
	}
	for _, c := range cases {
		t.Run(c.pair, func(t *testing.T) {
			e := acAsked(t, "")
			e.acOpenCard("Alpha")
			e.acClickOn(c.pair)
			e.screenHas(c.want)
		})
	}
	t.Run("e edit", func(t *testing.T) {
		st := acStubEditor(t, "")
		e := acAsked(t, "")
		e.acOpenCard("Alpha")
		e.acClickOn("e edit")
		e.until("editor called", func() bool { return st.calls() == 1 })
	})
}

// A click on an empty place of the card - nothing.
func TestCardActions_ClickEmptySpace_DoesNothing(t *testing.T) {
	e := acAsked(t, "")
	e.acOpenCard("Alpha")
	e.screenHas("seed") // history loads after the card opens: the baseline waits for it
	e.settle()
	before, commits := e.text(), e.acCommitCount()
	e.click(120, 8)
	e.click(60, 3)
	e.settle()
	if got := e.text(); got != before || e.acCommitCount() != commits {
		t.Errorf("a click on an empty place changed the card:\n%s\nwas:\n%s", got, before)
	}
}

// ---------------------------------------------------------------------------------------------
// Scenario 8a: criteria in the card
// ---------------------------------------------------------------------------------------------

// acCriteriaCard: the Alpha card with two criteria is open; the cursor is led to the first criterion
// (criteria first, then history: k up the history reaches them and stops there).
func acCriteriaCard(t *testing.T) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	_, rev, err := e.store.Get("T-1")
	if err == nil {
		_, err = e.store.Edit("T-1", rev, "## Details\n- [ ] one\n- [ ] two\n", "seed")
	}
	if err != nil {
		t.Fatal(err)
	}
	e.start(140, 30)
	e.screenHas("Alpha")
	e.acOpenCard("Alpha")
	e.screenHas("one", "two")
	e.key(strings.Repeat("k", 20))
	e.settle()
	return e
}

func (e *mvEnv) acDone() string {
	var out []string
	for _, c := range e.acTask("T-1").Criteria() {
		out = append(out, strconv.FormatBool(c.Done))
	}
	return strings.Join(out, " ")
}

// j to the item, enter - checked, enter again - unchecked (the file has "- [ ]" again).
func TestCriteria_JThenEnter_TogglesAndUntoggles(t *testing.T) {
	e := acCriteriaCard(t)
	e.key("j") // second criterion
	e.settle()
	e.key("enter")
	e.until("second criterion checked", func() bool { return e.acDone() == "false true" })
	if _, _, raw, err := e.store.Show("T-1"); err != nil || !strings.Contains(string(raw), "- [x] two") || !strings.Contains(string(raw), "- [ ] one") {
		t.Errorf("file after the check (err=%v):\n%s", err, raw)
	}
	e.key("enter")
	e.until("check removed", func() bool { return e.acDone() == "false false" })
	if _, _, raw, _ := e.store.Show("T-1"); !strings.Contains(string(raw), "- [ ] two") || strings.Contains(string(raw), "[x]") {
		t.Errorf("file after the uncheck:\n%s", raw)
	}
}

// A click on a criterion line checks it; a repeated click unchecks.
func TestCriteria_Click_TogglesCriterion(t *testing.T) {
	e := acCriteriaCard(t)
	x, y := e.acFind("[ ] one")
	e.click(x+1, y)
	e.until("first criterion checked", func() bool { return e.acDone() == "true false" })
	e.screenHas("[x] one")
	x, y = e.acFind("[x] one")
	e.click(x+1, y)
	e.until("check removed", func() bool { return e.acDone() == "false false" })
}

// ---------------------------------------------------------------------------------------------
// Scenarios 8 (delete, help, m M removed) and 9 (the jump into an agent session, the TUI side)
// ---------------------------------------------------------------------------------------------

// m and M are removed - no reason line, no write, on the board and in the card.
func TestKeys_MAndShiftM_DoNothing(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		t.Run(where, func(t *testing.T) {
			e := mvNewEnv(t, mvCfgWork)
			e.create("Alpha", "work")
			e.start(140, 30)
			e.screenHas("Alpha")
			if where == "card" {
				e.acOpenCard("Alpha")
			}
			before := e.acCommitCount()
			e.key("m", "M")
			e.settle()
			if strings.Contains(e.text(), "waiting for:") || strings.Contains(e.text(), "close reason:") {
				t.Errorf("m or M opened the move line:\n%s", e.text())
			}
			if st, _ := e.status("T-1"); st != "work" || e.acCommitCount() != before {
				t.Errorf("m M wrote: status %s, commits %d -> %d", st, before, e.acCommitCount())
			}
		})
	}
}

// acPressDelete sends the Delete key: delete or backspace.
func (e *mvEnv) acPressDelete(name string) {
	code := tea.KeyDelete
	if name == "backspace" {
		code = tea.KeyBackspace
	}
	e.tm.Send(tea.KeyPressMsg{Code: code})
}

func acDeleteEnv(t *testing.T, where string) *mvEnv {
	t.Helper()
	e := acAsked(t, "")
	if where == "card" {
		e.acOpenCard("Alpha")
	}
	return e
}

// Delete (delete and backspace) on the board and in the card asks "delete T-1? y/n";
// y deletes with one commit.
func TestDelete_ConfirmYesRemoves(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		for _, del := range []string{"delete", "backspace"} {
			t.Run(where+"/"+del, func(t *testing.T) {
				e := acDeleteEnv(t, where)
				before := e.acCommitCount()
				e.acPressDelete(del)
				e.screenHas("delete T-1? y/n")
				if _, _, err := e.store.Get("T-1"); err != nil || e.acCommitCount() != before {
					t.Fatalf("deleted before confirmation: err=%v, commits %d -> %d", err, before, e.acCommitCount())
				}
				e.key("y")
				e.until("task gone", func() bool { _, _, err := e.store.Get("T-1"); return err != nil })
				if after := e.acCommitsSettled(before + 1); after != before+1 {
					t.Errorf("commits %d -> %d, expected one", before, after)
				}
			})
		}
	}
}

// Any key except y cancels the deletion.
func TestDelete_OtherKeyCancels(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		for _, other := range []string{"n", "x", "esc", "enter"} {
			t.Run(where+"/"+other, func(t *testing.T) {
				e := acDeleteEnv(t, where)
				before := e.acCommitCount()
				e.acPressDelete("delete")
				e.screenHas("delete T-1? y/n")
				e.key(other)
				e.until("question closed", func() bool { return !strings.Contains(e.text(), "delete T-1?") })
				e.settle()
				if _, _, err := e.store.Get("T-1"); err != nil || e.acCommitCount() != before {
					t.Errorf("cancel by key %q deleted: err=%v, commits %d -> %d", other, err, before, e.acCommitCount())
				}
			})
		}
	}
}

// A click on the "del delete" button of the card actions line equals the key.
func TestDelete_CardButton_AsksConfirmation(t *testing.T) {
	e := acAsked(t, "")
	e.acOpenCard("Alpha")
	e.acClickOn("del delete")
	e.screenHas("delete T-1? y/n")
	e.key("y")
	e.until("task gone", func() bool { _, _, err := e.store.Get("T-1"); return err != nil })
}

// ? opens help with all keys; esc or ? return to the board; the "? help" button equals the key.
func TestHelp_QuestionMark_OpensAndCloses(t *testing.T) {
	for _, closer := range []string{"esc", "?"} {
		t.Run("close="+closer, func(t *testing.T) {
			e := acBoard(t, 140, 30)
			board := e.text()
			e.key("?")
			e.until("help instead of the board", func() bool {
				s := e.text()
				return s != board && strings.Contains(s, "grab") && strings.Contains(s, "delete")
			})
			e.key(closer)
			e.screenHas("arrows select", "Alpha")
		})
	}
	t.Run("click", func(t *testing.T) {
		e := acBoard(t, 140, 30)
		board := e.text()
		e.acClickOn("? help")
		e.until("help instead of the board", func() bool { return e.text() != board && !strings.Contains(e.text(), "arrows select") })
	})
}

type acFocusCase struct {
	name     string
	claim    bool
	fileName string // "" - no file
	content  string
	wantOpen bool
	ways     []string
}

// acFocusEnv prepares task Alpha with a claim (if claim) and a focus file in directory dir.
func acFocusEnv(t *testing.T, c acFocusCase, dir string) *mvEnv {
	t.Helper()
	steps := 0 // how many columns right from idea to the task
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.acCriterion("T-1")
	if c.claim {
		// Take takes a task from ready and moves it to active: the cursor column is found by status
		if _, err := e.store.Move("T-1", "todo", core.MoveOpts{}, "seed"); err != nil {
			t.Fatalf("Move: %v", err)
		}
		if _, err := e.store.Take("T-1", core.Take, "claude:c1"); err != nil {
			t.Fatalf("Take: %v", err)
		}
		steps = slices.Index(mvDefaultCols, e.acTask("T-1").Status)
	}
	if c.fileName != "" {
		if err := os.WriteFile(filepath.Join(dir, c.fileName), []byte(c.content+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e.start(140, 30)
	e.screenHas("Alpha")
	e.key(strings.Repeat("l", steps))
	e.settle()
	return e
}

// g - only with a live claim and the owner focus file that passed the form check; otherwise
// "no session to open". The open call is a substituted command in PATH.
func TestFocus_G_OpensOnlyForLiveClaimWithValidFile(t *testing.T) {
	all := []string{"board", "card", "button"}
	cases := []acFocusCase{
		{"claim and owner file", true, "claude-c1", acFocusURL, true, all},
		{"no claim", false, "claude-c1", acFocusURL, false, all},
		{"claim exists, no file", true, "", "", false, all},
		{"another agent file", true, "codex-x9", acFocusURL, false, []string{"board"}},
		{"value of the wrong form", true, "claude-c1", "warp://session/XYZ", false, []string{"board"}},
		{"other scheme", true, "claude-c1", "https://example.com/s", false, []string{"board"}},
	}
	for _, c := range cases {
		for _, way := range c.ways {
			t.Run(c.name+"/"+way, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("TIX_FOCUS_DIR", dir)
				log := acFakeOpen(t)
				e := acFocusEnv(t, c, dir)
				e.acPressG(way)
				if c.wantOpen {
					e.until("open called with the address", func() bool { return strings.TrimSpace(acRead(log)) == acFocusURL })
					return
				}
				e.screenHas("T-1: no session to open")
				e.settle()
				if got := acRead(log); got != "" {
					t.Errorf("open called without the right: %q", got)
				}
			})
		}
	}
}

// acPressG: g on the board, g in the card or the "g session" button of the card.
func (e *mvEnv) acPressG(way string) {
	e.t.Helper()
	if way != "board" {
		e.acOpenCard("Alpha")
	}
	if way == "button" {
		e.acClickOn("g session")
		return
	}
	e.key("g")
}
