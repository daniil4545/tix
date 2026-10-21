package tui

// Card actions and the jump into a session.
// The model is driven directly through Update, without a terminal.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
)

var cardANSI = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

func caEnter(m *Model) { feed(m, tea.KeyPressMsg{Code: tea.KeyEnter}) }

// frameLines - the screen text without escape codes, by lines.
func caLines(m *Model) []string {
	return strings.Split(cardANSI.ReplaceAllString(m.View().Content, ""), "\n")
}

// xyOf looks for a substring on the screen: the cell of its first character.
func caXY(t *testing.T, m *Model, sub string) (x, y int) {
	t.Helper()
	for y, ln := range caLines(m) {
		if i := strings.Index(ln, sub); i >= 0 {
			return len([]rune(ln[:i])), y
		}
	}
	t.Fatalf("%q is not on the screen:\n%s", sub, strings.Join(caLines(m), "\n"))
	return 0, 0
}

func (e *mvEnv) caTask(id string) (ask, due, status string, comments []string) {
	e.t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		e.t.Fatalf("Get %s: %v", id, err)
	}
	for _, c := range tk.Comments {
		comments = append(comments, c.Text)
	}
	return tk.Ask, tk.Due, tk.Status, comments
}

func caStatus(m *Model) string { s, _ := m.statusText(); return s }

func TestAnswer_WritesCommentThenClearsAsk(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.set(id, "ask", "which db?")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	typeKeys(m, "a")
	if m.input == nil || m.input.label != "answer:" {
		t.Fatalf("after a the input line is %+v, expected answer:", m.input)
	}
	typeKeys(m, "postgres")
	caEnter(m)
	ask, _, _, comments := e.caTask(id)
	if ask != "" || len(comments) != 1 || comments[0] != "Answer: postgres" {
		t.Errorf("ask=%q comments=%q; expected an empty ask and one comment Answer: postgres", ask, comments)
	}
}

func TestAnswer_AskChangedMeanwhile_QuestionStays(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.set(id, "ask", "which db?")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	typeKeys(m, "a")
	typeKeys(m, "postgres")
	e.set(id, "ask", "which version?") // the agent managed to ask a new question
	caEnter(m)
	ask, _, _, comments := e.caTask(id)
	if ask != "which version?" {
		t.Errorf("ask = %q, expected the new question untouched", ask)
	}
	if len(comments) != 1 {
		t.Errorf("comments %d, expected the answer to the old question", len(comments))
	}
	if got := caStatus(m); got != id+": new question" {
		t.Errorf("status %q, expected %q", got, id+": new question")
	}
}

func TestAnswer_NoQuestionOrEmpty_NothingWritten(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	typeKeys(m, "a")
	if got := caStatus(m); got != id+": no question" || m.input != nil {
		t.Errorf("without a question: status %q, input %v", got, m.input)
	}
	e.set(id, "ask", "q?")
	before := e.commits()
	caReload(m)
	typeKeys(m, "a")
	caEnter(m) // empty - cancel
	if e.commits() != before {
		t.Error("an empty answer left a commit")
	}
}

func caOpenCard(m *Model) {
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		m.Update(cmd()) // the card history arrives as a message
	}
}

func TestField_SetClearUnchangedRefused(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)

	typeKeys(m, "fd2026-11-01")
	caEnter(m)
	if _, due, _, _ := e.caTask(id); due != "2026-11-01" {
		t.Fatalf("due = %q, expected 2026-11-01", due)
	}

	caReload(m)
	n := e.commits()
	typeKeys(m, "fd") // the initial value in the line is the current one
	caEnter(m)
	if e.commits() != n {
		t.Error("the same value was written again")
	}

	typeKeys(m, "fd")
	feed(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	caEnter(m)
	if _, due, _, _ := e.caTask(id); due != "" {
		t.Errorf("an empty value did not clear the due: %q", due)
	}

	typeKeys(m, "fdnot-a-date")
	caEnter(m)
	if got := caStatus(m); !strings.Contains(got, "due") {
		t.Errorf("core refusal not visible: status %q", got)
	}
}

func TestField_SummaryWithSeveralLines_RefusesToEditInline(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.summary(id, "line one\nline two")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	typeKeys(m, "fs")
	if got := caStatus(m); got != id+": summary has several lines, use e" || m.input != nil {
		t.Errorf("status %q, input %v", got, m.input)
	}
}

func TestCardClick_AskRowFieldsRowAndButtons(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.set(id, "ask", "which db?")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)

	x, y := caXY(t, m, "* needs you:")
	leftClick(m, x, y)
	if m.input == nil || m.input.label != "answer:" {
		t.Fatalf("click on the ask: input %+v", m.input)
	}
	feed(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	leftClick(m, 0, 1) // fields line: right under the header
	if m.input == nil || m.input.pick == nil {
		t.Fatalf("click on the fields line: input %+v", m.input)
	}
	// A click on a choice pair equals its key.
	x, y = caXY(t, m, "d due")
	leftClick(m, x, y)
	if m.input == nil || m.input.label != "due:" {
		t.Fatalf("click on d due: input %+v", m.input)
	}
	feed(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	x, y = caXY(t, m, "c comment")
	leftClick(m, x, y)
	if m.input == nil || m.input.label != "comment:" {
		t.Fatalf("click on c comment: input %+v", m.input)
	}
	feed(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	x, y = caXY(t, m, "set ask")
	leftClick(m, x, y) // card body without a button
	if m.input != nil {
		t.Errorf("click on an empty place opened the input line %+v", m.input)
	}
	x, y = caXY(t, m, "esc back")
	leftClick(m, x, y)
	if m.showing {
		t.Error("click on esc back did not close the card")
	}
}

func TestFocus_G_OpensOnlyValidURLOfLiveClaim(t *testing.T) {
	var opened []string

	const good = "warp://session/0123456789abcdef0123456789abcdef"
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	m.acts.open = func(url string) error { opened = append(opened, url); return nil }

	typeKeys(m, "g")
	if got := caStatus(m); got != id+": no session to open" || len(opened) != 0 {
		t.Fatalf("without a claim: status %q, open %v", got, opened)
	}

	if _, err := e.store.Take(id, core.Take, "claude:abcd1234"); err != nil {
		t.Fatal(err)
	}
	caReload(m)
	typeKeys(m, "g")
	if len(opened) != 0 {
		t.Fatalf("claim exists, no focus file, but open was called: %v", opened)
	}

	dir := t.TempDir()
	t.Setenv("TIX_FOCUS_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"-a Calculator\n", good + "\nextra\n", "warp://session/ZZ\n", ""} {
		if err := os.WriteFile(filepath.Join(dir, "claude-abcd1234"), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		typeKeys(m, "g")
		if len(opened) != 0 {
			t.Fatalf("file %q passed the check, open %v", bad, opened)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "claude-abcd1234"), []byte(good+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	typeKeys(m, "g")
	if len(opened) != 1 || opened[0] != good {
		t.Fatalf("open called with %v, expected [%s]", opened, good)
	}
	caOpenCard(m)
	x, y := caXY(t, m, "g session")
	leftClick(m, x, y)
	if len(opened) != 2 {
		t.Errorf("button g session did not open a session: %v", opened)
	}
}

// caReload rereads the store, as the file watcher does.
func caReload(m *Model) {
	tasks, errs := m.d.Store.List()
	m.apply(tasks, len(errs))
}

func (e *mvEnv) caBody(id, body string) {
	e.t.Helper()
	_, rev, err := e.store.Get(id)
	if err == nil {
		_, err = e.store.Edit(id, rev, body, "seed")
	}
	if err != nil {
		e.t.Fatalf("body %s: %v", id, err)
	}
}

func TestCriteria_SelectedByCursor_EnterChecksAndUnchecks(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	const body = "## Details\nplain line\n- [ ] one\n- [x] two\n"
	e.caBody(id, body)
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	if m.card.nCrit != 2 {
		t.Fatalf("criteria in the choice %d, expected 2", m.card.nCrit)
	}
	// Enter on a history entry does not touch the criteria.
	caEnter(m)
	if tk, _, _ := e.store.Get(id); tk.Body != body {
		t.Fatalf("enter on the history changed the body: %q", tk.Body)
	}

	m.card.step(-100) // first criterion
	caEnter(m)
	tk, _, _ := e.store.Get(id)
	if c := tk.Criteria(); !c[0].Done || !c[1].Done {
		t.Fatalf("after the check: %+v", c)
	}
	caReload(m)
	m.card.step(1) // second criterion, it is checked
	caEnter(m)
	tk, _, _ = e.store.Get(id)
	if c := tk.Criteria(); !c[0].Done || c[1].Done {
		t.Fatalf("after the uncheck: %+v", c)
	}
	if want := "## Details\nplain line\n- [x] one\n- [ ] two\n"; tk.Body != want {
		t.Errorf("body after the uncheck = %q, expected %q (the rest did not change)", tk.Body, want)
	}
}

func TestCriteria_ClickOnRowToggles(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.caBody(id, "## Details\n- [ ] first\n")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	x, y := caXY(t, m, "- [ ] first")
	leftClick(m, x, y)
	tk, _, _ := e.store.Get(id)
	if !tk.Criteria()[0].Done {
		t.Errorf("click on the criterion did not check it: %q", tk.Body)
	}
}

func TestCriteria_ChangedMeanwhile_NotToggled(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.caBody(id, "## Details\n- [ ] first\n")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	e.caBody(id, "## Details\n- [ ] other\n") // the model has not reread the file yet
	m.card.step(-100)
	caEnter(m)
	tk, _, _ := e.store.Get(id)
	if tk.Criteria()[0].Done {
		t.Error("checked a criterion the user did not see")
	}
}

func TestDelete_ConfirmByY_AnyOtherKeyCancels(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	feed(m, tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.input == nil || m.input.label != "delete "+id+"? y/n" {
		t.Fatalf("input %+v", m.input)
	}
	typeKeys(m, "n")
	if _, _, err := e.store.Get(id); err != nil || m.input != nil {
		t.Fatalf("n deleted the task or left the line: err=%v input=%v", err, m.input)
	}
	feed(m, tea.KeyPressMsg{Code: tea.KeyDelete})
	typeKeys(m, "y")
	if _, _, err := e.store.Get(id); err == nil {
		t.Error("y did not delete the task")
	}
	if m.showing {
		t.Error("the card stayed after deletion from the card")
	}
}

func TestDelete_SecondDeleteOrBackspace_Cancels(t *testing.T) {
	for _, second := range []tea.KeyPressMsg{{Code: tea.KeyDelete}, {Code: tea.KeyBackspace}} {
		e := mvNewEnv(t, "")
		id := e.create("job", "todo")
		m := newMouseModel(t, e, 120, 30)
		m.selectTask(id)
		feed(m, tea.KeyPressMsg{Code: tea.KeyDelete})
		feed(m, second)
		if _, _, err := e.store.Get(id); err != nil || m.input != nil {
			t.Errorf("%v with an open question: err=%v input=%v; expected a cancel without deletion", second, err, m.input)
		}
	}
}

func TestFocus_G_ExpiredClaim_NoSession(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	if _, err := e.store.Take(id, core.Take, "claude:abcd1234"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("TIX_FOCUS_DIR", dir)
	url := "warp://session/0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(filepath.Join(dir, "claude-abcd1234"), []byte(url+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	var opened []string
	m.acts.open = func(u string) error { opened = append(opened, u); return nil }
	m.now = func() time.Time { return time.Now().Add(10000 * time.Hour) }
	typeKeys(m, "g")
	if got := caStatus(m); got != id+": no session to open" || len(opened) != 0 {
		t.Errorf("expired claim: status %q, open %v", got, opened)
	}
}
