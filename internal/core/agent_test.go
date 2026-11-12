package core_test

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
)

// The agent cycle, slice S2.

const (
	critBody = "## Details\n- [ ] one\n- [ ] two\n- [ ] three\n"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (e *env) openAt(c *clock) *core.Store {
	e.t.Helper()
	s, err := core.Open(e.root, core.Options{
		LockTimeout: 5 * time.Second, GitBin: filepath.Join(e.fake, "git"),
		Log: slog.New(slog.NewTextHandler(e.log, nil)), Now: c.now,
	})
	if err != nil {
		e.t.Fatalf("Open: %v", err)
	}
	return s
}

func (e *env) subject() string { return strings.TrimSpace(e.git("log", "-1", "--format=%s")) }

func (e *env) text(id string) string {
	e.t.Helper()
	d, err := os.ReadFile(e.file(id))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(d)
}

// mkIn creates a task in a column. A column with an entry check gets the task through the inbox:
// created, given criteria or a reason, then moved.
func mkIn(t *testing.T, s *core.Store, title, column string) string {
	t.Helper()
	return seed(t, s, title, column, false)
}

// mkCrit creates a task in a column and writes three criteria.
func mkCrit(t *testing.T, s *core.Store, title, column string) string {
	t.Helper()
	return seed(t, s, title, column, true)
}

func seed(t *testing.T, s *core.Store, title, column string, crit bool) string {
	t.Helper()
	checked := column == "open" || column == "blocked" || column == "closed"
	into := column
	if checked {
		into = "idea"
	}
	r, err := s.Create(title, "proj", into, "tester")
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	if crit || column == "open" {
		if r, err = s.Edit(r.ID, r.Rev, critBody, "tester"); err != nil {
			t.Fatalf("Edit %s: %v", r.ID, err)
		}
	}
	if checked {
		o := core.MoveOpts{}
		if column != "open" {
			o.Message, o.Force = "seed", column == "closed"
		}
		if _, err := s.Move(r.ID, column, o, "tester"); err != nil {
			t.Fatalf("Move %s to %s: %v", r.ID, column, err)
		}
	}
	return r.ID
}

func get(t *testing.T, s *core.Store, id string) *task.Task {
	t.Helper()
	tk, _, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	return tk
}

func ensure(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Errorf(format, args...)
	}
}

func wantRule(t *testing.T, what string, err error, substr string) {
	t.Helper()
	if !errors.Is(err, core.ErrRule) {
		t.Errorf("%s: error %v, want ErrRule", what, err)
		return
	}
	if substr != "" && !strings.Contains(err.Error(), substr) {
		t.Errorf("%s: text %q, want substring %q", what, err.Error(), substr)
	}
}

// lifecycle - one task claim on a fake clock; the phases follow one another.
type lifecycle struct {
	t   *testing.T
	e   *env
	clk *clock
	s   *core.Store
	id  string
}

func (l *lifecycle) ensure(ok bool, format string, args ...any) {
	l.t.Helper()
	ensure(l.t, ok, format, args...)
}

func (l *lifecycle) takeAndRefuseForeign() {
	l.t.Helper()
	r, err := l.s.Take(l.id, core.Take, "claude:a")
	if err != nil {
		l.t.Fatalf("Take: %v", err)
	}
	l.ensure(strings.Contains(r.Info, "open > in_progress"), "Info = %q, want `open > in_progress  @... until ...`", r.Info)
	l.ensure(get(l.t, l.s, l.id).Status == "in_progress" && strings.Contains(l.e.text(l.id), "claim: claude:a until 2026-10-20T15:00"),
		"after Take there is no status in_progress and a 3-hour claim:\n%s", l.e.text(l.id))
	l.ensure(l.e.subject() == l.id+": take", "commit subject %q, want %q", l.e.subject(), l.id+": take")

	// Another actor on a live claim: a refusal with the owner and a hint, the history is intact.
	n := l.e.count()
	_, err = l.s.Take(l.id, core.Take, "claude:b")
	l.ensure(errors.Is(err, core.ErrClaimed) && strings.Contains(err.Error(), "claude:a") &&
		strings.Contains(err.Error(), "--steal") && strings.Contains(err.Error(), "15:00"),
		"Take of another's = %v, want ErrClaimed with the owner, the expiry 15:00 and `--steal`", err)
	l.ensure(l.e.count() == n, "a Take refusal added a commit")
}

// extend: the owner changed the file - the expiry is now+3 h in the same commit; note and another actor do not move the expiry.
func (l *lifecycle) extend() {
	l.t.Helper()
	n := l.e.count()
	l.clk.add(time.Hour)
	if _, err := l.s.Comment(l.id, "progress", "claude:a"); err != nil {
		l.t.Fatal(err)
	}
	l.ensure(strings.Contains(l.e.text(l.id), "until 2026-10-20T16:00"), "the owner's comment did not extend the claim to 16:00:\n%s", l.e.text(l.id))
	l.ensure(l.e.count() == n+1, "the extension went in a separate commit: %d commits, want %d", l.e.count(), n+1)
	l.clk.add(30 * time.Minute)
	if _, err := l.s.Note(l.id, "step", "claude:a"); err != nil {
		l.t.Fatal(err)
	}
	if _, err := l.s.Comment(l.id, "from stranger", "claude:b"); err != nil {
		l.t.Fatal(err)
	}
	l.ensure(strings.Contains(l.e.text(l.id), "until 2026-10-20T16:00"), "note or another's comment moved the claim:\n%s", l.e.text(l.id))

	// Repeat: own take - no commit, the expiry as before.
	n = l.e.count()
	_, err := l.s.Take(l.id, core.Take, "claude:a")
	l.ensure(err == nil && l.e.count() == n && strings.Contains(l.e.text(l.id), "until 2026-10-20T16:00"),
		"repeated Take: err %v, commits %d (was %d)", err, l.e.count(), n)
}

func (l *lifecycle) stealAndRelease() {
	l.t.Helper()
	if _, err := l.s.Take(l.id, core.Steal, "claude:b"); err != nil {
		l.t.Fatalf("Steal: %v", err)
	}
	subj := l.e.subject()
	l.ensure(strings.HasPrefix(subj, l.id+": steal ") && strings.Contains(subj, "claude:a"), "steal subject %q, want `%s: steal claude:a`", subj, l.id)
	l.ensure(strings.Contains(l.e.text(l.id), "claim: claude:b"), "after Steal the owner is not b:\n%s", l.e.text(l.id))

	// Another actor cannot release; the owner releases; a repeated release - no commit.
	_, err := l.s.Take(l.id, core.Release, "claude:a")
	l.ensure(errors.Is(err, core.ErrClaimed), "Release of another's live claim = %v, want ErrClaimed", err)
	if _, err := l.s.Take(l.id, core.Release, "claude:b"); err != nil {
		l.t.Fatalf("Release by the owner: %v", err)
	}
	tk := get(l.t, l.s, l.id)
	_, ok := tk.ClaimInfo()
	l.ensure(!ok, "the claim remained after Release")
	l.ensure(tk.Status == "open", "status after Release %q, want open: active means someone works on it", tk.Status)
	l.ensure(l.e.subject() == l.id+": release", "subject %q, want %q", l.e.subject(), l.id+": release")
	n := l.e.count()
	_, err = l.s.Take(l.id, core.Release, "claude:b")
	l.ensure(err == nil && l.e.count() == n, "repeated Release: err %v, commits %d, was %d", err, l.e.count(), n)
}

// expired: an expired claim is free, take without --steal, no steal in the subject.
func (l *lifecycle) expired() {
	l.t.Helper()
	if _, err := l.s.Take(l.id, core.Take, "claude:c"); err != nil {
		l.t.Fatal(err)
	}
	l.clk.add(4 * time.Hour)
	if _, err := l.s.Take(l.id, core.Take, "claude:d"); err != nil {
		l.t.Fatalf("Take of an expired claim: %v", err)
	}
	l.ensure(l.e.subject() == l.id+": take", "subject after an expired claim %q, want %q (steal only on a live foreign claim)", l.e.subject(), l.id+": take")
}

// Race, expiry, extension; steal and release.
func TestTake_ClaimLifecycle_ExtendExpireStealRelease(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	s := e.openAt(clk)
	l := &lifecycle{t: t, e: e, clk: clk, s: s, id: mkIn(t, s, "Work", "open")}
	l.takeAndRefuseForeign()
	l.extend()
	l.stealAndRelease()
	l.expired()
}

func TestTake_Refusals_NothingChanges(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	s := e.openAt(clk)
	inbox := mkIn(t, s, "Idea", "idea")
	closed := mkIn(t, s, "Old", "closed")
	ready := mkIn(t, s, "Ready", "open")

	n := e.count()
	before := e.text(ready)
	inboxBefore := e.text(inbox)
	for _, c := range []struct{ name, id string }{{"idea", inbox}, {"closed", closed}} {
		_, err := s.Take(c.id, core.Take, "claude:a")
		wantRule(t, "Take from "+c.name, err, "tix move")
	}
	for _, who := range []string{"", "unknown"} {
		if _, err := s.Take(ready, core.Take, who); !errors.Is(err, core.ErrNoIdentity) {
			t.Errorf("Take by %q = %v, want ErrNoIdentity", who, err)
		}
	}
	if e.count() != n || e.text(ready) != before {
		t.Error("a Take refusal changed the history or the file")
	}
	// No criteria: the hint names the whole path, not a move that the ready check refuses.
	_, err := s.Take(inbox, core.Take, "claude:a")
	wantRule(t, "Take from inbox without criteria", err, "has no criteria")
	if err != nil && (!strings.Contains(err.Error(), "tix edit") || !strings.Contains(err.Error(), "tix take "+inbox)) {
		t.Errorf("text %q, want the hint `tix edit ... tix take %s`", err, inbox)
	}
	if e.count() != n || e.text(inbox) != inboxBefore {
		t.Error("Take from inbox without criteria changed the history or the file")
	}
}

func TestTake_InboxWithCriterion(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	s := e.openAt(clk)
	id := mkIn(t, s, "Planned", "idea")
	_, rev, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Edit(id, rev, "## Details\n- [ ] Ship it\n", "tester"); err != nil {
		t.Fatal(err)
	}
	n := e.count()
	r, err := s.Take(id, core.Take, "codex:a")
	if err != nil {
		t.Fatal(err)
	}
	tk := get(t, s, id)
	claim, has := tk.ClaimInfo()
	if tk.Status != "in_progress" || !has || claim.Actor != "codex:a" || !claim.Until.Equal(clk.now().Add(3*time.Hour)) {
		t.Errorf("Take from inbox: status %q, claim %+v, want in_progress with a three-hour claim", tk.Status, claim)
	}
	if e.count() != n+1 || !strings.Contains(r.Info, "idea > in_progress") {
		t.Errorf("Take from inbox: commits %d (was %d), info %q", e.count(), n, r.Info)
	}
	if _, err := s.Take(id, core.Take, "codex:a"); err != nil || e.count() != n+1 {
		t.Errorf("repeated Take: err %v, commits %d (was %d)", err, e.count(), n+1)
	}
}

// Release of a waiting task drops the claim and keeps the column.
func TestTake_Release_KeepsNonActiveColumn(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	id := mkCrit(t, s, "A", "open")
	if _, err := s.Take(id, core.Take, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(id, "blocked", core.MoveOpts{Message: "need key"}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Take(id, core.Release, "claude:a"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if tk := get(t, s, id); tk.Status != "blocked" {
		t.Errorf("status %q, want blocked", tk.Status)
	}
}

// Release by another actor of an expired claim: the stale claim goes, the task is the head of ready again.
func TestTake_Release_ExpiredForeignClaimToHeadOfReady(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	s := e.openAt(clk)
	id := mkCrit(t, s, "A", "open")
	mkCrit(t, s, "B", "open")
	if _, err := s.Take(id, core.Take, "claude:a"); err != nil {
		t.Fatal(err)
	}
	clk.add(4 * time.Hour)
	if _, err := s.Take(id, core.Release, "claude:b"); err != nil {
		t.Fatalf("Release of an expired foreign claim: %v", err)
	}
	tk := get(t, s, id)
	if _, ok := tk.ClaimInfo(); ok || tk.Status != "open" {
		t.Errorf("after Release: status %q, claim kept %v; want open without a claim", tk.Status, ok)
	}
	if got := order(t, s, "open"); len(got) == 0 || got[0] != id {
		t.Errorf("open order %v, want %s first: the released task stays next", got, id)
	}
}

// The ask field is gone: Set refuses it like any unknown field and writes nothing.
func TestSet_Ask_UnknownField(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	id := mkIn(t, s, "A", "in_progress")
	n := e.count()
	_, err := s.Set(id, []core.Field{{Name: "ask", Value: "which port?"}}, "claude:a")
	if !errors.Is(err, core.ErrRule) || !strings.Contains(err.Error(), "allowed: title project waiting tags external") {
		t.Errorf("Set ask = %v, want a rule refusal listing the allowed fields", err)
	}
	if e.count() != n {
		t.Error("a refused Set wrote a commit")
	}
}

// move refusal; waiting; repeat; leaving waiting clears the field (D52).
func TestMove_Rules(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	s := e.openAt(clk)
	bare := mkIn(t, s, "NoCriteria", "idea")
	good := mkCrit(t, s, "Good", "idea")
	act := mkCrit(t, s, "Active", "in_progress")

	n := e.count()
	_, err := s.Move(bare, "open", core.MoveOpts{}, "claude:a")
	wantRule(t, "to ready without a criterion", err, "open needs a criterion")
	_, err = s.Move(good, "review", core.MoveOpts{}, "claude:a")
	wantRule(t, "to an unknown status", err, `unknown status "review"`)
	_, err = s.Move(good, "todo", core.MoveOpts{}, "claude:a")
	wantRule(t, "to an old column word", err, "maybe open?")
	_, err = s.Move(act, "blocked", core.MoveOpts{}, "claude:a")
	wantRule(t, "to waiting without a field and -m", err, "blocked needs a reason")
	_, err = s.Move(act, "open", core.MoveOpts{Message: "x"}, "claude:a")
	wantRule(t, "-m outside waiting and closed", err, "")
	if e.count() != n {
		t.Fatalf("Move refusals added commits: %d, was %d", e.count(), n)
	}

	if _, err := s.Move(good, "open", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatalf("Move to ready with a criterion: %v", err)
	}
	if got := e.subject(); got != good+": move idea > open" {
		t.Errorf("subject %q, want `%s: move idea > open`", got, good)
	}
	// Repeat: the same column - ok without a commit.
	n = e.count()
	if _, err := s.Move(good, "open", core.MoveOpts{}, "claude:a"); err != nil || e.count() != n {
		t.Errorf("Move to the same column: err %v, commits %d, was %d", err, e.count(), n)
	}

	// waiting: -m writes the field; leaving clears it.
	if _, err := s.Move(act, "blocked", core.MoveOpts{Message: "need key"}, "claude:a"); err != nil {
		t.Fatalf("Move to waiting with -m: %v", err)
	}
	if w := get(t, s, act).Waiting; w != "need key" {
		t.Errorf("waiting field = %q, want `need key`", w)
	}
	if _, err := s.Move(act, "open", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if w := get(t, s, act).Waiting; w != "" {
		t.Errorf("after leaving waiting the field = %q, want empty", w)
	}
	// The field is already set: a move to waiting passes without -m.
	if _, err := s.Set(act, []core.Field{{Name: "waiting", Value: "from field"}}, "claude:a"); err != nil {
		t.Fatalf("Set waiting outside waiting: %v", err)
	}
	if _, err := s.Move(act, "blocked", core.MoveOpts{}, "claude:a"); err != nil {
		t.Errorf("Move to waiting with a ready field: %v", err)
	}
	// Take from waiting clears the field (D52).
	if _, err := s.Take(act, core.Take, "claude:a"); err != nil {
		t.Fatalf("Take from waiting: %v", err)
	}
	if tk := get(t, s, act); tk.Waiting != "" || tk.Status != "in_progress" {
		t.Errorf("after Take from waiting: status %q, waiting %q", tk.Status, tk.Waiting)
	}
	// Leaving active drops the claim.
	if _, err := s.Move(act, "open", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := get(t, s, act).ClaimInfo(); ok {
		t.Error("leaving active did not drop the claim")
	}
}

// Closing with unchecked criteria; done -m twice; 0 criteria (D40).
func TestMove_Close_CriteriaForceResult(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	s := e.openAt(clk)
	a := mkCrit(t, s, "A", "in_progress")
	if _, err := s.Check(a, []int{1}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	n := e.count()
	_, err := s.Move(a, "closed", core.MoveOpts{}, "claude:a")
	wantRule(t, "closing with unchecked", err, "1/3 criteria checked")
	if err != nil && !strings.Contains(err.Error(), "tix check "+a+" 2 3") {
		t.Errorf("text %q, want the hint `tix check %s 2 3`", err, a)
	}
	_, err = s.Move(a, "closed", core.MoveOpts{Force: true}, "claude:a")
	wantRule(t, "--force without -m", err, "")
	if e.count() != n {
		t.Fatalf("refusals changed the history: %d, was %d", e.count(), n)
	}
	if _, err := s.Move(a, "closed", core.MoveOpts{Force: true, Message: "skip rest"}, "claude:a"); err != nil {
		t.Fatalf("--force -m: %v", err)
	}
	if got := e.subject(); got != a+": done force" {
		t.Errorf("subject %q, want `%s: done force`", got, a)
	}
	if !strings.Contains(e.text(a), "Result: skip rest") {
		t.Errorf("no comment `Result: skip rest`:\n%s", e.text(a))
	}

	// All checked: closing without -m; the claim is dropped.
	b := mkCrit(t, s, "B", "open")
	if _, err := s.Take(b, core.Take, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(b, []int{1, 2, 3}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(b, "closed", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatalf("closing with checked: %v", err)
	}
	if got := e.subject(); got != b+": done" {
		t.Errorf("subject %q, want %q", got, b+": done")
	}
	if tk := get(t, s, b); tk.Status != "closed" {
		t.Errorf("status %q, want closed", tk.Status)
	} else if _, ok := tk.ClaimInfo(); ok {
		t.Error("closing did not drop the claim")
	}

	// Without criteria: -m is required; a repeated -m on a closed task does not write a second result.
	c := mkIn(t, s, "C", "in_progress")
	_, err = s.Move(c, "closed", core.MoveOpts{}, "claude:a")
	wantRule(t, "0 criteria without -m", err, "no criteria")
	if _, err := s.Move(c, "closed", core.MoveOpts{Message: "shipped"}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	n = e.count()
	if _, err := s.Move(c, "closed", core.MoveOpts{Message: "shipped again"}, "claude:a"); err != nil {
		t.Fatalf("repeated done: %v", err)
	}
	if e.count() != n || strings.Contains(e.text(c), "shipped again") {
		t.Errorf("repeated done: commits %d (was %d), text:\n%s", e.count(), n, e.text(c))
	}
}

// Order --first, --after (another column), renumbering.
func order(t *testing.T, s *core.Store, column string) []string {
	t.Helper()
	all, _ := s.List()
	var tasks []*task.Task
	for _, tk := range all {
		if tk.Status == column {
			tasks = append(tasks, tk)
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Rank < tasks[j].Rank })
	var ids []string
	for _, tk := range tasks {
		ids = append(ids, tk.ID)
	}
	return ids
}

func TestMove_Order_FirstAfterForeignColumn(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	a, b, c := mkIn(t, s, "A", "open"), mkIn(t, s, "B", "open"), mkIn(t, s, "C", "open")
	foreign := mkIn(t, s, "F", "idea")
	for _, id := range []string{a, b, c} {
		if _, err := s.Move(id, "in_progress", core.MoveOpts{}, "x:1"); err != nil {
			t.Fatal(err)
		}
	}
	want := func(ids ...string) {
		t.Helper()
		if got := order(t, s, "in_progress"); strings.Join(got, ",") != strings.Join(ids, ",") {
			t.Errorf("in_progress order %v, want %v", got, ids)
		}
	}
	want(a, b, c)
	d := mkIn(t, s, "D", "open")
	if _, err := s.Move(d, "in_progress", core.MoveOpts{Place: core.Place{First: true}}, "x:1"); err != nil {
		t.Fatal(err)
	}
	want(d, a, b, c)
	if _, err := s.Move(c, "in_progress", core.MoveOpts{Place: core.Place{After: a}}, "x:1"); err != nil {
		t.Fatal(err)
	}
	want(d, a, c, b)
	_, err := s.Move(b, "in_progress", core.MoveOpts{Place: core.Place{After: foreign}}, "x:1")
	wantRule(t, "--after a task from another column", err, "")
	want(d, a, c, b)

	// The same column: without a place - ok without a commit; with --first and --after the order changes by a commit (TUI, S4).
	n := e.count()
	if _, err := s.Move(b, "in_progress", core.MoveOpts{}, "x:1"); err != nil || e.count() != n {
		t.Errorf("move to the same column without a place: err %v, commits %d, was %d", err, e.count(), n)
	}
	want(d, a, c, b)
	if _, err := s.Move(b, "in_progress", core.MoveOpts{Place: core.Place{First: true}}, "x:1"); err != nil {
		t.Fatalf("--first in the same column: %v", err)
	}
	if e.count() != n+1 {
		t.Errorf("--first in the same column: commits %d, want %d", e.count(), n+1)
	}
	want(b, d, a, c)
	if _, err := s.Move(b, "in_progress", core.MoveOpts{Place: core.Place{After: a}}, "x:1"); err != nil {
		t.Fatalf("--after in the same column: %v", err)
	}
	if e.count() != n+2 {
		t.Errorf("--after in the same column: commits %d, want %d", e.count(), n+2)
	}
	want(d, a, b, c)
}

func TestMove_100InsertsAfterSameAnchor_OrderStaysTotal(t *testing.T) {
	if testing.Short() {
		t.Skip("long tier: 100 commits")
	}
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	anchor := mkIn(t, s, "Anchor", "in_progress")
	var ids []string
	for i := 0; i < 100; i++ {
		ids = append(ids, mkIn(t, s, fmt.Sprintf("N%d", i), "open"))
	}
	for _, id := range ids {
		if _, err := s.Move(id, "in_progress", core.MoveOpts{Place: core.Place{After: anchor}}, "x:1"); err != nil {
			t.Fatalf("Move %s after %s: %v", id, anchor, err)
		}
	}
	want := []string{anchor}
	for i := len(ids) - 1; i >= 0; i-- {
		want = append(want, ids[i])
	}
	got := order(t, s, "in_progress")
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order after 100 inserts is broken:\n%v\nwant:\n%v", got, want)
	}
	all, errs := s.List()
	if len(errs) != 0 {
		t.Fatalf("after renumbering the files do not parse: %v", errs)
	}
	seen := map[float64]bool{}
	for _, tk := range all {
		if tk.Status == "in_progress" && seen[tk.Rank] {
			t.Fatalf("rank %v repeats: the order holds by chance", tk.Rank)
		}
		seen[tk.Rank] = true
	}
}

// set status=; Set rules.
func TestSet_Rules(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	s := e.openAt(clk)
	id := mkIn(t, s, "Work", "open")
	w := mkIn(t, s, "Wait", "open")
	if _, err := s.Move(w, "blocked", core.MoveOpts{Message: "reason"}, "x:1"); err != nil {
		t.Fatal(err)
	}
	n, before := e.count(), e.text(id)
	for _, name := range []string{"status", "claim", "rank"} {
		_, err := s.Set(id, []core.Field{{Name: name, Value: "x"}}, "x:1")
		wantRule(t, "Set "+name, err, "tix move")
	}
	for _, name := range []string{"id", "created", "nosuch", "due"} {
		if _, err := s.Set(id, []core.Field{{Name: name, Value: "x"}}, "x:1"); err == nil {
			t.Errorf("Set %s accepted", name)
		}
	}
	if _, err := s.Set(id, []core.Field{{Name: "nosuch", Value: "x"}}, "x:1"); err == nil || !strings.Contains(err.Error(), "allowed:") {
		t.Errorf("Set of an unknown field: %v, want the hint `allowed: ...`", err)
	}
	_, err := s.Set(id, []core.Field{{Name: "title", Value: ""}}, "x:1")
	wantRule(t, "empty title", err, "")
	_, err = s.Set(w, []core.Field{{Name: "waiting", Value: ""}}, "x:1")
	wantRule(t, "waiting= in waiting", err, "")
	if e.count() != n || e.text(id) != before {
		t.Fatal("Set refusals changed the history or the file")
	}

	if _, err := s.Set(id, []core.Field{{Name: "title", Value: "New"}, {Name: "tags", Value: "a,b"}, {Name: "abstract", Value: "x=y"}}, "x:1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	tk := get(t, s, id)
	if tk.Title != "New" || strings.Join(tk.Tags, ",") != "a,b" || !strings.Contains(tk.Body, "x=y") {
		t.Errorf("after Set: title %q tags %v body %q", tk.Title, tk.Tags, tk.Body)
	}
	subj := e.subject()
	if !strings.HasPrefix(subj, id+": set ") || !strings.Contains(subj, "title") || !strings.Contains(subj, "abstract") {
		t.Errorf("subject %q, want `%s: set title tags abstract`", subj, id)
	}
	n = e.count()
	_, err = s.Set(id, []core.Field{{Name: "summary", Value: "x"}}, "x:1")
	wantRule(t, "summary is gone", err, "")
	if _, err := s.Set(id, []core.Field{{Name: "title", Value: "New"}}, "x:1"); err != nil || e.count() != n {
		t.Errorf("Set with the same value: err %v, commits %d, was %d", err, e.count(), n)
	}
}

func TestCheck_InfoSubjectRepeatBadNumber(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	id := mkCrit(t, s, "Work", "in_progress")
	r, err := s.Check(id, []int{1, 2}, "x:1")
	if err != nil || r.Info != "2/3" {
		t.Fatalf("Check 1 2: info %q, err %v; want `2/3`", r.Info, err)
	}
	if got := e.subject(); got != id+": check 1 2" {
		t.Errorf("subject %q, want %q", got, id+": check 1 2")
	}
	n, before := e.count(), e.text(id)
	if _, err := s.Check(id, []int{1}, "x:1"); err != nil || e.count() != n {
		t.Errorf("repeated Check: err %v, commits %d, was %d", err, e.count(), n)
	}
	if _, err := s.Check(id, []int{9}, "x:1"); err == nil {
		t.Error("Check of a nonexistent criterion accepted")
	}
	if e.count() != n || e.text(id) != before {
		t.Error("a Check refusal changed the history or the file")
	}
}

// Every 300 commits gc starts in the background after the lock. Replacement - a git wrapper:
// gc and commit-graph do not run but are written to gc.log; the threshold is checked with a margin.
func TestGC_EveryThreeHundredCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("long tier: 320 commits")
	}
	defer guard(t)
	e := newEnv(t)
	script := "#!/bin/sh\nD=" + e.fake + "\nfor a in \"$@\"; do case \"$a\" in gc|commit-graph) echo \"$a\" >> \"$D/gc.log\"; exit 0;; esac; done\n" +
		"exec " + gitBin + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(e.fake, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := e.openAt(newClock())
	id := mkIn(t, s, "Work", "open")
	gcLog := func() string { d, _ := os.ReadFile(filepath.Join(e.fake, "gc.log")); return string(d) }
	for i := 2; i <= 250; i++ {
		if _, err := s.Note(id, fmt.Sprintf("step %d", i), "x:1"); err != nil {
			t.Fatalf("Note %d: %v", i, err)
		}
	}
	time.Sleep(200 * time.Millisecond) // the background run could start: a negative result waits a bit longer
	if got := gcLog(); got != "" {
		t.Fatalf("gc started before the threshold (250 commits): %q", got)
	}
	for i := 251; i <= 320; i++ {
		if _, err := s.Note(id, fmt.Sprintf("step %d", i), "x:1"); err != nil {
			t.Fatalf("Note %d: %v", i, err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && (!strings.Contains(gcLog(), "gc") || !strings.Contains(gcLog(), "commit-graph")) {
		time.Sleep(50 * time.Millisecond)
	}
	got := gcLog()
	if strings.Count(got, "gc\n") != 1 || !strings.Contains(got, "commit-graph") {
		t.Errorf("after 320 commits gc.log = %q, want one gc and commit-graph", got)
	}
}

// move to blocked with -m; an old word and an empty name are refused with the five statuses named.
func TestMove_Blocked_OldWordsRefused(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	a := mkCrit(t, s, "A", "in_progress")
	_, err := s.Move(a, "waiting", core.MoveOpts{Message: "waiting for a reply"}, "claude:a")
	wantRule(t, "old role word", err, "maybe blocked?")
	if _, err := s.Move(a, "blocked", core.MoveOpts{Message: "waiting for a reply"}, "claude:a"); err != nil {
		t.Fatalf("move %s blocked: %v", a, err)
	}
	got, _, err := s.Get(a)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "blocked" {
		t.Errorf("status %q, want blocked", got.Status)
	}
	if subj := e.subject(); !strings.Contains(subj, "move in_progress > blocked") {
		t.Errorf("commit subject %q, want the column name: move in_progress > blocked", subj)
	}
	_, err = s.Move(a, "nowhere", core.MoveOpts{}, "claude:a")
	wantRule(t, "unknown name", err, `unknown status "nowhere"; statuses: idea open in_progress blocked closed`)
	_, err = s.Move(a, "", core.MoveOpts{}, "claude:a")
	wantRule(t, "empty name", err, `unknown status ""`)
}

// ReleaseAll: all live claims of the actor in one commit; foreign and expired claims stay.
func TestReleaseAll_OwnLiveClaimsOneCommit(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	s := e.openAt(clk)
	head := mkIn(t, s, "Head", "open")
	a1 := mkIn(t, s, "A1", "open")
	a2 := mkIn(t, s, "A2", "open")
	foreign := mkIn(t, s, "Foreign", "open")
	old := mkIn(t, s, "Old", "open")
	if _, err := s.Take(old, core.Take, "claude:a"); err != nil {
		t.Fatal(err)
	}
	clk.add(2 * time.Hour)
	for id, who := range map[string]string{a1: "claude:a", a2: "claude:a", foreign: "claude:b"} {
		if _, err := s.Take(id, core.Take, who); err != nil {
			t.Fatal(err)
		}
	}
	clk.add(time.Hour + 30*time.Minute) // old expired (3.5 h), the others live

	n := e.count()
	ids, res, err := s.ReleaseAll("claude:a")
	if err != nil {
		t.Fatalf("ReleaseAll: %v", err)
	}
	ensure(t, strings.Join(ids, " ") == a1+" "+a2, "released %v, want [%s %s]", ids, a1, a2)
	ensure(t, e.count() == n+1 && res.Commit != "", "commits %d -> %d, want one", n, e.count())
	for _, id := range []string{a1, a2} {
		tk := get(t, s, id)
		ensure(t, tk.Status == "open", "%s status %s, want open", id, tk.Status)
		_, has := tk.ClaimInfo()
		ensure(t, !has, "%s keeps a claim", id)
	}
	ensure(t, get(t, s, foreign).Status == "in_progress", "foreign claim was touched")
	ensure(t, strings.Contains(e.text(old), "claim: claude:a"), "expired claim was touched")
	ensure(t, get(t, s, old).Status == "in_progress", "expired task was moved")
	// In the head of open, before the untouched task.
	ensure(t, get(t, s, a1).Rank < get(t, s, head).Rank && get(t, s, a2).Rank < get(t, s, head).Rank,
		"released tasks are not in the head of open")

	n = e.count()
	ids, _, err = s.ReleaseAll("claude:a")
	ensure(t, err == nil && len(ids) == 0 && e.count() == n, "repeat: %v %v, commits %d -> %d", ids, err, n, e.count())
}
