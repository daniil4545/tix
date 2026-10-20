package cli_test

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
	"testing"

	"tracker/internal/cli"
)

// Тексты интерфейса и формата: при смене языка правятся здесь.
const (
	msgNoTasks      = "no tasks"
	personalProject = "personal"
	commentsHeading = "## Comments"
)

var gitBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tk-cli-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	gitBin, err = exec.LookPath("git")
	if err != nil {
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

// guard превращает панику заглушки в провал одного теста.
func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("паника: %v", r)
	}
}

type box struct {
	t    *testing.T
	home string
	cwd  string
	root string
}

func newBox(t *testing.T) *box {
	t.Helper()
	root := t.TempDir()
	b := &box{t: t, root: root, home: filepath.Join(root, "tasks"), cwd: filepath.Join(root, "work")}
	if err := os.MkdirAll(b.cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	return b
}

type res struct {
	out, err string
	code     int
}

func (b *box) runIn(cwd, stdin string, args ...string) res {
	b.t.Helper()
	var o, e bytes.Buffer
	vars := map[string]string{
		"TRACKER_HOME":         b.home,
		"TRACKER_GIT":          gitBin,
		"TRACKER_LOCK_TIMEOUT": "2s",
		"HOME":                 os.Getenv("HOME"),
	}
	code := cli.Run(context.Background(), args, cli.Env{
		Stdin: strings.NewReader(stdin), Stdout: &o, Stderr: &e, Cwd: cwd,
		Getenv: func(k string) string { return vars[k] },
	})
	return res{o.String(), e.String(), code}
}

func (b *box) tk(stdin string, args ...string) res {
	b.t.Helper()
	return b.runIn(b.cwd, stdin, args...)
}

func (b *box) ok(stdin string, args ...string) res {
	b.t.Helper()
	r := b.tk(stdin, args...)
	if r.code != 0 {
		b.t.Fatalf("tk %v: код %d, stdout %q, stderr %q", args, r.code, r.out, r.err)
	}
	return r
}

func (b *box) git(args ...string) string {
	b.t.Helper()
	c := exec.CommandContext(context.Background(), gitBin, append([]string{"-C", b.home}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		b.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

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

// snapshot - файлы базы без .git, лога и лока, плюс HEAD, если репозиторий есть.
func (b *box) snapshot() string {
	var sb strings.Builder
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
		if rel == ".tracker/log" || rel == ".tracker/lock" || strings.HasPrefix(rel, ".tracker/tmp") {
			return nil
		}
		d, _ := os.ReadFile(p)
		fmt.Fprintf(&sb, "%s=%q\n", rel, d)
		return nil
	})
	if _, err := os.Stat(filepath.Join(b.home, ".git")); err == nil {
		sb.WriteString("HEAD=" + strings.TrimSpace(b.git("rev-parse", "HEAD")))
	}
	return sb.String()
}

func oneErrLine(s string) bool { return regexp.MustCompile(`^err( T-\d+)?:? .+\n$`).MatchString(s) }

func TestCLI_EmptyBase_ListOneLine_NewCreatesHome(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	r := b.tk("", "list")
	if r.code != 0 || r.out != msgNoTasks+"\n" || r.err != "" {
		t.Fatalf("list в пустой базе: код %d, stdout %q, stderr %q, ждали %q", r.code, r.out, r.err, msgNoTasks)
	}
	if _, err := os.Stat(filepath.Join(b.home, ".git")); err == nil {
		t.Error("list создал репозиторий: чтение не должно писать")
	}
	r = b.tk("", "new", "Первая")
	if r.code != 0 || r.out != "ok T-1 idea\n" {
		t.Fatalf("new: код %d, stdout %q, ждали `ok T-1 idea`; stderr %q", r.code, r.out, r.err)
	}
	if _, err := os.Stat(filepath.Join(b.home, ".git")); err != nil {
		t.Errorf("new не создал репозиторий в ~/tasks: %v", err)
	}
}

func TestCLI_ListShow_FormatsOrderFilters(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	if r := b.ok("", "new", "Alpha", "--to", "todo"); r.out != "ok T-1 todo\n" {
		t.Errorf("new --to: %q", r.out)
	}
	b.ok("", "new", "Beta")
	b.ok("", "new", "Gamma")
	b.ok("", "new", "Delta", "--project", "other")

	lines := strings.Split(strings.TrimSpace(b.ok("", "list").out), "\n")
	if len(lines) != 4 {
		t.Fatalf("list: %d строк, ждали 4: %q", len(lines), lines)
	}
	re := regexp.MustCompile(`^(\w+) (T-\d+) (.+?)( \|.*)?$`)
	var order []string
	for _, l := range lines {
		m := re.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("строка list %q не по формату `<колонка> T-N <название> | <сводка>`", l)
		}
		order = append(order, m[1]+" "+m[2])
	}
	want := "idea T-2,idea T-3,idea T-4,todo T-1"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("порядок list: %s, ждали по колонкам и rank: %s", got, want)
	}
	if got := strings.TrimSpace(b.ok("", "list", "--status", "todo").out); !strings.HasPrefix(got, "todo T-1 Alpha") || strings.Contains(got, "\n") {
		t.Errorf("list --status todo: %q", got)
	}
	if got := strings.TrimSpace(b.ok("", "list", "--project", "other").out); !strings.Contains(got, "T-4 Delta") || strings.Contains(got, "\n") {
		t.Errorf("list --project other: %q", got)
	}

	var js []map[string]any
	if err := json.Unmarshal([]byte(b.ok("", "list", "--json").out), &js); err != nil {
		t.Fatalf("list --json не JSON: %v", err)
	}
	if len(js) != 4 {
		t.Fatalf("list --json: %d задач, ждали 4", len(js))
	}
	for _, k := range []string{"body", "comments"} {
		if _, has := js[0][k]; has {
			t.Errorf("list --json содержит %q: по плану Task без body и comments", k)
		}
	}
	for _, k := range []string{"id", "title", "status", "project", "rank", "rev", "path"} {
		if _, has := js[0][k]; !has {
			t.Errorf("list --json без поля %q: %v", k, js[0])
		}
	}
}

func TestCLI_Show_RevThenFileAsIs_JSON(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "comment", "T-1", "коммент")
	file, _ := os.ReadFile(b.taskFile("T-1"))

	r := b.ok("", "show", "T-1")
	first, rest, _ := strings.Cut(r.out, "\n")
	blob := strings.TrimSpace(b.git("hash-object", b.taskFile("T-1")))
	if first != "rev "+blob {
		t.Errorf("первая строка show %q, ждали `rev %s` (git blob id файла)", first, blob)
	}
	if rest != string(file) {
		t.Errorf("show печатает файл не как есть:\n%q\nфайл:\n%q", rest, file)
	}

	var s map[string]any
	if err := json.Unmarshal([]byte(b.ok("", "show", "T-1", "--json").out), &s); err != nil {
		t.Fatalf("show --json не JSON: %v", err)
	}
	if s["id"] != "T-1" || s["title"] != "Alpha" || s["rev"] != blob {
		t.Errorf("show --json: id=%v title=%v rev=%v, ждали T-1, Alpha, %s", s["id"], s["title"], s["rev"], blob)
	}
	if cs, _ := s["comments"].([]any); len(cs) != 1 {
		t.Errorf("show --json: comments %v, ждали 1", s["comments"])
	}
}

func TestCLI_WriteReplies_OneLine_VerboseAddsCommitHash(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	rev := strings.TrimPrefix(strings.SplitN(b.ok("", "show", "T-1").out, "\n", 2)[0], "rev ")
	cases := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"edit", "тело\n", []string{"edit", "T-1", "--base", rev}, "ok T-1 edit"},
		{"comment", "", []string{"comment", "T-1", "текст"}, "ok T-1 comment"},
		{"note", "", []string{"note", "T-1", "шаг"}, "ok T-1 note"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			plain := b.ok(c.stdin, append(append([]string(nil), c.args...), "--as", "cli:1")...)
			if c.name == "edit" {
				// второй edit с тем же телом - без изменений, но тот же ответ
				if r := b.ok(c.stdin, append(append([]string(nil), c.args...), "--as", "cli:1")...); r.out != plain.out {
					t.Errorf("повтор edit: %q, ждали %q", r.out, plain.out)
				}
			}
			if plain.out != c.want+"\n" || plain.err != "" {
				t.Errorf("ответ %q (stderr %q), ждали %q", plain.out, plain.err, c.want)
			}
			if c.name == "edit" {
				return
			}
			v := b.ok(c.stdin, append(append([]string(nil), c.args...), "-v")...)
			head := strings.TrimSpace(b.git("rev-parse", "HEAD"))
			if !strings.HasPrefix(v.out, c.want+" ") || !strings.Contains(v.out, head[:7]) || strings.Count(v.out, "\n") != 1 {
				t.Errorf("-v: %q, ждали `%s <хеш коммита %s>` одной строкой", v.out, c.want, head[:7])
			}
		})
	}
	if r := b.ok("", "rm", "T-1"); r.out != "ok T-1 rm\n" {
		t.Errorf("rm: %q", r.out)
	}
}

func TestCLI_Repeat_CommentDuplicates(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "comment", "T-1", "дубль")
	b.ok("", "comment", "T-1", "дубль")
	var s struct{ Comments []any }
	_ = json.Unmarshal([]byte(b.ok("", "show", "T-1", "--json").out), &s)
	if len(s.Comments) != 2 {
		t.Errorf("повторный comment: %d комментариев, ждали 2", len(s.Comments))
	}
}

func TestCLI_Project_FromCwdGitFolder(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	proj := filepath.Join(b.root, "dev", "myproj")
	sub := filepath.Join(proj, "pkg", "deep")
	for _, d := range []string{filepath.Join(proj, ".git"), sub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b.ok("", "new", "Личная") // вне git: проект личное
	if r := b.runIn(sub, "", "new", "Внутри"); r.code != 0 {
		t.Fatalf("new в проекте: %+v", r)
	}
	proj1 := func(id string) string {
		var s struct{ Project string }
		_ = json.Unmarshal([]byte(b.ok("", "show", id, "--json").out), &s)
		return s.Project
	}
	if p := proj1("T-1"); p != personalProject {
		t.Errorf("new вне git: проект %q, ждали личное", p)
	}
	if p := proj1("T-2"); p != "myproj" {
		t.Errorf("new в папке myproj/pkg/deep: проект %q, ждали myproj", p)
	}
	if got := b.runIn(sub, "", "list").out; !strings.Contains(got, "T-2") || strings.Contains(got, "T-1") {
		t.Errorf("list из проекта должен показать только его задачи: %q", got)
	}
	if got := b.tk("", "list").out; !strings.Contains(got, "T-2") || !strings.Contains(got, "T-1") {
		t.Errorf("list вне проекта показывает все задачи: %q", got)
	}
	if got := b.runIn(sub, "", "list", "--project", personalProject).out; !strings.Contains(got, "T-1") || strings.Contains(got, "T-2") {
		t.Errorf("--project перекрывает проект cwd: %q", got)
	}
}

func TestCLI_InputErrors_ErrOneLine_FilesIntact(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	rev := strings.TrimPrefix(strings.SplitN(b.ok("", "show", "T-1").out, "\n", 2)[0], "rev ")
	cases := []struct {
		name  string
		stdin string
		args  []string
	}{
		{"show нет задачи", "", []string{"show", "T-99"}},
		{"comment нет задачи", "", []string{"comment", "T-99", "x"}},
		{"rm нет задачи", "", []string{"rm", "T-99"}},
		{"new неизвестная колонка", "", []string{"new", "X", "--to", "nosuch"}},
		{"edit с заголовком комментариев", "текст\n" + commentsHeading + "\nподдельный\n", []string{"edit", "T-1", "--base", rev}},
		{"edit пустой stdin", "", []string{"edit", "T-1", "--base", rev}},
		{"as с чертой", "", []string{"comment", "T-1", "x", "--as", "a|b"}},
		{"as со стрелкой", "", []string{"comment", "T-1", "x", "--as", "a-->b"}},
		{"as с переводом строки", "", []string{"comment", "T-1", "x", "--as", "a\nb"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			before := b.snapshot()
			r := b.tk(c.stdin, c.args...)
			if r.code != 1 {
				t.Errorf("код %d, ждали 1; stdout %q stderr %q", r.code, r.out, r.err)
			}
			if r.out != "" {
				t.Errorf("при ошибке stdout должен быть пуст: %q", r.out)
			}
			if !oneErrLine(r.err) {
				t.Errorf("stderr %q, ждали одну строку `err ...`", r.err)
			}
			if after := b.snapshot(); after != before {
				t.Errorf("ошибка изменила базу:\nдо:\n%s\nпосле:\n%s", before, after)
			}
		})
	}
	if r := b.tk("", "show", "T-99"); !strings.HasPrefix(r.err, "err T-99: ") {
		t.Errorf("show T-99: stderr %q, ждали префикс `err T-99: `", r.err)
	}
}

func TestCLI_BrokenTaskFile_WarnOnListNoOverwriteOnWrite(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	broken := filepath.Join(b.home, "T-3-broken.md")
	data := "---\nid: T-3\ntitle: a: b\n---\nтело\n"
	if err := os.WriteFile(broken, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	r := b.tk("", "list")
	if r.code != 0 {
		t.Errorf("list с битым файлом: код %d, ждали 0", r.code)
	}
	if !strings.Contains(r.out, "T-1 Alpha") {
		t.Errorf("list не показал целую задачу: %q", r.out)
	}
	if !regexp.MustCompile(`T-3-broken\.md:\d+`).MatchString(r.err) {
		t.Errorf("list: stderr %q, ждали warn с `путь:строка` (T-3-broken.md:N)", r.err)
	}
	r = b.tk("", "comment", "T-3", "x")
	if r.code != 1 || !regexp.MustCompile(`T-3-broken\.md:\d+`).MatchString(r.err) {
		t.Errorf("comment в битый файл: код %d, stderr %q, ждали err с путём и строкой", r.code, r.err)
	}
	if got, _ := os.ReadFile(broken); string(got) != data {
		t.Errorf("битый файл изменён:\n%q", got)
	}
}

func TestCLI_BrokenConfig_ErrPathLine_NothingWritten(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	cfg := filepath.Join(b.home, ".tracker", "config.toml")
	data := "[[column]\nname = \"idea\"\n"
	if err := os.WriteFile(cfg, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	before := b.snapshot()
	for _, args := range [][]string{{"new", "Beta"}, {"comment", "T-1", "x"}, {"list"}} {
		r := b.tk("", args...)
		if r.code != 1 || !regexp.MustCompile(`config\.toml:\d+`).MatchString(r.err) {
			t.Errorf("tk %v: код %d, stderr %q, ждали err с `config.toml:N`", args, r.code, r.err)
		}
	}
	if after := b.snapshot(); after != before {
		t.Errorf("битый конфиг: база изменена:\n%s\nпосле:\n%s", before, after)
	}
}
