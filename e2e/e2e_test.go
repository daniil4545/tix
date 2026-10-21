// Package e2e проверяет бинарь tix как чёрный ящик: процессы, лок, git, kill -9.
// Сценарии: docs/plans/z1-core.md, раздел 3; docs/spec.md, 2.1 и 7.
// Допущение по CLI (раздел 5 плана, макет): флаги --as, -v, --json идут после позиционных.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Текст интерфейса: при смене языка правится здесь.
const warnCommitFailed = "warn T-1: commit not recorded"

var (
	tixBin string
	gitBin string
)

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "tix-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer os.RemoveAll(dir)
	root, _ := filepath.Abs("..")
	tixBin = filepath.Join(dir, "tix")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", tixBin, "./cmd/tix")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build tix: %v\n%s\n", err, out)
		return 2
	}
	gitBin, err = exec.LookPath("git")
	if err != nil {
		fmt.Fprintln(os.Stderr, "git not found")
		return 2
	}
	return m.Run()
}

type box struct {
	t    *testing.T
	root string // временный корень теста
	home string // TIX_HOME
	cwd  string // не git-папка
	env  []string
	// stale - rev, оставшийся позади файла (для сценариев слияния).
	stale string
}

func newBox(t *testing.T) *box {
	t.Helper()
	root := t.TempDir()
	b := &box{t: t, root: root, home: filepath.Join(root, "tasks"), cwd: filepath.Join(root, "work")}
	for _, d := range []string{b.cwd, filepath.Join(root, "home")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b.env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + filepath.Join(root, "home"),
		"TIX_HOME=" + b.home,
		"TIX_GIT=" + gitBin,
		"TIX_LOCK_TIMEOUT=30s",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	}
	return b
}

// with возвращает окружение с заменой или добавлением переменной.
func (b *box) with(kv ...string) []string {
	out := append([]string(nil), b.env...)
	for _, p := range kv {
		key := p[:strings.Index(p, "=")+1]
		replaced := false
		for i, e := range out {
			if strings.HasPrefix(e, key) {
				out[i] = p
				replaced = true
			}
		}
		if !replaced {
			out = append(out, p)
		}
	}
	return out
}

type res struct {
	out, err string
	code     int
}

func (b *box) cmd(env []string, stdin string, args ...string) *exec.Cmd {
	c := exec.CommandContext(context.Background(), tixBin, args...)
	c.Dir = b.cwd
	c.Env = env
	c.Stdin = strings.NewReader(stdin)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return c
}

func (b *box) runEnv(env []string, stdin string, args ...string) res {
	b.t.Helper()
	c := b.cmd(env, stdin, args...)
	var o, e bytes.Buffer
	c.Stdout, c.Stderr = &o, &e
	if err := c.Start(); err != nil {
		b.t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		b.t.Fatalf("tix %v завис дольше 90 с", args)
	}
	return res{o.String(), e.String(), c.ProcessState.ExitCode()}
}

func (b *box) tix(stdin string, args ...string) res {
	b.t.Helper()
	return b.runEnv(b.env, stdin, args...)
}

func (b *box) ok(stdin string, args ...string) res {
	b.t.Helper()
	r := b.tix(stdin, args...)
	if r.code != 0 {
		b.t.Fatalf("tix %v: код %d, stdout %q, stderr %q", args, r.code, r.out, r.err)
	}
	return r
}

func (b *box) git(args ...string) string {
	b.t.Helper()
	c := exec.CommandContext(context.Background(), gitBin, append([]string{"-C", b.home}, args...)...)
	c.Env = b.env
	out, err := c.CombinedOutput()
	if err != nil {
		b.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

type commit struct{ Subject, Actor, Message string }

// commits возвращает коммиты от старых к новым.
func (b *box) commits() []commit {
	b.t.Helper()
	raw := b.git("log", "--reverse", "--format=%s%x1f%(trailers:key=Actor,valueonly,unfold)%x1f%B%x1e")
	var out []commit
	for _, rec := range strings.Split(raw, "\x1e") {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		p := strings.SplitN(rec, "\x1f", 3)
		c := commit{Subject: p[0]}
		if len(p) == 3 {
			c.Actor = strings.TrimSpace(p[1])
			c.Message = p[2]
		}
		out = append(out, c)
	}
	return out
}

// hasSubject ищет строку сообщения: external edit по спеке 2.1 - строки в сообщении, по одной на id или файл.
func (b *box) hasSubject(prefix string) (commit, bool) {
	for _, c := range b.commits() {
		for _, line := range strings.Split(c.Message, "\n") {
			if strings.HasPrefix(line, prefix) {
				return c, true
			}
		}
	}
	return commit{}, false
}

// taskFile возвращает путь файла T-N: T-N-слаг.md или "T-N слаг.md".
func (b *box) taskFile(id string) string {
	b.t.Helper()
	all, _ := filepath.Glob(filepath.Join(b.home, id+"*.md"))
	var m []string
	for _, f := range all {
		if n := filepath.Base(f); strings.HasPrefix(n, id+" ") || strings.HasPrefix(n, id+"-") {
			m = append(m, f)
		}
	}
	if len(m) != 1 {
		b.t.Fatalf("файл %s: найдено %d (%v)", id, len(m), m)
	}
	return m[0]
}

func (b *box) read(path string) string {
	b.t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		b.t.Fatal(err)
	}
	return string(d)
}

func (b *box) write(path, data string) {
	b.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

// snapshot - содержимое базы без .git и служебных файлов лога и лока.
func (b *box) snapshot() map[string]string {
	b.t.Helper()
	snap := map[string]string{}
	_ = filepath.Walk(b.home, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(b.home, p)
		if info.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == ".tix/log" || rel == ".tix/lock" || strings.HasPrefix(rel, ".tix/tmp") {
			return nil
		}
		d, _ := os.ReadFile(p)
		snap[rel] = string(d)
		return nil
	})
	snap["HEAD"] = strings.TrimSpace(b.git("rev-parse", "HEAD"))
	return snap
}

// noTmpLeft: временных файлов нет. После kill -9 остаток в .tix/tmp (в .gitignore) допустим: strictTmp=false.
func (b *box) noTmpLeft(strictTmp bool) {
	b.t.Helper()
	_ = filepath.Walk(b.home, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(b.home, p)
		if info.IsDir() && rel == ".git" {
			return filepath.SkipDir
		}
		if !strictTmp && strings.HasPrefix(rel, ".tix/tmp") {
			return nil
		}
		if !info.IsDir() && (strings.Contains(info.Name(), ".tmp") || strings.HasSuffix(info.Name(), "~")) {
			b.t.Errorf("остался временный файл: %s", rel)
		}
		if !info.IsDir() && strings.HasPrefix(rel, ".tix/tmp/") {
			b.t.Errorf("остался файл в .tix/tmp: %s", rel)
		}
		return nil
	})
}

func (b *box) fsck() {
	b.t.Helper()
	c := exec.CommandContext(context.Background(), gitBin, "-C", b.home, "fsck", "--strict")
	c.Env = b.env
	raw, err := c.CombinedOutput()
	out := string(raw)
	if err != nil {
		b.t.Errorf("git fsck: %v\n%s", err, out)
	}
	if regexp.MustCompile(`(?i)error|missing|broken|corrupt`).MatchString(out) {
		b.t.Errorf("git fsck не чист:\n%s", out)
	}
}

func (b *box) showRev(id string) string {
	b.t.Helper()
	r := b.ok("", "show", id)
	first := strings.SplitN(r.out, "\n", 2)[0]
	if !strings.HasPrefix(first, "rev ") {
		b.t.Fatalf("show %s: первая строка %q, ждали `rev <blob>`", id, first)
	}
	return strings.TrimPrefix(first, "rev ")
}

type showJSON struct {
	ID       string `json:"id"`
	Body     string `json:"body"`
	Comments []struct {
		Text string `json:"text"`
	} `json:"comments"`
}

func (b *box) showJSON(id string) showJSON {
	b.t.Helper()
	var s showJSON
	r := b.ok("", "show", id, "--json")
	if err := json.Unmarshal([]byte(r.out), &s); err != nil {
		b.t.Fatalf("show --json не JSON: %v\n%s", err, r.out)
	}
	return s
}

// fakeGit ставит обёртку git: режим берётся из файла mode рядом: pass; fail и hang - на
// commit (hang оставляет .git/index.lock, как убитый git); failon:<подкоманда> - отказ
// с текстом boom-secret на любой подкоманде.
func (b *box) fakeGit() (script, dir string) {
	b.t.Helper()
	dir = filepath.Join(b.root, "fakegit")
	script = filepath.Join(dir, "git")
	b.write(script, "#!/bin/sh\nD=$(dirname \"$0\")\nMODE=pass\n[ -f \"$D/mode\" ] && MODE=$(cat \"$D/mode\")\n"+
		"for a in \"$@\"; do\n  case \"$MODE\" in failon:*)\n"+
		"    [ \"$a\" = \"${MODE#failon:}\" ] && { echo 'fatal: boom-secret' >&2; exit 128; };;\n  esac\n"+
		"  if [ \"$a\" = commit ]; then\n"+
		"    case \"$MODE\" in\n      fail) echo 'fatal: boom' >&2; exit 128;;\n"+
		"      hang) touch .git/index.lock; touch \"$D/marker\"; exec sleep 600;;\n    esac\n  fi\ndone\nexec "+gitBin+" \"$@\"\n")
	if err := os.Chmod(script, 0o755); err != nil {
		b.t.Fatal(err)
	}
	return script, dir
}

// ageLocks состаривает lock-файлы git: висящий lock старше порога снимается.
// Проверяется восстановление, а не ожидание 10 секунд.
func (b *box) ageLocks() []string {
	b.t.Helper()
	old := time.Now().Add(-time.Minute)
	var found []string
	_ = filepath.WalkDir(filepath.Join(b.home, ".git"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".lock") {
			if err := os.Chtimes(p, old, old); err != nil {
				b.t.Fatal(err)
			}
			found = append(found, p)
		}
		return nil
	})
	return found
}

func waitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("файл %s не появился за 20 с", path)
}

func TestScenario_Main_CommitPerOperation(t *testing.T) {
	b := newBox(t)
	if r := b.ok("", "new", "Alpha", "--as", "test:1"); r.out != "ok T-1 idea\n" {
		t.Fatalf("new: ответ %q, ждали %q", r.out, "ok T-1 idea\n")
	}
	rev := b.showRev("T-1")
	b.ok("Новое тело\n", "edit", "T-1", "--base", rev, "--as", "test:1")
	b.ok("", "comment", "T-1", "первый", "--as", "test:1")
	b.ok("", "comment", "T-1", "второй", "--as", "test:1")
	b.ok("", "note", "T-1", "шаг сделан", "--as", "test:1")
	file := b.taskFile("T-1")
	b.ok("", "rm", "T-1", "--as", "test:1")

	got := b.commits()
	want := []string{"T-1: ", "T-1: edit", "T-1: comment", "T-1: comment", "T-1: note", "T-1: rm"}
	if len(got) != len(want) {
		t.Fatalf("коммитов %d, ждали %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if !strings.HasPrefix(got[i].Subject, w) {
			t.Errorf("коммит %d: тема %q, ждали префикс %q", i, got[i].Subject, w)
		}
		if got[i].Actor != "test:1" {
			t.Errorf("коммит %d: Actor %q, ждали test:1", i, got[i].Actor)
		}
	}
	first := strings.Fields(b.git("log", "--reverse", "--format=%H", "-1", "--max-parents=0"))[0]
	files := b.git("show", "--name-only", "--format=", first)
	for _, f := range []string{".gitignore", ".tix/config.toml", ".tix/next_id", filepath.Base(file)} {
		if !strings.Contains(files, f) {
			t.Errorf("первый коммит без %s; файлы:\n%s", f, files)
		}
	}
	if note := strings.TrimSpace(b.git("show", "--name-only", "--format=", "HEAD~1")); note != "" {
		t.Errorf("note должен быть пустым коммитом, файлы: %q", note)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("rm: файл остался")
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("после rm дерево не чистое:\n%s", st)
	}
	ign := b.read(filepath.Join(b.home, ".gitignore"))
	for _, p := range []string{".tix/lock", ".tix/log", ".tix/tmp", ".obsidian/workspace", ".trash/", "*.swp"} {
		if !strings.Contains(ign, p) {
			t.Errorf(".gitignore без %q:\n%s", p, ign)
		}
	}
}

func TestScenario_TwoAgents_Race5x20(t *testing.T) {
	if testing.Short() {
		t.Skip("slow tier: гонка 5x20")
	}
	b := newBox(t)
	b.ok("", "new", "Race")
	var wg sync.WaitGroup
	var mu sync.Mutex
	var fails []string
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				r := b.tix("", "comment", "T-1", fmt.Sprintf("w%d-i%d", w, i), "--as", fmt.Sprintf("agent:%d", w))
				if r.code != 0 {
					mu.Lock()
					fails = append(fails, fmt.Sprintf("w%d i%d: exit %d %q", w, i, r.code, r.err))
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()
	if len(fails) > 0 {
		t.Fatalf("отказов %d, первые: %v", len(fails), fails[:min(3, len(fails))])
	}
	s := b.showJSON("T-1")
	if len(s.Comments) != 100 {
		t.Fatalf("комментариев %d, ждали 100", len(s.Comments))
	}
	seen := map[string]bool{}
	for _, c := range s.Comments {
		seen[strings.TrimSpace(c.Text)] = true
	}
	if len(seen) != 100 {
		t.Errorf("уникальных текстов %d, ждали 100 (потеря или дубль)", len(seen))
	}
	n := 0
	for _, c := range b.commits() {
		if strings.HasPrefix(c.Subject, "T-1: comment") {
			n++
		}
	}
	if n != 100 {
		t.Errorf("коммитов comment %d, ждали 100", n)
	}
	if total := len(b.commits()); total != 101 {
		t.Errorf("всего коммитов %d, ждали 101 (new + 100)", total)
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("дерево не чистое после гонки:\n%s", st)
	}
	b.noTmpLeft(true)
}

func TestScenario_EditStaleBase_MergeAndConflict(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	var lines []string
	for i := 1; i <= 12; i++ {
		lines = append(lines, fmt.Sprintf("l%02d", i))
	}
	body := func(first, last string) string {
		l := append([]string(nil), lines...)
		l[0], l[9] = first, last
		return strings.Join(l, "\n") + "\n"
	}
	b.ok(body("l01", "l10"), "edit", "T-1", "--base", b.showRev("T-1"))
	revA := b.showRev("T-1") // агент 1 прочитал и ушёл думать

	b.ok(body("A-first", "l10"), "edit", "T-1", "--base", revA) // агент 2 правит начало
	b.ok("", "comment", "T-1", "между правками")

	// Агент 1 правит конец по старой базе: разные места, слияние.
	b.ok(body("l01", "B-last"), "edit", "T-1", "--base", revA)
	got := b.read(b.taskFile("T-1"))
	for _, want := range []string{"A-first", "B-last", "между правками"} {
		if !strings.Contains(got, want) {
			t.Errorf("после слияния нет %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "l01\n") || strings.Contains(got, "l10\n") {
		t.Errorf("после слияния остались старые строки:\n%s", got)
	}

	// Те же строки: конфликт, отказ, файл цел.
	before := b.snapshot()
	r := b.tix(body("C-first", "l10"), "edit", "T-1", "--base", revA)
	if r.code != 3 {
		t.Fatalf("конфликт: код %d, ждали 3 (ErrConflict); stdout %q", r.code, r.out)
	}
	if !regexp.MustCompile(`^err T-1: .+\n$`).MatchString(r.err) {
		t.Errorf("конфликт: stderr %q, ждали одну строку `err T-1: ...`", r.err)
	}
	if after := b.snapshot(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("конфликт изменил базу:\nдо %v\nпосле %v", before, after)
	}

	// База не из git: просьба перечитать.
	r = b.tix(body("X", "Y"), "edit", "T-1", "--base", strings.Repeat("ab", 20))
	if r.code != 1 || !strings.Contains(r.err, "tix show") {
		t.Errorf("база не из git: код %d, stderr %q, ждали err с `tix show`", r.code, r.err)
	}
}

// Спека 2.1/3: правка тела и комментарий агента - разные части файла, слияние чисто.
// Первый комментарий добавляет раздел сразу после последней строки тела; правка этой строки
// по старой базе вплотную к новому разделу (git merge-file даёт конфликт).
func TestScenario_EditLastBodyLine_FirstCommentAndOtherEdit_MergesClean(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	body := func(first, last string) string {
		var l []string
		for i := 1; i <= 12; i++ {
			l = append(l, fmt.Sprintf("l%02d", i))
		}
		l[0], l[11] = first, last
		return strings.Join(l, "\n") + "\n"
	}
	b.ok(body("l01", "l12"), "edit", "T-1", "--base", b.showRev("T-1"))
	rev := b.showRev("T-1")
	b.ok(body("A-first", "l12"), "edit", "T-1", "--base", rev)
	b.ok("", "comment", "T-1", "агент")
	r := b.tix(body("l01", "B-last"), "edit", "T-1", "--base", rev)
	if r.code != 0 {
		t.Fatalf("правка последней строки по старой базе после первого комментария: код %d, stderr %q", r.code, r.err)
	}
	got := b.read(b.taskFile("T-1"))
	for _, want := range []string{"A-first", "B-last", "агент"} {
		if !strings.Contains(got, want) {
			t.Errorf("слияние потеряло %q:\n%s", want, got)
		}
	}
}

func TestScenario_Repeat_DuplicateCommentButNoOpEdit(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "comment", "T-1", "дубль")
	b.ok("", "comment", "T-1", "дубль")
	if n := len(b.showJSON("T-1").Comments); n != 2 {
		t.Errorf("повторный comment: комментариев %d, ждали 2 (дубль)", n)
	}
	b.ok("Тело\n", "edit", "T-1", "--base", b.showRev("T-1"))
	head := b.git("rev-parse", "HEAD")
	r := b.ok("Тело\n", "edit", "T-1", "--base", b.showRev("T-1"))
	if r.out != "ok T-1 edit\n" {
		t.Errorf("повторный edit: ответ %q", r.out)
	}
	if b.git("rev-parse", "HEAD") != head {
		t.Error("edit без изменения тела создал коммит")
	}
}

func TestScenario_Obsidian_ExternalEdit(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "new", "Beta")
	f2 := b.taskFile("T-2")
	b.write(f2, strings.Replace(b.read(f2), "title: Beta", "title: EDITED Beta", 1))
	b.write(filepath.Join(b.home, "Заметка про идею.md"), "# просто заметка\n")
	b.write(filepath.Join(b.home, "RULES.md"), "правила\n")
	b.write(filepath.Join(b.home, ".obsidian", "workspace.json"), "{}")

	b.ok("", "comment", "T-1", "привет")

	for _, line := range []string{"T-2: external edit", "external edit: Заметка про идею.md", "external edit: RULES.md"} {
		c, found := b.hasSubject(line)
		if !found {
			t.Errorf("нет строки %q в сообщениях коммитов: %+v", line, b.commits())
		} else if c.Actor != "external" {
			t.Errorf("коммит со строкой %q: Actor %q, ждали external", line, c.Actor)
		}
	}
	last := b.commits()
	if !strings.HasPrefix(last[len(last)-1].Subject, "T-1: comment") {
		t.Errorf("последний коммит %q, ждали T-1: comment", last[len(last)-1].Subject)
	}
	if files := strings.Split(strings.TrimSpace(b.git("show", "--name-only", "--format=", "HEAD")), "\n"); len(files) != 1 || !strings.HasPrefix(files[0], "T-1") {
		t.Errorf("в коммит comment ушли чужие файлы: %v", files)
	}
	if ls := b.git("ls-files"); strings.Contains(ls, ".obsidian") {
		t.Errorf(".obsidian/workspace* попал в git:\n%s", ls)
	}
	if !strings.Contains(b.read(f2), "EDITED Beta") {
		t.Error("внешняя правка T-2 потеряна")
	}

	// Возврат удалённого через git checkout - тоже external edit.
	f1 := b.taskFile("T-1")
	b.ok("", "rm", "T-1")
	b.git("checkout", "HEAD~1", "--", filepath.Base(f1))
	b.ok("", "note", "T-2", "после возврата")
	c, found := b.hasSubject("T-1: external edit")
	if !found {
		t.Fatalf("возврат файла не закоммичен как external edit: %+v", b.commits())
	}
	if c.Actor != "external" {
		t.Errorf("Actor %q, ждали external", c.Actor)
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("дерево не чистое:\n%s", st)
	}
}

func TestScenario_NoteCommentDoNotCarryOtherFiles(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "new", "Beta")
	f2 := b.taskFile("T-2")
	b.write(f2, strings.Replace(b.read(f2), "title: Beta", "title: EDITED Beta", 1))
	b.git("add", "-A") // чужая правка уже в индексе
	b.ok("", "note", "T-1", "заметка")
	if files := strings.TrimSpace(b.git("show", "--name-only", "--format=", "HEAD")); files != "" {
		t.Errorf("note не пустой коммит, файлы: %q", files)
	}
	if c, ok := b.hasSubject("T-2: external edit"); !ok || c.Actor != "external" {
		t.Errorf("правка T-2 не оформлена как external edit: %+v", b.commits())
	}
}

func TestScenario_LockBusy_ErrOneLine(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	lock, err := os.OpenFile(filepath.Join(b.home, ".tix", "lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	before := b.snapshot()
	start := time.Now()
	r := b.runEnv(b.with("TIX_LOCK_TIMEOUT=200ms"), "", "comment", "T-1", "busy")
	elapsed := time.Since(start)
	if r.code != 2 || !regexp.MustCompile(`^err .+\n$`).MatchString(r.err) {
		t.Errorf("лок занят: код %d, stderr %q, ждали код 2 (занято) и одну строку err", r.code, r.err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("ожидание %v, ждали около 200 мс", elapsed)
	}
	if fmt.Sprint(b.snapshot()) != fmt.Sprint(before) {
		t.Error("отказ по локу изменил базу")
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	b.ok("", "comment", "T-1", "free")
}

func TestScenario_StaleGitLocks_Dropped(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	var locks []string
	for _, rel := range []string{"index.lock", "HEAD.lock", filepath.Join("refs", "heads", "main.lock")} {
		p := filepath.Join(b.home, ".git", rel)
		b.write(p, "")
		locks = append(locks, p)
	}
	b.ageLocks()
	b.ok("", "comment", "T-1", "after lock")
	for _, p := range locks {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s не удалён", p)
		}
	}
	log := b.read(filepath.Join(b.home, ".tix", "log"))
	if !strings.Contains(log, "WARN") || !strings.Contains(log, "drop_index_lock") {
		t.Errorf("в .tix/log нет WARN op=drop_index_lock:\n%s", log)
	}
}

// Свежий lock - git другого процесса ещё работает: операция его не трогает и не пишет.
func TestScenario_FreshGitLock_BusyNothingWritten(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	lock := filepath.Join(b.home, ".git", "index.lock")
	b.write(lock, "")
	before := b.snapshot()
	r := b.tix("", "comment", "T-1", "busy")
	if r.code != 2 || r.err != "err T-1: git is busy\n" {
		t.Errorf("свежий lock: код %d, stderr %q, ждали код 2 и `err T-1: git is busy`", r.code, r.err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("свежий lock удалён: %v", err)
	}
	if fmt.Sprint(b.snapshot()) != fmt.Sprint(before) {
		t.Error("отказ git is busy изменил базу")
	}
	// Lock состарился: висящий, операция его снимает.
	b.ageLocks()
	b.ok("", "comment", "T-1", "after")
	if _, err := os.Stat(lock); err == nil {
		t.Error("висящий lock не удалён")
	}
}

// Текст stderr git не попадает в ответ ни на одном пути; полный текст - в .tix/log.
func TestScenario_GitStderr_NotInReply(t *testing.T) {
	const secret = "boom-secret"
	body := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\n"
	cases := []struct {
		name, mode string
		run        func(b *box, env []string) res
		wantCode   int
	}{
		{"init", "failon:init", func(b *box, env []string) res { return b.runEnv(env, "", "new", "Alpha") }, 1},
		{"status", "failon:status", func(b *box, env []string) res { return b.runEnv(env, "", "comment", "T-1", "x") }, 1},
		{"commit", "failon:commit", func(b *box, env []string) res { return b.runEnv(env, "", "comment", "T-1", "x") }, 0},
		{"hash-object", "failon:hash-object", func(b *box, env []string) res { return b.runEnv(env, "", "show", "T-1") }, 0},
		{"cat-file", "failon:cat-file", func(b *box, env []string) res {
			return b.runEnv(env, body, "edit", "T-1", "--base", b.stale)
		}, 1},
		{"merge-file", "failon:merge-file", func(b *box, env []string) res {
			return b.runEnv(env, strings.Replace(body, "l9", "mine", 1), "edit", "T-1", "--base", b.stale)
		}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			script, dir := b.fakeGit()
			env := b.with("TIX_GIT=" + script)
			if c.name != "init" {
				b.ok(body, "new", "Alpha")
				b.ok(body, "edit", "T-1", "--base", b.showRev("T-1"))
				b.stale = b.showRev("T-1")
				b.ok("", "comment", "T-1", "other") // файл ушёл от базы: edit пойдёт через слияние
				b.ok(strings.Replace(body, "l1", "first", 1), "edit", "T-1", "--base", b.showRev("T-1"))
			}
			b.write(filepath.Join(dir, "mode"), c.mode)
			r := c.run(b, env)
			if r.code != c.wantCode {
				t.Errorf("код %d, ждали %d; stdout %q, stderr %q", r.code, c.wantCode, r.out, r.err)
			}
			for _, bad := range []string{secret, "fatal", "exit 128"} {
				if strings.Contains(r.err, bad) || strings.Contains(r.out, bad) {
					t.Errorf("в ответе текст git %q: stdout %q, stderr %q", bad, r.out, r.err)
				}
			}
			if log := b.read(filepath.Join(b.home, ".tix", "log")); !strings.Contains(log, secret) {
				t.Errorf("полного текста git нет в .tix/log:\n%s", log)
			}
		})
	}
}

func TestScenario_CommitFails_WarnExitZero(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	script, dir := b.fakeGit()
	b.write(filepath.Join(dir, "mode"), "fail")
	env := b.with("TIX_GIT=" + script)
	r := b.runEnv(env, "", "comment", "T-1", "без коммита")
	if r.code != 0 || r.out != "ok T-1 comment\n" {
		t.Errorf("коммит упал: код %d, stdout %q, ждали код 0 и `ok T-1 comment`", r.code, r.out)
	}
	if !strings.Contains(r.err, warnCommitFailed) {
		t.Errorf("stderr %q, ждали %q", r.err, warnCommitFailed)
	}
	if strings.Contains(r.err, "boom") || strings.Contains(r.err, "fatal") {
		t.Errorf("в stderr вывод git: %q", r.err)
	}
	if !strings.Contains(b.read(b.taskFile("T-1")), "без коммита") {
		t.Error("правка не попала в файл")
	}
	if log := b.read(filepath.Join(b.home, ".tix", "log")); strings.TrimSpace(log) == "" {
		t.Error("упавший коммит не записан в .tix/log")
	}
	// Следующая операция коммитит записанное как external edit.
	b.write(filepath.Join(dir, "mode"), "pass")
	b.runEnv(env, "", "note", "T-1", "дальше")
	if c, ok := b.hasSubject("T-1: external edit"); !ok || c.Actor != "external" {
		t.Errorf("записанное не закоммичено как external edit: %+v", b.commits())
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("дерево не чистое:\n%s", st)
	}
}

func TestScenario_Kill9AtCommit_NoPartialFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("slow tier: kill -9")
	}
	b := newBox(t)
	b.ok("", "new", "Alpha")
	script, dir := b.fakeGit()
	b.write(filepath.Join(dir, "mode"), "hang")
	env := b.with("TIX_GIT=" + script)
	c := b.cmd(env, "", "comment", "T-1", "zzz")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	waitFile(t, filepath.Join(dir, "marker"))
	_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	_ = c.Wait()

	b.noTmpLeft(false)
	b.fsck()
	lock := filepath.Join(b.home, ".git", "index.lock")
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("убитый git не оставил index.lock: %v", err)
	}
	if !strings.Contains(b.read(b.taskFile("T-1")), "zzz") {
		t.Error("записанная правка пропала")
	}
	if r := b.ok("", "list"); r.err != "" {
		t.Errorf("list после kill: stderr %q, файл полузаписан?", r.err)
	}
	b.write(filepath.Join(dir, "mode"), "pass")
	// Пока lock свежий, git мог быть живым: отказ без записи.
	if r := b.tix("", "note", "T-1", "рано"); r.code != 2 || !strings.Contains(r.err, "git is busy") {
		t.Errorf("свежий lock после kill: код %d, stderr %q, ждали git is busy", r.code, r.err)
	}
	b.ageLocks()
	r := b.runEnv(b.with("TIX_LOCK_TIMEOUT=5s"), "", "note", "T-1", "после kill")
	if r.code != 0 {
		t.Fatalf("после kill -9 операция не прошла (лок не снят?): код %d, %q", r.code, r.err)
	}
	if c, ok := b.hasSubject("T-1: external edit"); !ok || c.Actor != "external" {
		t.Errorf("правка не ушла как external edit: %+v", b.commits())
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("дерево не чистое:\n%s", st)
	}
}

func TestScenario_Kill9Random_RepoStaysConsistent(t *testing.T) {
	if testing.Short() {
		t.Skip("slow tier: kill -9 в случайные моменты")
	}
	b := newBox(t)
	b.ok("", "new", "Alpha")
	var finished []string
	for i := 0; i < 60; i++ {
		text := fmt.Sprintf("k%d", i)
		c := b.cmd(b.env, "", "comment", "T-1", text)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(i%12*4) * time.Millisecond) // момент удара: 0-44 мс
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if err := c.Wait(); err == nil {
			finished = append(finished, text)
		}
		b.ageLocks() // убитый git оставил lock: считаем его висящим
	}
	b.noTmpLeft(false)
	b.fsck()
	if r := b.ok("", "list"); r.err != "" {
		t.Errorf("list: stderr %q, файл задачи полузаписан", r.err)
	}
	fin := b.ok("", "note", "T-1", "финал")
	if st := b.git("status", "--porcelain"); st != "" {
		t.Errorf("после финальной операции дерево не чистое:\n%s\nstderr note: %q\ndiff:\n%s\nlog:\n%s", st, fin.err, b.git("diff", "HEAD"), b.read(filepath.Join(b.home, ".tix", "log")))
	}
	have := map[string]bool{}
	for _, c := range b.showJSON("T-1").Comments {
		have[strings.TrimSpace(c.Text)] = true
	}
	for _, f := range finished {
		if !have[f] {
			t.Errorf("команда %s завершилась с кодом 0, но комментария нет", f)
		}
	}
}
