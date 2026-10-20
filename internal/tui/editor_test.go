package tui_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// stubScript - заглушка EDITOR. Вызов n: пишет seen.n (копия при запуске) и started.n; если есть
// wait.n - ждёт go.n (не дольше 10 с); затем выполняет script.n с путём копии первым аргументом.
const stubScript = `#!/bin/sh
d="$TK_STUB_DIR"; f="$1"
n=$(( $(cat "$d/count" 2>/dev/null || echo 0) + 1 )); echo $n > "$d/count"
cp "$f" "$d/seen.$n"; echo "$f" > "$d/path.$n"; : > "$d/started.$n"
if [ -f "$d/wait.$n" ]; then i=0; while [ ! -f "$d/go.$n" ] && [ $i -lt 500 ]; do sleep 0.02; i=$((i+1)); done; fi
if [ -f "$d/script.$n" ]; then sh "$d/script.$n" "$f"; fi
exit 0
`

type stub struct {
	t   *testing.T
	dir string
}

func newStub(t *testing.T) *stub {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(bin, []byte(stubScript), 0o700); err != nil { //nolint:gosec // заглушка теста
		t.Fatal(err)
	}
	t.Setenv("EDITOR", bin)
	t.Setenv("TK_STUB_DIR", dir)
	return &stub{t: t, dir: dir}
}

func (s *stub) file(name string) string { return filepath.Join(s.dir, name) }

// plan задаёт вызов n: gated - ждать go.n; script - команда sh над копией (пусто - выйти без правки).
func (s *stub) plan(n int, gated bool, script string) {
	s.t.Helper()
	i := strconv.Itoa(n)
	if gated {
		s.touch("wait." + i)
	}
	if script != "" {
		if err := os.WriteFile(s.file("script."+i), []byte(script), 0o600); err != nil {
			s.t.Fatal(err)
		}
	}
}

func (s *stub) touch(name string) {
	s.t.Helper()
	if err := os.WriteFile(s.file(name), nil, 0o600); err != nil {
		s.t.Fatal(err)
	}
}

func (s *stub) seen(n int) string {
	b, _ := os.ReadFile(s.file("seen." + strconv.Itoa(n)))
	return string(b)
}

func (s *stub) calls() int {
	b, _ := os.ReadFile(s.file("count"))
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func copies(e *env) []string {
	m, _ := filepath.Glob(filepath.Join(e.root, ".tracker", "tmp", "ed-T-1-*"))
	return m
}

const base3 = "line1\nline2\nline3\n"

// openEditor: задача T-1 с телом base3, карточка открыта, нажато e.
func openEditor(t *testing.T, st *stub) *env {
	t.Helper()
	e := newEnv(t, "")
	e.create("Alpha", "p", "idea")
	e.body("T-1", base3)
	e.start("", 220, 40)
	e.screenHas("Alpha")
	e.key("enter")
	e.screenHas("Alpha", "new")
	e.key("e")
	e.until("редактор запущен", func() bool { _, err := os.Stat(st.file("started.1")); return err == nil })
	return e
}

func TestEditor_ExitWithoutChange_NoCommitCopyRemoved(t *testing.T) {
	st := newStub(t)
	e := newEnv(t, "")
	e.create("Alpha", "p", "idea")
	e.body("T-1", base3)
	n := e.gitOut("rev-list", "--count", "HEAD")
	e.start("", 220, 40)
	e.screenHas("Alpha")
	e.key("enter")
	e.screenHas("new")
	e.key("e")
	e.until("редактор вызван", func() bool { return st.calls() == 1 })
	e.until("копия удалена", func() bool { return len(copies(e)) == 0 })
	if got := e.gitOut("rev-list", "--count", "HEAD"); got != n {
		t.Errorf("выход без правки создал коммит: было %s, стало %s", n, got)
	}
	if !strings.Contains(st.seen(1), "line2") {
		t.Errorf("редактор открыт не на теле задачи: %q", st.seen(1))
	}
}

func TestEditor_ParallelAgentComment_MergesCleanly(t *testing.T) {
	st := newStub(t)
	st.plan(1, true, `printf 'line1\nline2\nline3\nUSER-LINE\n' > "$1"`)
	e := openEditor(t, st)
	if _, err := e.store.Comment("T-1", "agent says", "agent"); err != nil {
		t.Fatal(err)
	}
	st.touch("go.1")
	e.until("правка и комментарий в файле", func() bool {
		_, _, body, cs := e.get("T-1")
		return strings.Contains(body, "USER-LINE") && strings.Contains(strings.Join(cs, "|"), "agent says")
	})
	e.until("копия удалена", func() bool { return len(copies(e)) == 0 })
	if st.calls() != 1 {
		t.Errorf("редактор вызван %d раз, ждали 1", st.calls())
	}
}

func TestEditor_Conflict_ReopensWithMarkersAndRetrySaves(t *testing.T) {
	st := newStub(t)
	st.plan(1, true, `printf 'line1\nUSER\nline3\n' > "$1"`)
	st.plan(2, false, `printf 'line1\nBOTH\nline3\n' > "$1"`)
	e := openEditor(t, st)
	_, curRev, _ := e.store.Get("T-1")
	if _, err := e.store.Edit("T-1", curRev, "line1\nAGENT\nline3\n", "agent"); err != nil {
		t.Fatal(err)
	}
	st.touch("go.1")
	e.until("второй запуск редактора", func() bool { return st.calls() == 2 })
	for _, want := range []string{"<<<<<<<", "USER", "AGENT"} {
		if !strings.Contains(st.seen(2), want) {
			t.Errorf("повторный запуск без %q в копии:\n%s", want, st.seen(2))
		}
	}
	e.until("слияние сохранено", func() bool { _, _, body, _ := e.get("T-1"); return strings.Contains(body, "BOTH") })
	if _, _, body, _ := e.get("T-1"); strings.Contains(body, "<<<<<<<") {
		t.Errorf("маркеры попали в файл:\n%s", body)
	}
	e.until("копия удалена", func() bool { return len(copies(e)) == 0 })
}

func TestEditor_Conflict_SecondRunUnchanged_Cancels(t *testing.T) {
	st := newStub(t)
	st.plan(1, true, `printf 'line1\nUSER\nline3\n' > "$1"`)
	e := openEditor(t, st)
	_, curRev, _ := e.store.Get("T-1")
	if _, err := e.store.Edit("T-1", curRev, "line1\nAGENT\nline3\n", "agent"); err != nil {
		t.Fatal(err)
	}
	n := e.gitOut("rev-list", "--count", "HEAD")
	st.touch("go.1")
	e.until("второй запуск", func() bool { return st.calls() == 2 })
	e.until("копия удалена", func() bool { return len(copies(e)) == 0 })
	if _, _, body, _ := e.get("T-1"); strings.Contains(body, "USER") || !strings.Contains(body, "AGENT") {
		t.Errorf("отмена изменила файл:\n%s", body)
	}
	if got := e.gitOut("rev-list", "--count", "HEAD"); got != n {
		t.Error("отмена создала коммит")
	}
}

func TestEditor_MarkersLeftInCopy_RefusedCopyKeptPathShown(t *testing.T) {
	st := newStub(t)
	st.plan(1, true, `printf 'line1\nUSER\nline3\n' > "$1"`)
	st.plan(2, false, `printf 'extra\n' >> "$1"`) // правка есть, маркеры остались
	e := openEditor(t, st)
	_, curRev, _ := e.store.Get("T-1")
	if _, err := e.store.Edit("T-1", curRev, "line1\nAGENT\nline3\n", "agent"); err != nil {
		t.Fatal(err)
	}
	n := e.gitOut("rev-list", "--count", "HEAD")
	st.touch("go.1")
	e.until("копия осталась", func() bool { return st.calls() == 2 && len(copies(e)) == 1 })
	e.screenHas("ed-T-1-")
	if _, _, body, _ := e.get("T-1"); strings.Contains(body, "<<<<<<<") || strings.Contains(body, "extra") {
		t.Errorf("копия с маркерами записана:\n%s", body)
	}
	if got := e.gitOut("rev-list", "--count", "HEAD"); got != n {
		t.Error("отказ создал коммит")
	}
}

func TestEditor_StoreLocked_CopyKeptPathShownNothingWritten(t *testing.T) {
	st := newStub(t)
	st.plan(1, true, `printf 'line1\nUSER\nline3\n' > "$1"`)
	e := newEnv(t, "")
	e.create("Alpha", "p", "idea")
	e.body("T-1", base3)
	e.store = e0(t, e.root, 200*time.Millisecond)
	e.start("", 220, 40)
	e.screenHas("Alpha")
	e.key("enter")
	e.screenHas("new")
	e.key("e")
	e.until("редактор запущен", func() bool { _, err := os.Stat(st.file("started.1")); return err == nil })
	lock, err := os.OpenFile(filepath.Join(e.root, ".tracker", "lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	st.touch("go.1")
	e.until("копия осталась", func() bool { return len(copies(e)) == 1 })
	e.screenHas("ed-T-1-")
	if _, _, body, _ := e.get("T-1"); strings.Contains(body, "USER") {
		t.Errorf("запись прошла под занятым локом:\n%s", body)
	}
}
