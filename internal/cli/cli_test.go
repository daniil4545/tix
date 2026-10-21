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

	"github.com/daniil4545/tix/internal/cli"
)

// UI and format texts: edit them here when the language changes.
const (
	msgNoTasks      = "no tasks"
	personalProject = "personal"
	commentsHeading = "## Comments"
)

// anyInput: the exit code table does not set a code for these inputs - 1 and 6 are both acceptable.
const anyInput = -1

var gitBin string

func codeMatches(got, want int) bool {
	if want == anyInput {
		return got == 1 || got == 6
	}
	return got == want
}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tix-cli-")
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

// guard turns a stub panic into the failure of one test.
func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

type box struct {
	t    *testing.T
	home string
	cwd  string
	root string
	// extra - environment variables beyond the base ones (agent identity and so on).
	extra map[string]string
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
		"TIX_HOME":         b.home,
		"TIX_GIT":          gitBin,
		"TIX_LOCK_TIMEOUT": "2s",
		"HOME":             os.Getenv("HOME"),
	}
	for k, v := range b.extra {
		vars[k] = v
	}
	code := cli.Run(context.Background(), args, cli.Env{
		Stdin: strings.NewReader(stdin), Stdout: &o, Stderr: &e, Cwd: cwd,
		Getenv: func(k string) string { return vars[k] },
	})
	return res{o.String(), e.String(), code}
}

func (b *box) tix(stdin string, args ...string) res {
	b.t.Helper()
	return b.runIn(b.cwd, stdin, args...)
}

func (b *box) ok(stdin string, args ...string) res {
	b.t.Helper()
	r := b.tix(stdin, args...)
	if r.code != 0 {
		b.t.Fatalf("tix %v: code %d, stdout %q, stderr %q", args, r.code, r.out, r.err)
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
		b.t.Fatalf("file %s: found %d (%v)", id, len(m), m)
	}
	return m[0]
}

// snapshot - the store files without .git, the log and the lock, plus HEAD if a repository exists.
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
		if rel == ".tix/log" || rel == ".tix/lock" || strings.HasPrefix(rel, ".tix/tmp") {
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
	r := b.tix("", "list")
	if r.code != 0 || r.out != msgNoTasks+"\n" || r.err != "" {
		t.Fatalf("list in an empty store: code %d, stdout %q, stderr %q, want %q", r.code, r.out, r.err, msgNoTasks)
	}
	if _, err := os.Stat(filepath.Join(b.home, ".git")); err == nil {
		t.Error("list created a repository: a read must not write")
	}
	r = b.tix("", "new", "First")
	if r.code != 0 || r.out != "ok T-1 idea\n" {
		t.Fatalf("new: code %d, stdout %q, want `ok T-1 idea`; stderr %q", r.code, r.out, r.err)
	}
	if _, err := os.Stat(filepath.Join(b.home, ".git")); err != nil {
		t.Errorf("new did not create a repository in ~/tasks: %v", err)
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
		t.Fatalf("list: %d lines, want 4: %q", len(lines), lines)
	}
	re := regexp.MustCompile(`^(\w+) (T-\d+) (.+?)( \|.*)?$`)
	var order []string
	for _, l := range lines {
		m := re.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("list line %q does not match the format `<column> T-N <title> | <summary>`", l)
		}
		order = append(order, m[1]+" "+m[2])
	}
	want := "idea T-2,idea T-3,idea T-4,todo T-1"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("list order: %s, want by columns and rank: %s", got, want)
	}
	if got := strings.TrimSpace(b.ok("", "list", "--status", "todo").out); !strings.HasPrefix(got, "todo T-1 Alpha") || strings.Contains(got, "\n") {
		t.Errorf("list --status todo: %q", got)
	}
	if got := strings.TrimSpace(b.ok("", "list", "--project", "other").out); !strings.Contains(got, "T-4 Delta") || strings.Contains(got, "\n") {
		t.Errorf("list --project other: %q", got)
	}

	var js []map[string]any
	if err := json.Unmarshal([]byte(b.ok("", "list", "--json").out), &js); err != nil {
		t.Fatalf("list --json not JSON: %v", err)
	}
	if len(js) != 4 {
		t.Fatalf("list --json: %d tasks, want 4", len(js))
	}
	for _, k := range []string{"body", "comments"} {
		if _, has := js[0][k]; has {
			t.Errorf("list --json contains %q: Task is without body and comments", k)
		}
	}
	for _, k := range []string{"id", "title", "status", "project", "rank", "rev", "path"} {
		if _, has := js[0][k]; !has {
			t.Errorf("list --json without field %q: %v", k, js[0])
		}
	}
}

func TestCLI_Show_RevThenFileAsIs_JSON(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "comment", "T-1", "a comment")
	file, _ := os.ReadFile(b.taskFile("T-1"))

	r := b.ok("", "show", "T-1")
	first, rest, _ := strings.Cut(r.out, "\n")
	blob := strings.TrimSpace(b.git("hash-object", b.taskFile("T-1")))
	if first != "rev "+blob {
		t.Errorf("first line of show %q, want `rev %s` (git blob id of the file)", first, blob)
	}
	if rest != string(file) {
		t.Errorf("show does not print the file as is:\n%q\nfile:\n%q", rest, file)
	}

	var s map[string]any
	if err := json.Unmarshal([]byte(b.ok("", "show", "T-1", "--json").out), &s); err != nil {
		t.Fatalf("show --json not JSON: %v", err)
	}
	if s["id"] != "T-1" || s["title"] != "Alpha" || s["rev"] != blob {
		t.Errorf("show --json: id=%v title=%v rev=%v, want T-1, Alpha, %s", s["id"], s["title"], s["rev"], blob)
	}
	if cs, _ := s["comments"].([]any); len(cs) != 1 {
		t.Errorf("show --json: comments %v, want 1", s["comments"])
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
		{"edit", "body\n", []string{"edit", "T-1", "--base", rev}, "ok T-1 edit"},
		{"comment", "", []string{"comment", "T-1", "text"}, "ok T-1 comment"},
		{"note", "", []string{"note", "T-1", "step"}, "ok T-1 note"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			plain := b.ok(c.stdin, append(append([]string(nil), c.args...), "--as", "cli:1")...)
			if c.name == "edit" {
				// a second edit with the same body - no change, but the same reply
				if r := b.ok(c.stdin, append(append([]string(nil), c.args...), "--as", "cli:1")...); r.out != plain.out {
					t.Errorf("repeated edit: %q, want %q", r.out, plain.out)
				}
			}
			if plain.out != c.want+"\n" || plain.err != "" {
				t.Errorf("reply %q (stderr %q), want %q", plain.out, plain.err, c.want)
			}
			if c.name == "edit" {
				return
			}
			v := b.ok(c.stdin, append(append([]string(nil), c.args...), "-v")...)
			head := strings.TrimSpace(b.git("rev-parse", "HEAD"))
			if !strings.HasPrefix(v.out, c.want+" ") || !strings.Contains(v.out, head[:7]) || strings.Count(v.out, "\n") != 1 {
				t.Errorf("-v: %q, want `%s <commit hash %s>` on one line", v.out, c.want, head[:7])
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
	b.ok("", "comment", "T-1", "dup")
	b.ok("", "comment", "T-1", "dup")
	var s struct{ Comments []any }
	_ = json.Unmarshal([]byte(b.ok("", "show", "T-1", "--json").out), &s)
	if len(s.Comments) != 2 {
		t.Errorf("repeated comment: %d comments, want 2", len(s.Comments))
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
	b.ok("", "new", "Personal") // outside git: project personal
	if r := b.runIn(sub, "", "new", "Inside"); r.code != 0 {
		t.Fatalf("new in a project: %+v", r)
	}
	proj1 := func(id string) string {
		var s struct{ Project string }
		_ = json.Unmarshal([]byte(b.ok("", "show", id, "--json").out), &s)
		return s.Project
	}
	if p := proj1("T-1"); p != personalProject {
		t.Errorf("new outside git: project %q, want personal", p)
	}
	if p := proj1("T-2"); p != "myproj" {
		t.Errorf("new in folder myproj/pkg/deep: project %q, want myproj", p)
	}
	if got := b.runIn(sub, "", "list").out; !strings.Contains(got, "T-2") || strings.Contains(got, "T-1") {
		t.Errorf("list from a project must show only its tasks: %q", got)
	}
	if got := b.tix("", "list").out; !strings.Contains(got, "T-2") || !strings.Contains(got, "T-1") {
		t.Errorf("list outside a project shows all tasks: %q", got)
	}
	if got := b.runIn(sub, "", "list", "--project", personalProject).out; !strings.Contains(got, "T-1") || strings.Contains(got, "T-2") {
		t.Errorf("--project overrides the cwd project: %q", got)
	}
}

func TestCLI_InputErrors_ErrOneLine_FilesIntact(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	rev := strings.TrimPrefix(strings.SplitN(b.ok("", "show", "T-1").out, "\n", 2)[0], "rev ")
	// Exit codes: 4 no task, 6 input (flag, id), 1 other.
	cases := []struct {
		name  string
		stdin string
		args  []string
		want  int
	}{
		{"show no task", "", []string{"show", "T-99"}, 4},
		{"comment no task", "", []string{"comment", "T-99", "x"}, 4},
		{"rm no task", "", []string{"rm", "T-99"}, 4},
		{"new unknown column", "", []string{"new", "X", "--to", "nosuch"}, anyInput},
		{"edit with comments heading", "text\n" + commentsHeading + "\nforged\n", []string{"edit", "T-1", "--base", rev}, anyInput},
		{"edit empty stdin", "", []string{"edit", "T-1", "--base", rev}, anyInput},
		{"as with pipe", "", []string{"comment", "T-1", "x", "--as", "a|b"}, 6},
		{"as with arrow", "", []string{"comment", "T-1", "x", "--as", "a-->b"}, 6},
		{"as with newline", "", []string{"comment", "T-1", "x", "--as", "a\nb"}, 6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			before := b.snapshot()
			r := b.tix(c.stdin, c.args...)
			if !codeMatches(r.code, c.want) {
				t.Errorf("code %d, want %d (anyInput: 1 or 6); stdout %q stderr %q", r.code, c.want, r.out, r.err)
			}
			if r.out != "" {
				t.Errorf("on error stdout must be empty: %q", r.out)
			}
			if !oneErrLine(r.err) {
				t.Errorf("stderr %q, want one line `err ...`", r.err)
			}
			if after := b.snapshot(); after != before {
				t.Errorf("the error changed the store:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
	if r := b.tix("", "show", "T-99"); !strings.HasPrefix(r.err, "err T-99: ") {
		t.Errorf("show T-99: stderr %q, want prefix `err T-99: `", r.err)
	}
}

func TestCLI_BrokenTaskFile_WarnOnListNoOverwriteOnWrite(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	broken := filepath.Join(b.home, "T-3-broken.md")
	data := "---\nid: T-3\ntitle: a: b\n---\nbody\n"
	if err := os.WriteFile(broken, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	r := b.tix("", "list")
	if r.code != 0 {
		t.Errorf("list with a broken file: code %d, want 0", r.code)
	}
	if !strings.Contains(r.out, "T-1 Alpha") {
		t.Errorf("list did not show the whole task: %q", r.out)
	}
	if !regexp.MustCompile(`T-3-broken\.md:\d+`).MatchString(r.err) {
		t.Errorf("list: stderr %q, want a warn with `path:line` (T-3-broken.md:N)", r.err)
	}
	r = b.tix("", "comment", "T-3", "x")
	if r.code != 1 || !regexp.MustCompile(`T-3-broken\.md:\d+`).MatchString(r.err) {
		t.Errorf("comment on a broken file: code %d, stderr %q, want err with path and line", r.code, r.err)
	}
	if got, _ := os.ReadFile(broken); string(got) != data {
		t.Errorf("broken file changed:\n%q", got)
	}
}

func TestCLI_BrokenConfig_ErrPathLine_NothingWritten(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	cfg := filepath.Join(b.home, ".tix", "config.toml")
	data := "[[column]\nname = \"idea\"\n"
	if err := os.WriteFile(cfg, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	before := b.snapshot()
	for _, args := range [][]string{{"new", "Beta"}, {"comment", "T-1", "x"}, {"list"}} {
		r := b.tix("", args...)
		if r.code != 1 || !regexp.MustCompile(`config\.toml:\d+`).MatchString(r.err) {
			t.Errorf("tix %v: code %d, stderr %q, want err with `config.toml:N`", args, r.code, r.err)
		}
	}
	if after := b.snapshot(); after != before {
		t.Errorf("broken config: store changed:\n%s\nafter:\n%s", before, after)
	}
}

// new without --to puts the task into the inbox role column, whatever its name and place
// in the config. Turns red if the column is looked up by name or the first one is taken.
func TestCLI_New_DefaultColumnByInboxRole(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	cfg := filepath.Join(b.home, ".tix", "config.toml")
	data := "[[column]]\nname = \"todo\"\nrole = \"ready\"\n\n[[column]]\nname = \"ideas\"\nrole = \"inbox\"\n"
	if err := os.WriteFile(cfg, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := b.ok("", "new", "Beta"); r.out != "ok T-2 ideas\n" {
		t.Errorf("tix new Beta: stdout %q, want %q", r.out, "ok T-2 ideas\n")
	}
}
