// Package e2e tests the tix binary as a black box: processes, lock, git, kill -9.
// Scenarios: docs/spec.md, 2.1 and 7.
// CLI assumption (mockup): the flags --as, -v, --json come after the positional arguments.
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

// Interface text: when the language changes, edit it here.
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
	root string // temporary root of the test
	home string // TIX_HOME
	cwd  string // not a git folder
	env  []string
	// stale - a rev left behind the file (for merge scenarios).
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

// with returns the environment with a variable replaced or added.
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
		b.t.Fatalf("tix %v hung for over 90 s", args)
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
		b.t.Fatalf("tix %v: code %d, stdout %q, stderr %q", args, r.code, r.out, r.err)
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

// commits returns the commits from old to new.
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

// hasSubject looks for a message line: an external edit per spec 2.1 has lines in the message, one per id or file.
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

// taskFile returns the path of file T-N: T-N-slug.md or "T-N slug.md".
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

// snapshot - the store content without .git and the service log and lock files.
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

// noTmpLeft: no temporary files. After kill -9 a leftover in .tix/tmp (in .gitignore) is allowed: strictTmp=false.
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
			b.t.Errorf("temporary file left: %s", rel)
		}
		if !info.IsDir() && strings.HasPrefix(rel, ".tix/tmp/") {
			b.t.Errorf("file left in .tix/tmp: %s", rel)
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
		b.t.Errorf("git fsck is not clean:\n%s", out)
	}
}

func (b *box) showRev(id string) string {
	b.t.Helper()
	r := b.ok("", "show", id)
	first := strings.SplitN(r.out, "\n", 2)[0]
	if !strings.HasPrefix(first, "rev ") {
		b.t.Fatalf("show %s: first line %q, want `rev <blob>`", id, first)
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
		b.t.Fatalf("show --json is not JSON: %v\n%s", err, r.out)
	}
	return s
}

// fakeGit installs a git wrapper: the mode comes from the file mode next to it: pass; fail and hang - on
// commit (hang leaves .git/index.lock, like a killed git); failon:<subcommand> - a refusal
// with the text boom-secret on any subcommand.
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

// ageLocks ages the git lock files: a dangling lock older than the threshold is removed.
// This tests the recovery, not a wait of 10 seconds.
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
	t.Fatalf("file %s did not appear within 20 s", path)
}

func TestScenario_Main_CommitPerOperation(t *testing.T) {
	b := newBox(t)
	if r := b.ok("", "new", "Alpha", "--as", "test:1"); r.out != "ok T-1 idea\n" {
		t.Fatalf("new: answer %q, want %q", r.out, "ok T-1 idea\n")
	}
	rev := b.showRev("T-1")
	b.ok("New body\n", "edit", "T-1", "--base", rev, "--as", "test:1")
	b.ok("", "comment", "T-1", "first", "--as", "test:1")
	b.ok("", "comment", "T-1", "second", "--as", "test:1")
	b.ok("", "note", "T-1", "step done", "--as", "test:1")
	file := b.taskFile("T-1")
	b.ok("", "rm", "T-1", "--as", "test:1")

	got := b.commits()
	want := []string{"T-1: ", "T-1: edit", "T-1: comment", "T-1: comment", "T-1: note", "T-1: rm"}
	if len(got) != len(want) {
		t.Fatalf("commits %d, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if !strings.HasPrefix(got[i].Subject, w) {
			t.Errorf("commit %d: subject %q, want prefix %q", i, got[i].Subject, w)
		}
		if got[i].Actor != "test:1" {
			t.Errorf("commit %d: Actor %q, want test:1", i, got[i].Actor)
		}
	}
	first := strings.Fields(b.git("log", "--reverse", "--format=%H", "-1", "--max-parents=0"))[0]
	files := b.git("show", "--name-only", "--format=", first)
	for _, f := range []string{".gitignore", ".tix/config.toml", ".tix/next_id", filepath.Base(file)} {
		if !strings.Contains(files, f) {
			t.Errorf("first commit lacks %s; files:\n%s", f, files)
		}
	}
	if note := strings.TrimSpace(b.git("show", "--name-only", "--format=", "HEAD~1")); note != "" {
		t.Errorf("note must be an empty commit, files: %q", note)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("rm: the file stayed")
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("after rm the tree is not clean:\n%s", st)
	}
	ign := b.read(filepath.Join(b.home, ".gitignore"))
	for _, p := range []string{".tix/lock", ".tix/log", ".tix/tmp", ".obsidian/workspace", ".trash/", "*.swp"} {
		if !strings.Contains(ign, p) {
			t.Errorf(".gitignore lacks %q:\n%s", p, ign)
		}
	}
}

func TestScenario_TwoAgents_Race5x20(t *testing.T) {
	if testing.Short() {
		t.Skip("slow tier: race 5x20")
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
		t.Fatalf("failures %d, first: %v", len(fails), fails[:min(3, len(fails))])
	}
	s := b.showJSON("T-1")
	if len(s.Comments) != 100 {
		t.Fatalf("comments %d, want 100", len(s.Comments))
	}
	seen := map[string]bool{}
	for _, c := range s.Comments {
		seen[strings.TrimSpace(c.Text)] = true
	}
	if len(seen) != 100 {
		t.Errorf("unique texts %d, want 100 (loss or duplicate)", len(seen))
	}
	n := 0
	for _, c := range b.commits() {
		if strings.HasPrefix(c.Subject, "T-1: comment") {
			n++
		}
	}
	if n != 100 {
		t.Errorf("comment commits %d, want 100", n)
	}
	if total := len(b.commits()); total != 101 {
		t.Errorf("total commits %d, want 101 (new + 100)", total)
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("tree is not clean after the race:\n%s", st)
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
	revA := b.showRev("T-1") // agent 1 read it and went to think

	b.ok(body("A-first", "l10"), "edit", "T-1", "--base", revA) // agent 2 edits the start
	b.ok("", "comment", "T-1", "between edits")

	// Agent 1 edits the end on the old base: different places, a merge.
	b.ok(body("l01", "B-last"), "edit", "T-1", "--base", revA)
	got := b.read(b.taskFile("T-1"))
	for _, want := range []string{"A-first", "B-last", "between edits"} {
		if !strings.Contains(got, want) {
			t.Errorf("after the merge no %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "l01\n") || strings.Contains(got, "l10\n") {
		t.Errorf("after the merge old lines remained:\n%s", got)
	}

	// The same lines: a conflict, a refusal, the file is intact.
	before := b.snapshot()
	r := b.tix(body("C-first", "l10"), "edit", "T-1", "--base", revA)
	if r.code != 3 {
		t.Fatalf("conflict: code %d, want 3 (ErrConflict); stdout %q", r.code, r.out)
	}
	if !regexp.MustCompile(`^err T-1: .+\n$`).MatchString(r.err) {
		t.Errorf("conflict: stderr %q, want one line `err T-1: ...`", r.err)
	}
	if after := b.snapshot(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("conflict changed the store:\nbefore %v\nafter %v", before, after)
	}

	// A store not from git: a request to re-read.
	r = b.tix(body("X", "Y"), "edit", "T-1", "--base", strings.Repeat("ab", 20))
	if r.code != 1 || !strings.Contains(r.err, "tix show") {
		t.Errorf("store not from git: code %d, stderr %q, want err with `tix show`", r.code, r.err)
	}
}

// Spec 2.1/3: a body edit and an agent comment are different parts of the file, the merge is clean.
// The first comment adds the section right after the last body line; an edit of that line
// on the old base adjoins the new section (git merge-file gives a conflict).
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
	b.ok("", "comment", "T-1", "agent")
	r := b.tix(body("l01", "B-last"), "edit", "T-1", "--base", rev)
	if r.code != 0 {
		t.Fatalf("edit of the last line on the old base after the first comment: code %d, stderr %q", r.code, r.err)
	}
	got := b.read(b.taskFile("T-1"))
	for _, want := range []string{"A-first", "B-last", "agent"} {
		if !strings.Contains(got, want) {
			t.Errorf("the merge lost %q:\n%s", want, got)
		}
	}
}

func TestScenario_Repeat_DuplicateCommentButNoOpEdit(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "comment", "T-1", "duplicate")
	b.ok("", "comment", "T-1", "duplicate")
	if n := len(b.showJSON("T-1").Comments); n != 2 {
		t.Errorf("repeated comment: comments %d, want 2 (duplicate)", n)
	}
	b.ok("Body\n", "edit", "T-1", "--base", b.showRev("T-1"))
	head := b.git("rev-parse", "HEAD")
	r := b.ok("Body\n", "edit", "T-1", "--base", b.showRev("T-1"))
	if r.out != "ok T-1 edit\n" {
		t.Errorf("repeated edit: answer %q", r.out)
	}
	if b.git("rev-parse", "HEAD") != head {
		t.Error("edit without a body change created a commit")
	}
}

func TestScenario_Obsidian_ExternalEdit(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "new", "Beta")
	f2 := b.taskFile("T-2")
	b.write(f2, strings.Replace(b.read(f2), "title: Beta", "title: EDITED Beta", 1))
	b.write(filepath.Join(b.home, "Заметка про идею.md"), "# just a note\n")
	b.write(filepath.Join(b.home, "RULES.md"), "rules\n")
	b.write(filepath.Join(b.home, ".obsidian", "workspace.json"), "{}")

	b.ok("", "comment", "T-1", "hello")

	for _, line := range []string{"T-2: external edit", "external edit: Заметка про идею.md", "external edit: RULES.md"} {
		c, found := b.hasSubject(line)
		if !found {
			t.Errorf("no line %q in the commit messages: %+v", line, b.commits())
		} else if c.Actor != "external" {
			t.Errorf("commit with line %q: Actor %q, want external", line, c.Actor)
		}
	}
	last := b.commits()
	if !strings.HasPrefix(last[len(last)-1].Subject, "T-1: comment") {
		t.Errorf("last commit %q, want T-1: comment", last[len(last)-1].Subject)
	}
	if files := strings.Split(strings.TrimSpace(b.git("show", "--name-only", "--format=", "HEAD")), "\n"); len(files) != 1 || !strings.HasPrefix(files[0], "T-1") {
		t.Errorf("foreign files went into the comment commit: %v", files)
	}
	if ls := b.git("ls-files"); strings.Contains(ls, ".obsidian") {
		t.Errorf(".obsidian/workspace* got into git:\n%s", ls)
	}
	if !strings.Contains(b.read(f2), "EDITED Beta") {
		t.Error("the external edit of T-2 is lost")
	}

	// Restoring a deleted file through git checkout is also an external edit.
	f1 := b.taskFile("T-1")
	b.ok("", "rm", "T-1")
	b.git("checkout", "HEAD~1", "--", filepath.Base(f1))
	b.ok("", "note", "T-2", "after restore")
	c, found := b.hasSubject("T-1: external edit")
	if !found {
		t.Fatalf("file restore is not committed as an external edit: %+v", b.commits())
	}
	if c.Actor != "external" {
		t.Errorf("Actor %q, want external", c.Actor)
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("tree is not clean:\n%s", st)
	}
}

func TestScenario_NoteCommentDoNotCarryOtherFiles(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "new", "Beta")
	f2 := b.taskFile("T-2")
	b.write(f2, strings.Replace(b.read(f2), "title: Beta", "title: EDITED Beta", 1))
	b.git("add", "-A") // a foreign edit is already in the index
	b.ok("", "note", "T-1", "note")
	if files := strings.TrimSpace(b.git("show", "--name-only", "--format=", "HEAD")); files != "" {
		t.Errorf("note is not an empty commit, files: %q", files)
	}
	if c, ok := b.hasSubject("T-2: external edit"); !ok || c.Actor != "external" {
		t.Errorf("the edit of T-2 is not recorded as an external edit: %+v", b.commits())
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
		t.Errorf("lock is busy: code %d, stderr %q, want code 2 (busy) and one err line", r.code, r.err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("wait %v, want about 200 ms", elapsed)
	}
	if fmt.Sprint(b.snapshot()) != fmt.Sprint(before) {
		t.Error("a lock refusal changed the store")
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
			t.Errorf("%s not removed", p)
		}
	}
	log := b.read(filepath.Join(b.home, ".tix", "log"))
	if !strings.Contains(log, "WARN") || !strings.Contains(log, "drop_index_lock") {
		t.Errorf("no WARN op=drop_index_lock in .tix/log:\n%s", log)
	}
}

// A fresh lock - git of another process is still working: the operation does not touch it and does not write.
func TestScenario_FreshGitLock_BusyNothingWritten(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Alpha")
	lock := filepath.Join(b.home, ".git", "index.lock")
	b.write(lock, "")
	before := b.snapshot()
	r := b.tix("", "comment", "T-1", "busy")
	if r.code != 2 || r.err != "err T-1: git is busy\n" {
		t.Errorf("fresh lock: code %d, stderr %q, want code 2 and `err T-1: git is busy`", r.code, r.err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("fresh lock removed: %v", err)
	}
	if fmt.Sprint(b.snapshot()) != fmt.Sprint(before) {
		t.Error("the git is busy refusal changed the store")
	}
	// The lock got old: dangling, the operation removes it.
	b.ageLocks()
	b.ok("", "comment", "T-1", "after")
	if _, err := os.Stat(lock); err == nil {
		t.Error("dangling lock not removed")
	}
}

// The git stderr text does not reach the answer on any path; the full text is in .tix/log.
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
				b.ok("", "comment", "T-1", "other") // the file moved away from the base: edit goes through a merge
				b.ok(strings.Replace(body, "l1", "first", 1), "edit", "T-1", "--base", b.showRev("T-1"))
			}
			b.write(filepath.Join(dir, "mode"), c.mode)
			r := c.run(b, env)
			if r.code != c.wantCode {
				t.Errorf("code %d, want %d; stdout %q, stderr %q", r.code, c.wantCode, r.out, r.err)
			}
			for _, bad := range []string{secret, "fatal", "exit 128"} {
				if strings.Contains(r.err, bad) || strings.Contains(r.out, bad) {
					t.Errorf("git text %q in the answer: stdout %q, stderr %q", bad, r.out, r.err)
				}
			}
			if log := b.read(filepath.Join(b.home, ".tix", "log")); !strings.Contains(log, secret) {
				t.Errorf("the full git text is not in .tix/log:\n%s", log)
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
	r := b.runEnv(env, "", "comment", "T-1", "no commit")
	if r.code != 0 || r.out != "ok T-1 comment\n" {
		t.Errorf("commit failed: code %d, stdout %q, want code 0 and `ok T-1 comment`", r.code, r.out)
	}
	if !strings.Contains(r.err, warnCommitFailed) {
		t.Errorf("stderr %q, want %q", r.err, warnCommitFailed)
	}
	if strings.Contains(r.err, "boom") || strings.Contains(r.err, "fatal") {
		t.Errorf("git output in stderr: %q", r.err)
	}
	if !strings.Contains(b.read(b.taskFile("T-1")), "no commit") {
		t.Error("the edit did not reach the file")
	}
	if log := b.read(filepath.Join(b.home, ".tix", "log")); strings.TrimSpace(log) == "" {
		t.Error("the failed commit is not recorded in .tix/log")
	}
	// The next operation commits the written change as an external edit.
	b.write(filepath.Join(dir, "mode"), "pass")
	b.runEnv(env, "", "note", "T-1", "next")
	if c, ok := b.hasSubject("T-1: external edit"); !ok || c.Actor != "external" {
		t.Errorf("the written change is not committed as an external edit: %+v", b.commits())
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("tree is not clean:\n%s", st)
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
		t.Fatalf("the killed git left no index.lock: %v", err)
	}
	if !strings.Contains(b.read(b.taskFile("T-1")), "zzz") {
		t.Error("the written edit disappeared")
	}
	if r := b.ok("", "list"); r.err != "" {
		t.Errorf("list after kill: stderr %q, is the file half-written?", r.err)
	}
	b.write(filepath.Join(dir, "mode"), "pass")
	// While the lock is fresh, git may have been alive: a refusal without a write.
	if r := b.tix("", "note", "T-1", "early"); r.code != 2 || !strings.Contains(r.err, "git is busy") {
		t.Errorf("fresh lock after kill: code %d, stderr %q, want git is busy", r.code, r.err)
	}
	b.ageLocks()
	r := b.runEnv(b.with("TIX_LOCK_TIMEOUT=5s"), "", "note", "T-1", "after kill")
	if r.code != 0 {
		t.Fatalf("after kill -9 the operation failed (lock not released?): code %d, %q", r.code, r.err)
	}
	if c, ok := b.hasSubject("T-1: external edit"); !ok || c.Actor != "external" {
		t.Errorf("the edit did not go as an external edit: %+v", b.commits())
	}
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("tree is not clean:\n%s", st)
	}
}

func TestScenario_Kill9Random_RepoStaysConsistent(t *testing.T) {
	if testing.Short() {
		t.Skip("slow tier: kill -9 at random moments")
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
		time.Sleep(time.Duration(i%12*4) * time.Millisecond) // moment of the strike: 0-44 ms
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if err := c.Wait(); err == nil {
			finished = append(finished, text)
		}
		b.ageLocks() // the killed git left a lock: treat it as dangling
	}
	b.noTmpLeft(false)
	b.fsck()
	if r := b.ok("", "list"); r.err != "" {
		t.Errorf("list: stderr %q, the task file is half-written", r.err)
	}
	fin := b.ok("", "note", "T-1", "final")
	if st := b.git("status", "--porcelain"); st != "" {
		t.Errorf("after the final operation the tree is not clean:\n%s\nstderr note: %q\ndiff:\n%s\nlog:\n%s", st, fin.err, b.git("diff", "HEAD"), b.read(filepath.Join(b.home, ".tix", "log")))
	}
	have := map[string]bool{}
	for _, c := range b.showJSON("T-1").Comments {
		have[strings.TrimSpace(c.Text)] = true
	}
	for _, f := range finished {
		if !have[f] {
			t.Errorf("command %s exited with code 0, but there is no comment", f)
		}
	}
}
