package core_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"tracker/internal/core"
)

// Заголовок раздела комментариев: при смене языка формата правится здесь.
const commentsHeading = "## Comments"

var gitBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tk-core-")
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

func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("паника: %v", r)
	}
}

type env struct {
	t    *testing.T
	root string
	fake string // каталог обёртки git
	log  *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, root: filepath.Join(t.TempDir(), "tasks"), fake: t.TempDir(), log: &bytes.Buffer{}}
	script := "#!/bin/sh\nD=" + e.fake + "\necho \"$*\" >> \"$D/calls.log\"\n" +
		"if [ -f \"$D/failcommit\" ]; then for a in \"$@\"; do [ \"$a\" = commit ] && { echo 'fatal: boom' >&2; exit 128; }; done; fi\n" +
		"exec " + gitBin + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(e.fake, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) open(timeout time.Duration) *core.Store {
	e.t.Helper()
	s, err := core.Open(e.root, core.Options{
		LockTimeout: timeout, GitBin: filepath.Join(e.fake, "git"),
		Log: slog.New(slog.NewTextHandler(e.log, nil)),
	})
	if err != nil {
		e.t.Fatalf("Open: %v", err)
	}
	return s
}

func (e *env) git(args ...string) string {
	e.t.Helper()
	out, err := exec.CommandContext(context.Background(), gitBin, append([]string{"-C", e.root}, args...)...).CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (e *env) file(id string) string {
	e.t.Helper()
	all, _ := filepath.Glob(filepath.Join(e.root, id+"*.md"))
	var m []string
	for _, f := range all {
		if n := filepath.Base(f); strings.HasPrefix(n, id+" ") || strings.HasPrefix(n, id+"-") {
			m = append(m, f)
		}
	}
	if len(m) != 1 {
		e.t.Fatalf("файл %s: найдено %d", id, len(m))
	}
	return m[0]
}

func (e *env) calls() string {
	d, _ := os.ReadFile(filepath.Join(e.fake, "calls.log"))
	return string(d)
}

func (e *env) count() int {
	var n int
	fmt.Sscan(strings.TrimSpace(e.git("rev-list", "--count", "HEAD")), &n)
	return n
}

func mustCreate(t *testing.T, s *core.Store, title string) core.Result {
	t.Helper()
	r, err := s.Create(title, "proj", "todo", "tester")
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	return r
}

func TestStore_CreateGetComment_RevIsBlobOfFile(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	r := mustCreate(t, s, "Alpha")
	if r.ID != "T-1" || r.Commit == "" || r.Warning != "" {
		t.Fatalf("Create: %+v, ждали ID T-1 и коммит без предупреждения", r)
	}
	if head := strings.TrimSpace(e.git("rev-parse", "HEAD")); !strings.HasPrefix(head, r.Commit) && !strings.HasPrefix(r.Commit, head) {
		t.Errorf("Result.Commit %q не HEAD %q", r.Commit, head)
	}
	c, err := s.Comment("T-1", "привет", "tester")
	if err != nil {
		t.Fatal(err)
	}
	blob := strings.TrimSpace(e.git("hash-object", e.file("T-1")))
	if c.Rev != blob {
		t.Errorf("Result.Rev %q, git blob файла %q", c.Rev, blob)
	}
	tk, rev, err := s.Get("T-1")
	if err != nil {
		t.Fatal(err)
	}
	if rev != blob || tk.Title != "Alpha" || tk.Status != "todo" || tk.Project != "proj" || len(tk.Comments) != 1 {
		t.Errorf("Get: rev=%q task=%+v", rev, tk)
	}
	if out := e.git("cat-file", "-t", rev); strings.TrimSpace(out) != "blob" {
		t.Errorf("Get не записал blob %s в git: %q", rev, out)
	}
}

func TestStore_NotFoundAndBadColumn(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	n := e.count()
	if _, err := s.Create("X", "proj", "nosuch", "tester"); err == nil {
		t.Error("неизвестная колонка принята")
	}
	if _, _, err := s.Get("T-99"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("Get T-99: %v, ждали ErrNotFound", err)
	}
	for name, f := range map[string]func() (core.Result, error){
		"Comment": func() (core.Result, error) { return s.Comment("T-99", "x", "a") },
		"Note":    func() (core.Result, error) { return s.Note("T-99", "x", "a") },
		"Edit":    func() (core.Result, error) { return s.Edit("T-99", "deadbeef", "x\n", "a") },
		"Remove":  func() (core.Result, error) { return s.Remove("T-99", "a") },
	} {
		if _, err := f(); !errors.Is(err, core.ErrNotFound) {
			t.Errorf("%s T-99: %v, ждали ErrNotFound", name, err)
		}
	}
	if e.count() != n {
		t.Error("ошибки создали коммиты")
	}
}

func TestStore_NoteEmptyCommit_RemoveDeletes(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	if _, err := s.Note("T-1", "шаг", "tester"); err != nil {
		t.Fatal(err)
	}
	if subj := strings.TrimSpace(e.git("log", "-1", "--format=%s")); !strings.HasPrefix(subj, "T-1: note") || !strings.Contains(subj, "шаг") {
		t.Errorf("тема note %q, ждали `T-1: note шаг`", subj)
	}
	if files := strings.TrimSpace(e.git("show", "--name-only", "--format=", "HEAD")); files != "" {
		t.Errorf("note несёт файлы: %q", files)
	}
	f := e.file("T-1")
	if _, err := s.Remove("T-1", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f); err == nil {
		t.Error("Remove оставил файл")
	}
	if _, _, err := s.Get("T-1"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("Get после Remove: %v", err)
	}
}

func TestStore_Edit_MergeConflictNoopStale(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	body := func(first, last string) string {
		var l []string
		for i := 1; i <= 12; i++ {
			l = append(l, fmt.Sprintf("l%02d", i))
		}
		l[0], l[11] = first, last
		return strings.Join(l, "\n") + "\n"
	}
	_, rev0, _ := s.Get("T-1")
	if _, err := s.Edit("T-1", rev0, body("l01", "l12"), "a"); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	_, revA, _ := s.Get("T-1")
	if _, err := s.Edit("T-1", revA, body("A-first", "l12"), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Edit("T-1", revA, body("l01", "B-last"), "b"); err != nil {
		t.Fatalf("слияние по старой базе: %v", err)
	}
	got, _ := os.ReadFile(e.file("T-1"))
	if !strings.Contains(string(got), "A-first") || !strings.Contains(string(got), "B-last") {
		t.Errorf("слияние потеряло правку:\n%s", got)
	}
	// конфликт
	n := e.count()
	if _, err := s.Edit("T-1", revA, body("C-first", "l12"), "c"); !errors.Is(err, core.ErrConflict) {
		t.Errorf("те же строки: %v, ждали ErrConflict", err)
	}
	after, _ := os.ReadFile(e.file("T-1"))
	if !bytes.Equal(got, after) || e.count() != n {
		t.Error("конфликт изменил файл или историю")
	}
	// без изменения тела - без коммита
	_, cur, _ := s.Get("T-1")
	tk, _, _ := s.Get("T-1")
	if _, err := s.Edit("T-1", cur, tk.Body, "a"); err != nil {
		t.Fatal(err)
	}
	if e.count() != n {
		t.Error("edit без изменений создал коммит")
	}
	// заголовок комментариев в теле
	if _, err := s.Edit("T-1", cur, "x\n"+commentsHeading+"\ny\n", "a"); err == nil {
		t.Error("заголовок комментариев в теле принят")
	}
	// база не из git
	if _, err := s.Edit("T-1", strings.Repeat("ab", 20), "x\n", "a"); err == nil {
		t.Error("база не из git принята")
	}
}

func TestStore_LockBusy_ErrLocked(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(100 * time.Millisecond)
	mustCreate(t, s, "Alpha")
	lock, err := os.OpenFile(filepath.Join(e.root, ".tracker", "lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	n := e.count()
	start := time.Now()
	if _, err := s.Comment("T-1", "x", "a"); !errors.Is(err, core.ErrLocked) {
		t.Errorf("лок занят: %v, ждали ErrLocked", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("ожидание лока не ограничено LockTimeout")
	}
	if e.count() != n {
		t.Error("отказ по локу создал коммит")
	}
}

func TestStore_CommitFails_WarningNotError(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	if err := os.WriteFile(filepath.Join(e.fake, "failcommit"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := s.Comment("T-1", "без коммита", "a")
	if err != nil {
		t.Fatalf("упавший коммит должен дать Warning, а не ошибку: %v", err)
	}
	if r.Warning == "" {
		t.Error("Result.Warning пуст при упавшем коммите")
	}
	if got, _ := os.ReadFile(e.file("T-1")); !strings.Contains(string(got), "без коммита") {
		t.Error("правка не в файле")
	}
	if err := os.Remove(filepath.Join(e.fake, "failcommit")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Note("T-1", "дальше", "a"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.git("log", "--format=%s"), "T-1: external edit") {
		t.Errorf("записанное не закоммичено как external edit:\n%s", e.git("log", "--format=%s"))
	}
}

func TestStore_StaleIndexLock_DroppedWithWarn(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	idx := filepath.Join(e.root, ".git", "index.lock")
	if err := os.WriteFile(idx, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(idx, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Comment("T-1", "x", "a"); err != nil {
		t.Fatalf("Comment при index.lock: %v", err)
	}
	if _, err := os.Stat(idx); err == nil {
		t.Error("index.lock не удалён")
	}
	if l := e.log.String(); !strings.Contains(l, "level=WARN") || !strings.Contains(l, "op=drop_index_lock") {
		t.Errorf("в логе нет `level=WARN ... op=drop_index_lock`:\n%s", l)
	}
}

func TestStore_List_NoGit_BrokenFileReported(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	mustCreate(t, s, "Beta")
	broken := filepath.Join(e.root, "T-9-broken.md")
	if err := os.WriteFile(broken, []byte("---\nid: T-9\ntitle: a: b\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.fake, "calls.log")); err != nil {
		t.Fatal(err)
	}
	tasks, errs := s.List()
	if len(tasks) != 2 || len(errs) != 1 {
		t.Fatalf("List: %d задач, %d ошибок; ждали 2 и 1", len(tasks), len(errs))
	}
	if !strings.Contains(errs[0].Error(), "T-9-broken.md:") {
		t.Errorf("ошибка %q без `путь:строка`", errs[0])
	}
	if c := e.calls(); c != "" {
		t.Errorf("List вызвал git:\n%s", c)
	}
}

func TestStore_NoPartialWritesInTree(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	if _, err := s.Comment("T-1", "x", "a"); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Walk(e.root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if err == nil && !info.IsDir() && strings.Contains(info.Name(), ".tmp") {
			t.Errorf("остался временный файл %s", p)
		}
		return nil
	})
}

func TestStore_FreshIndexLock_BusyNothingWritten(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	idx := filepath.Join(e.root, ".git", "index.lock")
	if err := os.WriteFile(idx, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(e.file("T-1"))
	_, err := s.Comment("T-1", "x", "a")
	if !errors.Is(err, core.ErrLocked) || err.Error() != "git is busy" {
		t.Fatalf("Comment при свежем lock: %v, ждали `git is busy` (ErrLocked)", err)
	}
	if _, err := os.Stat(idx); err != nil {
		t.Errorf("свежий lock удалён: %v", err)
	}
	if after, _ := os.ReadFile(e.file("T-1")); !bytes.Equal(before, after) {
		t.Error("отказ git is busy изменил файл задачи")
	}
}

// Шаг 4 из 2.1: файл изменился между чтением и rename - правка мимо ядра не теряется,
// операция повторяется с external edit. Краснеет, если убрать сверку в replace.
func TestStore_ExternalEditBetweenReadAndRename_NotLost(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	const outside = "\nправка мимо ядра\n"
	calls := 0
	core.SetAfterRead(s, func(path string, attempt int) {
		calls++
		if attempt == 0 {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			if _, err := f.WriteString(outside); err != nil {
				t.Error(err)
			}
		}
	})
	if _, err := s.Comment("T-1", "наш комментарий", "agent:1"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	got, _ := os.ReadFile(e.file("T-1"))
	for _, want := range []string{"правка мимо ядра", "наш комментарий"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("в файле нет %q:\n%s", want, got)
		}
	}
	if calls != 2 {
		t.Errorf("попыток %d, ждали 2 (повтор после правки мимо ядра)", calls)
	}
	if !strings.Contains(e.git("log", "--format=%s"), "T-1: external edit") {
		t.Errorf("правка мимо ядра не закоммичена отдельно:\n%s", e.git("log", "--format=%s"))
	}
	if st := strings.TrimSpace(e.git("status", "--porcelain")); st != "" {
		t.Errorf("дерево не чистое:\n%s", st)
	}
}

// Файл меняется на каждой из трёх попыток: ошибка, файл не тронут нами, temp не остался.
func TestStore_FileChangesEveryAttempt_ErrorAfterThree(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	calls := 0
	core.SetAfterRead(s, func(path string, attempt int) {
		calls++
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		_, _ = f.WriteString("\nвнешняя правка " + string(rune('A'+attempt)) + "\n")
	})
	_, err := s.Comment("T-1", "не должен записаться", "agent:1")
	if err == nil || !strings.Contains(err.Error(), "file changes during write") {
		t.Fatalf("Comment: %v, ждали ошибку `file changes during write`", err)
	}
	if calls != 3 {
		t.Errorf("попыток %d, ждали 3", calls)
	}
	got, _ := os.ReadFile(e.file("T-1"))
	if strings.Contains(string(got), "не должен записаться") || !strings.Contains(string(got), "внешняя правка C") {
		t.Errorf("файл после отказа:\n%s", got)
	}
	_ = filepath.Walk(e.root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if err == nil && !info.IsDir() && strings.Contains(info.Name(), ".tmp") {
			t.Errorf("остался временный файл %s", p)
		}
		return nil
	})
}
