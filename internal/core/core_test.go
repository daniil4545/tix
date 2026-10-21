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

	"github.com/daniil4545/tix/internal/core"
)

// The comments section heading: edit it here when the format language changes.
const commentsHeading = "## Comments"

var gitBin string

// injected - the line the git wrapper writes into the file around the core before commit.
const injected = "OUTSIDE-LINE"

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tix-core-")
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
		t.Fatalf("panic: %v", r)
	}
}

type env struct {
	t    *testing.T
	root string
	fake string // dir of the git wrapper
	log  *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, root: filepath.Join(t.TempDir(), "tasks"), fake: t.TempDir(), log: &bytes.Buffer{}}
	script := "#!/bin/sh\nD=" + e.fake + "\necho \"$*\" >> \"$D/calls.log\"\n" +
		"if [ -f \"$D/failcommit\" ]; then for a in \"$@\"; do [ \"$a\" = commit ] && { echo 'fatal: boom' >&2; exit 128; }; done; fi\n" +
		// inject: before commit a foreign process writes the file through temp and rename (the rename-commit window).
		"if [ -f \"$D/inject\" ]; then for a in \"$@\"; do if [ \"$a\" = commit ]; then f=$(cat \"$D/inject\"); rm \"$D/inject\"; " +
		"{ echo " + injected + "; cat \"$f\" 2>/dev/null; } > \"$f.x\"; mv \"$f.x\" \"$f\"; break; fi; done; fi\n" +
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
		e.t.Fatalf("file %s: found %d", id, len(m))
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
		t.Fatalf("Create: %+v, want ID T-1 and a commit without a warning", r)
	}
	if head := strings.TrimSpace(e.git("rev-parse", "HEAD")); !strings.HasPrefix(head, r.Commit) && !strings.HasPrefix(r.Commit, head) {
		t.Errorf("Result.Commit %q is not HEAD %q", r.Commit, head)
	}
	c, err := s.Comment("T-1", "hello", "tester")
	if err != nil {
		t.Fatal(err)
	}
	blob := strings.TrimSpace(e.git("hash-object", e.file("T-1")))
	if c.Rev != blob {
		t.Errorf("Result.Rev %q, git blob of the file %q", c.Rev, blob)
	}
	tk, rev, err := s.Get("T-1")
	if err != nil {
		t.Fatal(err)
	}
	if rev != blob || tk.Title != "Alpha" || tk.Status != "todo" || tk.Project != "proj" || len(tk.Comments) != 1 {
		t.Errorf("Get: rev=%q task=%+v", rev, tk)
	}
	if out := e.git("cat-file", "-t", rev); strings.TrimSpace(out) != "blob" {
		t.Errorf("Get did not write blob %s to git: %q", rev, out)
	}
}

func TestStore_NotFoundAndBadColumn(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	n := e.count()
	if _, err := s.Create("X", "proj", "nosuch", "tester"); err == nil {
		t.Error("unknown column accepted")
	}
	if _, _, err := s.Get("T-99"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("Get T-99: %v, want ErrNotFound", err)
	}
	for name, f := range map[string]func() (core.Result, error){
		"Comment": func() (core.Result, error) { return s.Comment("T-99", "x", "a") },
		"Note":    func() (core.Result, error) { return s.Note("T-99", "x", "a") },
		"Edit":    func() (core.Result, error) { return s.Edit("T-99", "deadbeef", "x\n", "a") },
		"Remove":  func() (core.Result, error) { return s.Remove("T-99", "a") },
	} {
		if _, err := f(); !errors.Is(err, core.ErrNotFound) {
			t.Errorf("%s T-99: %v, want ErrNotFound", name, err)
		}
	}
	if e.count() != n {
		t.Error("errors created commits")
	}
}

func TestStore_NoteEmptyCommit_RemoveDeletes(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	if _, err := s.Note("T-1", "step", "tester"); err != nil {
		t.Fatal(err)
	}
	if subj := strings.TrimSpace(e.git("log", "-1", "--format=%s")); !strings.HasPrefix(subj, "T-1: note") || !strings.Contains(subj, "step") {
		t.Errorf("note subject %q, want `T-1: note step`", subj)
	}
	if files := strings.TrimSpace(e.git("show", "--name-only", "--format=", "HEAD")); files != "" {
		t.Errorf("note carries files: %q", files)
	}
	f := e.file("T-1")
	if _, err := s.Remove("T-1", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f); err == nil {
		t.Error("Remove left the file")
	}
	if _, _, err := s.Get("T-1"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("Get after Remove: %v", err)
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
		t.Fatalf("merge on an old base: %v", err)
	}
	got, _ := os.ReadFile(e.file("T-1"))
	if !strings.Contains(string(got), "A-first") || !strings.Contains(string(got), "B-last") {
		t.Errorf("the merge lost an edit:\n%s", got)
	}
	// conflict
	n := e.count()
	if _, err := s.Edit("T-1", revA, body("C-first", "l12"), "c"); !errors.Is(err, core.ErrConflict) {
		t.Errorf("same lines: %v, want ErrConflict", err)
	}
	after, _ := os.ReadFile(e.file("T-1"))
	if !bytes.Equal(got, after) || e.count() != n {
		t.Error("the conflict changed the file or the history")
	}
	// no body change - no commit
	_, cur, _ := s.Get("T-1")
	tk, _, _ := s.Get("T-1")
	if _, err := s.Edit("T-1", cur, tk.Body, "a"); err != nil {
		t.Fatal(err)
	}
	if e.count() != n {
		t.Error("edit without changes created a commit")
	}
	// comments heading in the body
	if _, err := s.Edit("T-1", cur, "x\n"+commentsHeading+"\ny\n", "a"); err == nil {
		t.Error("comments heading in the body accepted")
	}
	// a store not from git
	if _, err := s.Edit("T-1", strings.Repeat("ab", 20), "x\n", "a"); err == nil {
		t.Error("a store not from git accepted")
	}
}

func TestStore_LockBusy_ErrLocked(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(100 * time.Millisecond)
	mustCreate(t, s, "Alpha")
	lock, err := os.OpenFile(filepath.Join(e.root, ".tix", "lock"), os.O_RDWR|os.O_CREATE, 0o644)
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
		t.Errorf("lock busy: %v, want ErrLocked", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("waiting for the lock is not bounded by LockTimeout")
	}
	if e.count() != n {
		t.Error("a lock refusal created a commit")
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
	r, err := s.Comment("T-1", "no commit", "a")
	if err != nil {
		t.Fatalf("a failed commit must give a Warning, not an error: %v", err)
	}
	if r.Warning == "" {
		t.Error("Result.Warning is empty on a failed commit")
	}
	if got, _ := os.ReadFile(e.file("T-1")); !strings.Contains(string(got), "no commit") {
		t.Error("the edit is not in the file")
	}
	if err := os.Remove(filepath.Join(e.fake, "failcommit")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Note("T-1", "next", "a"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.git("log", "--format=%s"), "T-1: external edit") {
		t.Errorf("what was written is not committed as an external edit:\n%s", e.git("log", "--format=%s"))
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
		t.Fatalf("Comment with index.lock: %v", err)
	}
	if _, err := os.Stat(idx); err == nil {
		t.Error("index.lock not removed")
	}
	if l := e.log.String(); !strings.Contains(l, "level=WARN") || !strings.Contains(l, "op=drop_index_lock") {
		t.Errorf("no `level=WARN ... op=drop_index_lock` in the log:\n%s", l)
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
		t.Fatalf("List: %d tasks, %d errors; want 2 and 1", len(tasks), len(errs))
	}
	if !strings.Contains(errs[0].Error(), "T-9-broken.md:") {
		t.Errorf("error %q without `path:line`", errs[0])
	}
	if c := e.calls(); c != "" {
		t.Errorf("List called git:\n%s", c)
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
			t.Errorf("temp file left: %s", p)
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
		t.Fatalf("Comment with a fresh lock: %v, want `git is busy` (ErrLocked)", err)
	}
	if _, err := os.Stat(idx); err != nil {
		t.Errorf("fresh lock removed: %v", err)
	}
	if after, _ := os.ReadFile(e.file("T-1")); !bytes.Equal(before, after) {
		t.Error("the git is busy refusal changed the task file")
	}
}

// Step 4 of 2.1: the file changed between the read and rename - an edit around the core is not lost,
// the operation repeats with external edit. Turns red if the check in replace is removed.
func TestStore_ExternalEditBetweenReadAndRename_NotLost(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	const outside = "\nedit around the core\n"
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
	if _, err := s.Comment("T-1", "our comment", "agent:1"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	got, _ := os.ReadFile(e.file("T-1"))
	for _, want := range []string{"edit around the core", "our comment"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("no %q in the file:\n%s", want, got)
		}
	}
	if calls != 2 {
		t.Errorf("attempts %d, want 2 (a repeat after an edit around the core)", calls)
	}
	if !strings.Contains(e.git("log", "--format=%s"), "T-1: external edit") {
		t.Errorf("the edit around the core is not committed separately:\n%s", e.git("log", "--format=%s"))
	}
	if st := strings.TrimSpace(e.git("status", "--porcelain")); st != "" {
		t.Errorf("tree not clean:\n%s", st)
	}
}

// The file changes on each of three attempts: an error, the file is not touched by us, no temp left.
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
		_, _ = f.WriteString("\nexternal edit " + string(rune('A'+attempt)) + "\n")
	})
	_, err := s.Comment("T-1", "must not be written", "agent:1")
	if err == nil || !strings.Contains(err.Error(), "file changes during write") {
		t.Fatalf("Comment: %v, want the error `file changes during write`", err)
	}
	if calls != 3 {
		t.Errorf("attempts %d, want 3", calls)
	}
	got, _ := os.ReadFile(e.file("T-1"))
	if strings.Contains(string(got), "must not be written") || !strings.Contains(string(got), "external edit C") {
		t.Errorf("file after the refusal:\n%s", got)
	}
	_ = filepath.Walk(e.root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if err == nil && !info.IsDir() && strings.Contains(info.Name(), ".tmp") {
			t.Errorf("temp file left: %s", p)
		}
		return nil
	})
}

// The window between the core rename and git commit: a foreign process writes the file (temp and rename).
// The operation commit carries exactly the core content under the operation Actor; the foreign edit
// goes with the next operation with Actor: external. Turns red if the commit takes the file from the tree.
func TestStore_ExternalWriteBetweenRenameAndCommit_KeepsAuthor(t *testing.T) {
	cases := []struct {
		name, subject, file, want string // file is written by the wrapper; want - the operation edit in the commit
		op                        func(s *core.Store) (core.Result, error)
	}{
		{"comment", "T-1: comment", "T-1 alpha.md", "our comment", func(s *core.Store) (core.Result, error) {
			return s.Comment("T-1", "our comment", "agent-x")
		}},
		{"rm", "T-1: rm", "T-1 alpha.md", "", func(s *core.Store) (core.Result, error) {
			return s.Remove("T-1", "agent-x")
		}},
		{"new", "T-2: new", "T-2 beta.md", "title: Beta", func(s *core.Store) (core.Result, error) {
			return s.Create("Beta", "proj", "todo", "agent-x")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			e := newEnv(t)
			s := e.open(2 * time.Second)
			mustCreate(t, s, "Alpha")
			inject := filepath.Join(e.fake, "inject")
			if err := os.WriteFile(inject, []byte(filepath.Join(e.root, tc.file)), 0o644); err != nil {
				t.Fatal(err)
			}
			r, err := tc.op(s)
			if err != nil || r.Warning != "" || r.Commit == "" {
				t.Fatalf("operation: err=%v warning=%q commit=%q", err, r.Warning, r.Commit)
			}
			if _, err := os.Stat(inject); err == nil {
				t.Fatal("the wrapper did not reach commit: the window is not reproduced")
			}
			e.checkOwnCommit(r.Commit, tc.subject, tc.file, tc.want)
			if _, err := s.Create("Gamma", "proj", "todo", "tester"); err != nil {
				t.Fatal(err)
			}
			e.checkExternalCommitted()
		})
	}
}

// checkOwnCommit: the operation commit under Actor: agent-x carries the core bytes, not a foreign edit.
// want empty - the operation deletes the file.
func (e *env) checkOwnCommit(commit, subject, file, want string) {
	e.t.Helper()
	if msg := e.git("log", "-1", "--format=%s%n%b", commit); !strings.Contains(msg, subject) || !strings.Contains(msg, "Actor: agent-x") {
		e.t.Fatalf("operation commit %s:\n%s", commit, msg)
	}
	if want == "" {
		if strings.Contains(e.git("ls-tree", "--name-only", commit), file) {
			e.t.Errorf("commit %q did not delete the file: a foreign version is in the tree", subject)
		}
		return
	}
	blob := e.git("show", commit+":"+file)
	if strings.Contains(blob, injected) || !strings.Contains(blob, want) {
		e.t.Errorf("commit %q under Actor: agent-x: want %q without a foreign edit:\n%s", subject, want, blob)
	}
}

// checkExternalCommitted: the foreign edit went in an external edit commit, the tree is clean.
func (e *env) checkExternalCommitted() {
	e.t.Helper()
	ext := strings.TrimSpace(e.git("log", "--format=%H", "--grep", "external edit", "-1"))
	if ext == "" || !strings.Contains(e.git("show", ext), "+"+injected) ||
		!strings.Contains(e.git("log", "-1", "--format=%b", ext), "Actor: external") {
		e.t.Errorf("the foreign edit did not go in an external edit commit:\n%s", e.git("log", "--format=%h %s | %b"))
	}
	if st := strings.TrimSpace(e.git("status", "--porcelain")); st != "" {
		e.t.Errorf("tree not clean:\n%s", st)
	}
}

// Spec 2.1, step 5, for rm and new: the commit failed - Warning, the edit is in the tree; the next operation
// commits it as an external edit, the tree is clean. Turns red if the index is stuck after the refusal.
func TestStore_CommitFails_RemoveAndCreate_NextOpExternalEdit(t *testing.T) {
	cases := []struct {
		name string
		op   func(s *core.Store) (core.Result, error)
		want string // the external edit commit line
	}{
		{"rm", func(s *core.Store) (core.Result, error) { return s.Remove("T-2", "a") }, "T-2: external edit"},
		{"new", func(s *core.Store) (core.Result, error) { return s.Create("Gamma", "proj", "todo", "a") }, "T-3: external edit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			e := newEnv(t)
			s := e.open(2 * time.Second)
			mustCreate(t, s, "Alpha")
			mustCreate(t, s, "Beta")
			fail := filepath.Join(e.fake, "failcommit")
			if err := os.WriteFile(fail, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if r, err := tc.op(s); err != nil || r.Warning == "" {
				t.Fatalf("failed commit: err=%v warning=%q, want a Warning without an error", err, r.Warning)
			}
			if err := os.Remove(fail); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Note("T-1", "next", "a"); err != nil {
				t.Fatal(err)
			}
			if log := e.git("log", "--format=%s"); !strings.Contains(log, tc.want) {
				t.Errorf("no commit %q:\n%s", tc.want, log)
			}
			if st := strings.TrimSpace(e.git("status", "--porcelain")); st != "" {
				t.Errorf("tree not clean:\n%s", st)
			}
		})
	}
}
