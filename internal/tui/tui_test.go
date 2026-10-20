package tui_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"tracker/internal/core"
)

func TestBoard_NavigateOpenCardEscKeepsSelection(t *testing.T) {
	e := newEnv(t, "")
	e.create("Alpha", "p", "idea")
	e.create("Bravo", "p", "idea")
	e.create("Gamma", "p", "todo")
	e.body("T-2", "## Details\n- [ ] one\n")
	if _, err := e.store.Comment("T-2", "hello from agent", "agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Set("T-2", []core.Field{{Name: "ask", Value: "pick A or B"}}, "agent"); err != nil {
		t.Fatal(err)
	}
	e.start("", 140, 40)
	e.screenHas("idea", "todo", "doing", "blocked", "done", "Alpha", "Bravo", "Gamma")

	e.key("j", "enter")
	e.screenHas("Bravo", "hello from agent", "* needs you: pick A or B")
	if s := e.text(); strings.Contains(s, "Alpha") || strings.Contains(s, "Gamma") {
		t.Errorf("карточка показывает чужие задачи:\n%s", s)
	}
	e.key("esc")
	e.screenHas("Alpha", "Gamma")
	e.key("enter") // выбор сохранён: снова Bravo
	e.screenHas("Bravo", "hello from agent")
	e.key("esc", "l", "enter")
	e.screenHas("Gamma", "new")
}

func TestBoard_EmptyStoreAndBrokenFile(t *testing.T) {
	e := newEnv(t, "")
	e.start("", 140, 30)
	e.screenHas("idea", "todo", "done")

	b := newEnv(t, "")
	b.create("Alpha", "p", "idea")
	if err := os.WriteFile(filepath.Join(b.root, "T-9 broken.md"), []byte("no frontmatter at all\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.start("", 140, 30)
	b.screenHas("Alpha", "1 file skipped")
}

const closeCfg = `[[column]]
name = "wait"
role = "waiting"

[[column]]
name = "fin"
role = "closed"
`

const moveCfg = `[[column]]
name = "work"
role = "active"

[[column]]
name = "wait"
role = "waiting"

[[column]]
name = "fin"
role = "closed"
`

func TestMove_ToWaiting_AsksReasonOnlyWhenFieldEmpty(t *testing.T) {
	t.Run("ввод и enter", func(t *testing.T) {
		e := newEnv(t, moveCfg)
		e.create("Alpha", "p", "work")
		e.start("", 140, 30)
		e.screenHas("Alpha")
		e.key("m")
		e.screenHas("waiting for:")
		e.tm.Type("vendor reply")
		e.key("enter")
		e.until("задача в wait с причиной", func() bool { st, w, _, _ := e.get("T-1"); return st == "wait" && w == "vendor reply" })
	})
	t.Run("esc отменяет", func(t *testing.T) {
		e := newEnv(t, moveCfg)
		e.create("Alpha", "p", "work")
		n := e.gitOut("rev-list", "--count", "HEAD")
		e.start("", 140, 30)
		e.screenHas("Alpha")
		e.key("m")
		e.screenHas("waiting for:")
		e.key("esc")
		e.until("строка ввода закрыта", func() bool { return !strings.Contains(e.text(), "waiting for:") })
		if st, _, _, _ := e.get("T-1"); st != "work" || e.gitOut("rev-list", "--count", "HEAD") != n {
			t.Errorf("отмена изменила задачу: статус %s", st)
		}
	})
	t.Run("поле уже заполнено - без вопроса", func(t *testing.T) {
		e := newEnv(t, moveCfg)
		e.create("Alpha", "p", "work")
		if _, err := e.store.Set("T-1", []core.Field{{Name: "waiting", Value: "set earlier"}}, "seed"); err != nil {
			t.Fatal(err)
		}
		e.start("", 140, 30)
		e.screenHas("Alpha")
		e.key("m")
		e.until("задача в wait", func() bool { st, _, _, _ := e.get("T-1"); return st == "wait" })
	})
}

func TestMove_RuleViolation_ShownInStatusLine(t *testing.T) {
	e := newEnv(t, "")
	e.create("Alpha", "p", "idea") // в todo нужен критерий
	_, err := e.store.Move("T-1", "todo", core.MoveOpts{}, "probe")
	if err == nil {
		t.Fatal("ядро пустило задачу без критерия в ready")
	}
	e.start("", 140, 30)
	e.screenHas("Alpha")
	e.key("m")
	e.screenHas(err.Error())
	if st, _, _, _ := e.get("T-1"); st != "idea" {
		t.Errorf("статус %s после отказа", st)
	}
}

func TestMove_ToClosed_CriteriaCases(t *testing.T) {
	cases := []struct {
		name, body, reply, subject, comment string
	}{
		{"неотмеченные критерии: причина и Force", "## Details\n- [ ] one\n", "skipped", "T-1: done force", "Result: skipped"},
		{"критериев нет: причина как -m", "just text\n", "fine", "T-1: done", "Result: fine"},
		{"все отмечены: без вопроса", "## Details\n- [x] one\n", "", "T-1: done", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, closeCfg)
			e.create("Alpha", "p", "wait")
			e.body("T-1", c.body)
			e.start("", 140, 30)
			e.screenHas("Alpha")
			e.key("m")
			if c.reply != "" {
				e.screenHas("close reason:")
				e.tm.Type(c.reply)
				e.key("enter")
			}
			e.until("задача в fin", func() bool { st, _, _, _ := e.get("T-1"); return st == "fin" })
			e.until("коммит "+c.subject, func() bool { return e.gitOut("log", "-1", "--format=%s") == c.subject })
			_, _, _, comments := e.get("T-1")
			if c.comment != "" && !strings.Contains(strings.Join(comments, "|"), c.comment) {
				t.Errorf("комментарии %v без %q", comments, c.comment)
			}
			if c.reply == "" && strings.Contains(e.text(), "close reason:") {
				t.Error("вопрос показан, хотя критерии отмечены")
			}
		})
	}
}

func order(e *env, col string) []string {
	all, _ := e.store.List()
	var ts = all[:0:0]
	for _, tk := range all {
		if tk.Status == col {
			ts = append(ts, tk)
		}
	}
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Rank < ts[j].Rank })
	var out []string
	for _, tk := range ts {
		out = append(out, tk.Title)
	}
	return out
}

func TestReorder_JK_MovesWithinColumn_EdgesDoNothing(t *testing.T) {
	e := newEnv(t, "")
	for _, title := range []string{"Alpha", "Bravo", "Charlie", "Delta"} {
		e.create(title, "p", "idea")
	}
	e.start("", 140, 40)
	e.screenHas("Alpha", "Delta")
	step := func(k string, want string) {
		t.Helper()
		e.key(k)
		e.until("порядок "+want, func() bool { return strings.Join(order(e, "idea"), ",") == want })
		e.until("экран с порядком "+want, func() bool {
			s, prev := e.text(), -1
			for _, title := range strings.Split(want, ",") {
				i := strings.Index(s, title)
				if i < prev || i < 0 {
					return false
				}
				prev = i
			}
			return true
		})
	}
	step("J", "Bravo,Alpha,Charlie,Delta")
	step("J", "Bravo,Charlie,Alpha,Delta")
	step("K", "Bravo,Alpha,Charlie,Delta")
	step("K", "Alpha,Bravo,Charlie,Delta") // к верху - First
	n := e.gitOut("rev-list", "--count", "HEAD")
	e.key("K") // у верха ничего
	e.key("j", "j", "j", "J")
	e.key("esc")
	e.screenHas("Alpha")
	if got := e.gitOut("rev-list", "--count", "HEAD"); got != n {
		t.Errorf("на краю появился коммит: было %s, стало %s", n, got)
	}
}

func TestFilterProject_CyclesAndInitialFilter(t *testing.T) {
	e := newEnv(t, "")
	e.create("Alpha task", "alpha", "idea")
	e.create("Beta task", "beta", "idea")
	e.start("", 140, 30)
	e.screenHas("Alpha task", "Beta task")
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		e.key("p")
		e.until("кадр после p", func() bool {
			s := e.text()
			a, b := strings.Contains(s, "Alpha task"), strings.Contains(s, "Beta task")
			switch {
			case a && b:
				seen["all"] = true
			case a:
				seen["alpha"] = true
			case b:
				seen["beta"] = true
			default:
				return false
			}
			return true
		})
	}
	if !seen["all"] || !seen["alpha"] || !seen["beta"] {
		t.Errorf("за четыре нажатия p видели %v; ждали все, alpha, beta", seen)
	}

	f := newEnv(t, "")
	f.create("Alpha task", "alpha", "idea")
	f.create("Beta task", "beta", "idea")
	f.start("alpha", 140, 30)
	f.screenHas("Alpha task")
	if strings.Contains(f.text(), "Beta task") {
		t.Errorf("начальный фильтр alpha показывает beta:\n%s", f.text())
	}
}

func TestCard_Comment_WrittenAsHumanEmptyCancels(t *testing.T) {
	e := newEnv(t, "")
	e.create("Alpha", "p", "idea")
	e.start("", 140, 30)
	e.screenHas("Alpha")
	e.key("enter")
	e.screenHas("new")
	n := e.gitOut("rev-list", "--count", "HEAD")
	e.key("c", "enter") // пустой текст - отмена
	if got := e.gitOut("rev-list", "--count", "HEAD"); got != n {
		t.Error("пустой комментарий создал коммит")
	}
	e.key("c")
	e.tm.Type("note to self")
	e.key("enter")
	e.until("комментарий записан", func() bool {
		_, _, _, cs := e.get("T-1")
		return len(cs) == 1 && cs[0] == "human: note to self"
	})
	e.screenHas("note to self")
}

func TestCard_HistoryFoldsSetsAndShowsCommentOnce(t *testing.T) {
	e := newEnv(t, "")
	e.create("Alpha", "p", "idea")
	for _, tag := range []string{"a", "b", "c"} {
		if _, err := e.store.Set("T-1", []core.Field{{Name: "tags", Value: tag}}, "claude:x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.store.Comment("T-1", "unique-comment-text", "daniil"); err != nil {
		t.Fatal(err)
	}
	e.start("", 140, 40)
	e.screenHas("Alpha")
	e.key("enter")
	e.screenHas("3 edits", "unique-comment-text")
	if n := strings.Count(e.text(), "unique-comment-text"); n != 1 {
		t.Errorf("комментарий показан %d раз, ждали 1:\n%s", n, e.text())
	}
}
