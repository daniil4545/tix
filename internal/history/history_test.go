package history_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/history"
	"github.com/daniil4545/tix/internal/task"
)

var gitBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tix-history-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if gitBin, err = exec.LookPath("git"); err != nil {
		fmt.Fprintln(os.Stderr, "git not found")
		os.Exit(2)
	}
	os.Setenv("HOME", dir)
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_DIR", "GIT_WORK_TREE"} {
		os.Unsetenv(k)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func at(h, m int) time.Time { return time.Date(2026, 10, 1, h, m, 0, 0, time.UTC) }

func clock(t *testing.T, when time.Time) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_DATE", when.Format("2006-01-02T15:04:05-0700"))
	t.Setenv("GIT_COMMITTER_DATE", when.Format("2006-01-02T15:04:05-0700"))
}

func store(t *testing.T) (*core.Store, gitx.Repo) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tasks")
	s, err := core.Open(root, core.Options{GitBin: gitBin})
	if err != nil {
		t.Fatal(err)
	}
	return s, gitx.Repo{Dir: root, Bin: gitBin}
}

func ops(es []history.Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Op)
	}
	return out
}

func TestLoad_CommitsAndFileCommentsByTime_CommentCommitNotDuplicated(t *testing.T) {
	s, repo := store(t)
	clock(t, at(10, 0))
	if _, err := s.Create("Alpha", "p", "todo", "claude:a"); err != nil {
		t.Fatal(err)
	}
	clock(t, at(10, 10))
	if _, err := s.Take("T-1", core.Take, "claude:a"); err != nil {
		t.Fatal(err)
	}
	clock(t, at(10, 20))
	if _, err := s.Note("T-1", "half done", "claude:a"); err != nil {
		t.Fatal(err)
	}
	clock(t, at(11, 0))
	if _, err := s.Comment("T-1", "from commit", "daniil"); err != nil {
		t.Fatal(err)
	}
	clock(t, at(12, 0))
	if _, err := s.Move("T-1", "done", core.MoveOpts{Message: "finished"}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	tk, _, err := s.Get("T-1")
	if err != nil {
		t.Fatal(err)
	}
	// Время комментария из файла задаёт тест: ровно между note и done.
	tk.Comments = []task.Comment{{At: at(11, 0), Actor: "daniil", Text: "hello"}}

	got, err := history.Load(repo, tk)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"new", "take", "note", "comment", "done"}
	if g := ops(got); strings.Join(g, ",") != strings.Join(want, ",") {
		t.Fatalf("операции по времени: %v, ждали %v", g, want)
	}
	if c := got[3]; c.Actor != "daniil" || !strings.Contains(c.Text, "hello") {
		t.Errorf("комментарий из файла: %+v", c)
	}
	if n := got[2]; !strings.Contains(n.Text, "half done") || n.Actor != "claude:a" {
		t.Errorf("note: %+v", n)
	}
}

func TestLoad_ExternalEdits_OwnAuthor_SeveralIDsInOneCommit(t *testing.T) {
	s, repo := store(t)
	clock(t, at(9, 0))
	for _, title := range []string{"Alpha", "Beta"} {
		if _, err := s.Create(title, "p", "todo", "claude:a"); err != nil {
			t.Fatal(err)
		}
	}
	// Правка мимо ядра обоих файлов; следующая операция коммитит её как external edit.
	editOutside(t, repo.Dir, "T-1")
	editOutside(t, repo.Dir, "T-2")
	clock(t, at(9, 30))
	if _, err := s.Note("T-1", "after", "claude:a"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T-1", "T-2"} {
		tk, _, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		es, err := history.Load(repo, tk)
		if err != nil {
			t.Fatal(err)
		}
		if !hasExternalEdit(es) {
			t.Errorf("%s: нет строки external edit от автора external: %+v", id, es)
		}
	}
}

// editOutside правит файл задачи мимо ядра.
func editOutside(t *testing.T, dir, id string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, id+" *.md"))
	if len(matches) != 1 {
		t.Fatalf("файл %s: %v", id, matches)
	}
	data, _ := os.ReadFile(matches[0])
	if err := os.WriteFile(matches[0], []byte(strings.Replace(string(data), "tags: []", "tags: [x]", 1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hasExternalEdit(es []history.Entry) bool {
	for _, e := range es {
		if e.Actor == "external" && e.Op == "external edit" {
			return true
		}
	}
	return false
}

func TestLoad_UnknownCommitType_ShownAsIs(t *testing.T) {
	_, repo := store(t)
	clock(t, at(8, 0))
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(nil, "T-1: frobnicate now", "claude:a"); err != nil {
		t.Fatal(err)
	}
	es, err := history.Load(repo, &task.Task{ID: "T-1"})
	if err != nil || len(es) != 1 || !strings.Contains(es[0].Op+" "+es[0].Text, "frobnicate") {
		t.Fatalf("Load = %+v, %v; ждали одну строку со словом frobnicate", es, err)
	}
}

func TestLoad_NoCommits_OnlyFileComments(t *testing.T) {
	repo := gitx.Repo{Dir: t.TempDir(), Bin: gitBin}
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	tk := &task.Task{ID: "T-7", Comments: []task.Comment{{At: at(10, 0), Actor: "a", Text: "x"}}}
	es, err := history.Load(repo, tk)
	if err != nil || len(es) != 1 || es[0].Op != "comment" {
		t.Fatalf("Load без коммитов = %+v, %v; ждали один комментарий", es, err)
	}
}

func e(op, actor string, m int) history.Entry {
	return history.Entry{At: at(10, m), Actor: actor, Op: op}
}

func TestFold(t *testing.T) {
	cases := []struct {
		name string
		in   []history.Entry
		want string // операции результата через запятую; "set*3" - свёрнутая строка из трёх
	}{
		{"два подряд одного автора", []history.Entry{e("set", "A", 1), e("set", "A", 2)}, "fold(2)"},
		{"одна правка не сворачивается", []history.Entry{e("set", "A", 1)}, "set"},
		{"разные типы мелких правок одного автора", []history.Entry{e("set", "A", 1), e("check", "A", 2), e("edit", "A", 3)}, "fold(3)"},
		{"разные авторы", []history.Entry{e("set", "A", 1), e("set", "B", 2)}, "set,set"},
		{"комментарий разрывает серию", []history.Entry{e("set", "A", 1), e("comment", "A", 2), e("set", "A", 3)}, "set,comment,set"},
		{"видимые типы не сворачиваются", []history.Entry{e("note", "A", 1), e("note", "A", 2), e("move", "A", 3), e("move", "A", 4)}, "note,note,move,move"},
		{"external отдельный автор", []history.Entry{e("external edit", "external", 1), e("external edit", "external", 2), e("edit", "A", 3), e("edit", "A", 4)}, "fold(2),fold(2)"},
		{"серия между видимыми", []history.Entry{e("take", "A", 1), e("set", "A", 2), e("release", "A", 3), e("done", "A", 4)}, "take,fold(2),done"},
		{"незнакомый тип виден", []history.Entry{e("frobnicate", "A", 1), e("frobnicate", "A", 2)}, "frobnicate,frobnicate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := history.Fold(c.in)
			var parts []string
			for _, g := range got {
				if len(g.Folded) > 0 {
					parts = append(parts, fmt.Sprintf("fold(%d)", len(g.Folded)))
					if want := fmt.Sprintf("%d edits", len(g.Folded)); !strings.Contains(g.Text, want) {
						t.Errorf("свёрнутая строка %+v: ждали текст %q", g, want)
					}
				} else {
					parts = append(parts, g.Op)
				}
			}
			if s := strings.Join(parts, ","); s != c.want {
				t.Errorf("Fold = %s, ждали %s", s, c.want)
			}
		})
	}
}
