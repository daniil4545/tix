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

// stubScript - an EDITOR stub. Call n: writes seen.n (the copy at start) and started.n; if wait.n
// exists - waits for go.n (not longer than 10 s); then runs script.n with the copy path as the first argument.
const stubScript = `#!/bin/sh
d="$TIX_STUB_DIR"; f="$1"
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
	if err := os.WriteFile(bin, []byte(stubScript), 0o700); err != nil { //nolint:gosec // test stub
		t.Fatal(err)
	}
	t.Setenv("EDITOR", bin)
	t.Setenv("TIX_STUB_DIR", dir)
	return &stub{t: t, dir: dir}
}

func (s *stub) file(name string) string { return filepath.Join(s.dir, name) }

// plan sets call n: gated - wait for go.n; script - an sh command over the copy (empty - exit without an edit).
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
	m, _ := filepath.Glob(filepath.Join(e.root, ".tix", "tmp", "ed-T-1-*"))
	return m
}

const base3 = "line1\nline2\nline3\n"

// openEditor: task T-1 with body base3, the card is open, e is pressed.
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
	e.until("editor started", func() bool { _, err := os.Stat(st.file("started.1")); return err == nil })
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
	e.until("editor called", func() bool { return st.calls() == 1 })
	e.until("copy removed", func() bool { return len(copies(e)) == 0 })
	if got := e.gitOut("rev-list", "--count", "HEAD"); got != n {
		t.Errorf("exit without an edit created a commit: was %s, now %s", n, got)
	}
	if !strings.Contains(st.seen(1), "line2") {
		t.Errorf("editor was not opened on the task body: %q", st.seen(1))
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
	e.until("edit and comment in the file", func() bool {
		_, _, body, cs := e.get("T-1")
		return strings.Contains(body, "USER-LINE") && strings.Contains(strings.Join(cs, "|"), "agent says")
	})
	e.until("copy removed", func() bool { return len(copies(e)) == 0 })
	if st.calls() != 1 {
		t.Errorf("editor called %d times, expected 1", st.calls())
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
	e.until("second editor start", func() bool { _, err := os.Stat(st.file("started.2")); return err == nil })
	for _, want := range []string{"<<<<<<<", "USER", "AGENT"} {
		if !strings.Contains(st.seen(2), want) {
			t.Errorf("repeat start without %q in the copy:\n%s", want, st.seen(2))
		}
	}
	e.until("merge saved", func() bool { _, _, body, _ := e.get("T-1"); return strings.Contains(body, "BOTH") })
	if _, _, body, _ := e.get("T-1"); strings.Contains(body, "<<<<<<<") {
		t.Errorf("markers got into the file:\n%s", body)
	}
	e.until("copy removed", func() bool { return len(copies(e)) == 0 })
}

func TestEditor_Conflict_SecondRunUnchanged_CancelsCopyKept(t *testing.T) {
	st := newStub(t)
	st.plan(1, true, `printf 'line1\nUSER\nline3\n' > "$1"`)
	e := openEditor(t, st)
	_, curRev, _ := e.store.Get("T-1")
	if _, err := e.store.Edit("T-1", curRev, "line1\nAGENT\nline3\n", "agent"); err != nil {
		t.Fatal(err)
	}
	n := e.gitOut("rev-list", "--count", "HEAD")
	st.touch("go.1")
	e.until("second start", func() bool { return st.calls() == 2 })
	e.screenHas("copy kept: ")
	e.screenHas("edit cancelled")
	if len(copies(e)) != 1 {
		t.Errorf("copy with the human edit not kept: %v", copies(e))
	}
	if _, _, body, _ := e.get("T-1"); strings.Contains(body, "USER") || !strings.Contains(body, "AGENT") {
		t.Errorf("cancel changed the file:\n%s", body)
	}
	if got := e.gitOut("rev-list", "--count", "HEAD"); got != n {
		t.Error("cancel created a commit")
	}
}

func TestEditor_MarkersLeftInCopy_RefusedCopyKeptPathShown(t *testing.T) {
	st := newStub(t)
	st.plan(1, true, `printf 'line1\nUSER\nline3\n' > "$1"`)
	st.plan(2, false, `printf 'extra\n' >> "$1"`) // there is an edit, markers remain
	e := openEditor(t, st)
	_, curRev, _ := e.store.Get("T-1")
	if _, err := e.store.Edit("T-1", curRev, "line1\nAGENT\nline3\n", "agent"); err != nil {
		t.Fatal(err)
	}
	n := e.gitOut("rev-list", "--count", "HEAD")
	st.touch("go.1")
	e.until("copy stays", func() bool { return st.calls() == 2 && len(copies(e)) == 1 })
	e.screenHas("ed-T-1-")
	if _, _, body, _ := e.get("T-1"); strings.Contains(body, "<<<<<<<") || strings.Contains(body, "extra") {
		t.Errorf("copy with markers was written:\n%s", body)
	}
	if got := e.gitOut("rev-list", "--count", "HEAD"); got != n {
		t.Error("refusal created a commit")
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
	e.until("editor started", func() bool { _, err := os.Stat(st.file("started.1")); return err == nil })
	lock, err := os.OpenFile(filepath.Join(e.root, ".tix", "lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	st.touch("go.1")
	e.until("copy stays", func() bool { return len(copies(e)) == 1 })
	e.screenHas("ed-T-1-")
	if _, _, body, _ := e.get("T-1"); strings.Contains(body, "USER") {
		t.Errorf("write passed under a held lock:\n%s", body)
	}
}
