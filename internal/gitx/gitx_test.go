package gitx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/gitx"
)

var gitBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tix-gitx-")
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

func newRepo(t *testing.T) gitx.Repo {
	t.Helper()
	r := gitx.Repo{Dir: t.TempDir(), Bin: gitBin}
	if err := r.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return r
}

func raw(t *testing.T, r gitx.Repo, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), gitBin, append([]string{"-C", r.Dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func put(t *testing.T, r gitx.Repo, name, data string) {
	t.Helper()
	p := filepath.Join(r.Dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	defer guard(t)
	if got, err := gitx.Resolve(gitBin); err != nil || got != gitBin {
		t.Errorf("Resolve(override) = %q, %v; ждали %q", got, err, gitBin)
	}
	got, err := gitx.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\"): %v", err)
	}
	if out, err := exec.CommandContext(context.Background(), got, "--version").CombinedOutput(); err != nil || !bytes.Contains(out, []byte("git version")) {
		t.Errorf("найденный %q не git: %v %s", got, err, out)
	}
}

func TestInit_CreatesRepository(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	if st, err := os.Stat(filepath.Join(r.Dir, ".git")); err != nil || !st.IsDir() {
		t.Errorf(".git не создан: %v", err)
	}
}

func TestCommit_OnlyGivenPaths_ActorTrailer(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	put(t, r, "a.md", "a\n")
	put(t, r, "b.md", "b\n")
	put(t, r, "c.md", "c\n")
	raw(t, r, "add", "c.md") // чужая правка уже в индексе
	hash, err := r.Commit([]string{"a.md"}, "T-1: new", "bob:1")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if head := strings.TrimSpace(raw(t, r, "rev-parse", "HEAD")); hash != head {
		t.Errorf("хеш %q, HEAD %q", hash, head)
	}
	if files := strings.Fields(raw(t, r, "show", "--name-only", "--format=", "HEAD")); len(files) != 1 || files[0] != "a.md" {
		t.Errorf("в коммит ушли файлы %v, ждали только a.md", files)
	}
	if s := raw(t, r, "log", "-1", "--format=%s"); strings.TrimSpace(s) != "T-1: new" {
		t.Errorf("тема %q", s)
	}
	if a := strings.TrimSpace(raw(t, r, "log", "-1", "--format=%(trailers:key=Actor,valueonly)")); a != "bob:1" {
		t.Errorf("трейлер Actor %q, ждали bob:1", a)
	}
	st := raw(t, r, "status", "--porcelain")
	if !strings.Contains(st, "b.md") || !strings.Contains(st, "c.md") {
		t.Errorf("b.md и c.md должны остаться вне коммита:\n%s", st)
	}
}

func TestCommit_DeletedPathAndEmptyCommit(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	put(t, r, "a.md", "a\n")
	if _, err := r.Commit([]string{"a.md"}, "T-1: new", "x"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.Dir, "a.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit([]string{"a.md"}, "T-1: rm", "x"); err != nil {
		t.Fatalf("коммит удаления: %v", err)
	}
	if ls := strings.TrimSpace(raw(t, r, "ls-files")); ls != "" {
		t.Errorf("a.md остался в индексе: %q", ls)
	}
	before := strings.TrimSpace(raw(t, r, "rev-list", "--count", "HEAD"))
	if _, err := r.Commit(nil, "T-1: note шаг", "x"); err != nil {
		t.Fatalf("пустой коммит: %v", err)
	}
	if after := strings.TrimSpace(raw(t, r, "rev-list", "--count", "HEAD")); after == before {
		t.Error("пустой список путей не создал пустой коммит")
	}
	if files := strings.TrimSpace(raw(t, r, "show", "--name-only", "--format=", "HEAD")); files != "" {
		t.Errorf("note-коммит несёт файлы: %q", files)
	}
}

func TestStatus_UntrackedModifiedDeleted_CyrillicPaths(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	put(t, r, "tracked.md", "1\n")
	put(t, r, "gone.md", "1\n")
	if _, err := r.Commit([]string{"tracked.md", "gone.md"}, "init", "x"); err != nil {
		t.Fatal(err)
	}
	put(t, r, "tracked.md", "2\n")
	_ = os.Remove(filepath.Join(r.Dir, "gone.md"))
	put(t, r, "Заметка про идею.md", "x\n")
	put(t, r, "dir/nested file.md", "x\n")
	got, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{}
	for _, c := range got {
		codes[c.Path] = strings.TrimSpace(c.Code)
	}
	want := map[string]string{"tracked.md": "M", "gone.md": "D", "Заметка про идею.md": "??", "dir/nested file.md": "??"}
	for p, c := range want {
		if codes[p] != c {
			t.Errorf("Status[%q] = %q, ждали %q; всё: %v", p, codes[p], c, codes)
		}
	}
}

func TestHashObject_BlobRoundTrip(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	data := []byte("тело\nвторая строка\n")
	id, err := r.HashObject(data)
	if err != nil {
		t.Fatal(err)
	}
	put(t, r, "f.md", string(data))
	if want := strings.TrimSpace(raw(t, r, "hash-object", "f.md")); id != want {
		t.Errorf("HashObject = %q, git hash-object = %q", id, want)
	}
	back, err := r.Blob(id)
	if err != nil || !bytes.Equal(back, data) {
		t.Errorf("Blob(%s) = %q, %v; ждали %q (blob записан -w)", id, back, err, data)
	}
}

func TestMergeFile(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	base := []byte("a\nb\nc\nd\ne\nf\ng\nh\n")
	t.Run("разные места - чисто", func(t *testing.T) {
		defer guard(t)
		out, conflict, err := r.MergeFile([]byte("A\nb\nc\nd\ne\nf\ng\nh\n"), base, []byte("a\nb\nc\nd\ne\nf\ng\nH\n"))
		if err != nil || conflict {
			t.Fatalf("конфликт=%v err=%v, ждали чистое слияние", conflict, err)
		}
		if string(out) != "A\nb\nc\nd\ne\nf\ng\nH\n" {
			t.Errorf("результат %q", out)
		}
	})
	t.Run("те же строки - конфликт", func(t *testing.T) {
		defer guard(t)
		_, conflict, err := r.MergeFile([]byte("a\nb\nc\nX\ne\nf\ng\nh\n"), base, []byte("a\nb\nc\nY\ne\nf\ng\nh\n"))
		if err != nil || !conflict {
			t.Errorf("конфликт=%v err=%v, ждали conflict=true без ошибки", conflict, err)
		}
	})
}

var testLocks = []string{"index.lock", "HEAD.lock", filepath.Join("refs", "heads", "main.lock"),
	filepath.Join("refs", "heads", "feat", "x.lock")}

// putLocks создаёт пустые lock-файлы; age > 0 состаривает их на это время.
func putLocks(t *testing.T, r gitx.Repo, locks []string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	for _, l := range locks {
		p := filepath.Join(r.Dir, ".git", l)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDropGitLocks_NoFiles(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	if dropped, err := r.DropGitLocks(); err != nil || len(dropped) != 0 {
		t.Errorf("dropped=%v err=%v, want none, nil", dropped, err)
	}
}

func TestDropGitLocks_StaleRemoved(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	putLocks(t, r, testLocks, time.Minute)
	dropped, err := r.DropGitLocks()
	if err != nil || len(dropped) != len(testLocks) {
		t.Errorf("dropped=%v err=%v, want %d paths", dropped, err, len(testLocks))
	}
	for _, l := range testLocks {
		if _, err := os.Stat(filepath.Join(r.Dir, ".git", l)); err == nil {
			t.Errorf("%s left behind", l)
		}
	}
}

func TestDropGitLocks_FreshKeptBusy(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	putLocks(t, r, testLocks, 0)
	if dropped, err := r.DropGitLocks(); !errors.Is(err, gitx.ErrBusy) || len(dropped) != 0 {
		t.Errorf("dropped=%v err=%v, want none, ErrBusy", dropped, err)
	}
	for _, l := range testLocks {
		if _, err := os.Stat(filepath.Join(r.Dir, ".git", l)); err != nil {
			t.Errorf("fresh %s removed: %v", l, err)
		}
	}
}

func TestDropGitLocks_OneFreshRemovesNothing(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	putLocks(t, r, testLocks[1:], time.Minute)
	putLocks(t, r, testLocks[:1], 0)
	if dropped, err := r.DropGitLocks(); !errors.Is(err, gitx.ErrBusy) || len(dropped) != 0 {
		t.Errorf("dropped=%v err=%v, want none, ErrBusy", dropped, err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, ".git", testLocks[1])); err != nil {
		t.Errorf("stale lock removed despite fresh one: %v", err)
	}
}

func TestError_ShortTextAndDetail(t *testing.T) {
	err := error(fmt.Errorf("outer: %w", &gitx.Error{Op: "status", Code: 128, Stderr: "fatal: boom\n"}))
	if strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "git status failed") {
		t.Errorf("Error() = %q, want short phrase without git text", err)
	}
	if d := gitx.Detail(err); !strings.Contains(d, "boom") || !strings.Contains(d, "exit 128") {
		t.Errorf("Detail() = %q, want git text", d)
	}
}
