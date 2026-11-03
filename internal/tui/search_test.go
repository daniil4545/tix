package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
)

func findKey(m *Model, keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			feed(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			feed(m, tea.KeyPressMsg{Code: tea.KeyEscape})
		case "tab":
			feed(m, tea.KeyPressMsg{Code: tea.KeyTab})
		case "ctrl+t":
			feed(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
		default:
			for _, r := range k {
				feed(m, tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
}

// findModel - project p: A (open), Z with a needle in the Abstract; project beta: B with the needle in a title,
// and a closed task C with the needle.
func findModel(t *testing.T) (*Model, map[string]string) {
	t.Helper()
	e := mvNewEnv(t, "")
	ids := map[string]string{}
	add := func(key, title, project, status, body string) {
		r, err := e.store.CreateWithBody(title, project, status, body+"## Details\n- [ ] done\n", "seed")
		if err != nil {
			t.Fatal(err)
		}
		ids[key] = r.ID
	}
	add("A", "Alpha", "p", "open", "")
	add("Z", "Zulu", "p", "open", "## Abstract\nSome words about the Needle in the abstract.\n")
	add("B", "Needle in beta", "beta", "idea", "")
	add("C", "Closed needle", "beta", "idea", "")
	if _, err := e.store.Move(ids["C"], "closed", core.MoveOpts{Message: "gone", Force: true}, "seed"); err != nil {
		t.Fatal(err)
	}
	m := newMouseModel(t, e, 100, 20)
	tasks, _ := e.store.List()
	m.apply(tasks, 0)
	m.project = "p"
	m.rebuild()
	return m, ids
}

func foundIDs(m *Model) string {
	var ids []string
	for _, t := range m.find.found {
		ids = append(ids, t.ID)
	}
	return strings.Join(ids, " ")
}

// S1: f, typed words give results with an excerpt; enter opens the card, esc keeps the board.
func TestSearch_OpenTypeEnterEsc(t *testing.T) {
	m, ids := findModel(t)
	findKey(m, "f")
	if m.find == nil {
		t.Fatal("f did not open the search")
	}
	findKey(m, "NEEDLE")
	if got := foundIDs(m); got != ids["Z"] {
		t.Fatalf("project p: %q, want %s", got, ids["Z"])
	}
	if out := m.View().Content; !strings.Contains(out, ids["Z"]) || !strings.Contains(out, "about the Needle") {
		t.Errorf("no id or excerpt:\n%s", out)
	}
	findKey(m, "enter")
	if m.find != nil || !m.showing || m.card.id != ids["Z"] {
		t.Fatalf("enter: find %v showing %v card %q", m.find, m.showing, m.card.id)
	}

	m, _ = findModel(t)
	findKey(m, "j") // cursor on the second card
	row, col, proj := m.board.Row, m.board.Col, m.project
	findKey(m, "f", "needle", "esc")
	if m.find != nil || m.showing || m.project != proj || m.board.Row != row || m.board.Col != col {
		t.Fatalf("esc: find %v project %q col %d row %d", m.find, m.project, m.board.Col, m.board.Row)
	}
}

// Typed letters that are board keys go to the query, not to the board.
func TestSearch_KeysDoNotReachBoard(t *testing.T) {
	m, _ := findModel(t)
	findKey(m, "f", "qpnj?")
	if m.find == nil || string(m.find.query) != "qpnj?" || m.help || m.pick != nil || m.input != nil {
		t.Fatalf("keys leaked: query %q help %v", string(m.find.query), m.help)
	}
}

// S2: the scope and closed switches change the result inside the search.
func TestSearch_Switches(t *testing.T) {
	m, ids := findModel(t)
	findKey(m, "f", "needle")
	if got := foundIDs(m); got != ids["Z"] {
		t.Fatalf("project: %q", got)
	}
	findKey(m, "tab")
	if m.find == nil || foundIDs(m) != ids["B"]+" "+ids["Z"] && foundIDs(m) != ids["Z"]+" "+ids["B"] {
		t.Fatalf("all: %q", foundIDs(m))
	}
	findKey(m, "ctrl+t")
	if m.find == nil || len(m.find.found) != 3 {
		t.Fatalf("closed shown: %q", foundIDs(m))
	}
	findKey(m, "tab")
	if len(m.find.found) != 1 {
		t.Fatalf("project again: %q", foundIDs(m))
	}
}

// Enter on a task of another project switches the board to it.
func TestSearch_EnterOtherProject(t *testing.T) {
	m, ids := findModel(t)
	findKey(m, "f", "needle", "tab", "enter")
	if !m.showing || m.card.id == "" {
		t.Fatal("card not open")
	}
	if tk := m.byID(m.card.id); tk == nil || !strings.EqualFold(tk.Project, m.project) {
		t.Fatalf("card %s not in project %q", m.card.id, m.project)
	}
	_ = ids
}

func TestExcerpt_AroundMatch(t *testing.T) {
	text := strings.Repeat("word ", 40) + "NeEdLe tail " + strings.Repeat("x", 100)
	got := excerpt(text, "needle")
	if n := len([]rune(got)); n > excerptLen || !strings.Contains(got, "NeEdLe") {
		t.Errorf("excerpt %d runes: %q", n, got)
	}
	if got := excerpt("short Needle", "needle"); got != "short Needle" {
		t.Errorf("short: %q", got)
	}
}

// S3: one typed character over 1000 tasks takes under 30 ms.
func TestSearch_OneCharOn1000Tasks(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	m, _ := findModel(t)
	var tasks []*task.Task
	for i := 1; i <= 1000; i++ {
		tasks = append(tasks, &task.Task{ID: fmt.Sprintf("T-%d", i), Title: fmt.Sprintf("Task number %d", i), Project: "p", Status: "open",
			Body: "## Abstract\nSome abstract text for the task, long enough to scan.\n\n## Details\n- [ ] first criterion\n- [ ] second criterion\n"})
	}
	m.tasks = tasks
	m.startSearch()
	m.find.all = true
	m.find.query = []rune("criterio")
	start := time.Now()
	m.searchKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if d := time.Since(start); d > 30*time.Millisecond {
		t.Errorf("one char took %v", d)
	}
	if len(m.find.found) != 1000 {
		t.Errorf("found %d", len(m.find.found))
	}
}
