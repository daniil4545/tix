package core_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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
	// reviewConfig: the review column without a role; the other roles as by default.
	reviewConfig = "[[column]]\nname = \"idea\"\nrole = \"inbox\"\n\n[[column]]\nname = \"todo\"\nrole = \"ready\"\n\n" +
		"[[column]]\nname = \"review\"\n\n[[column]]\nname = \"doing\"\nrole = \"active\"\n\n" +
		"[[column]]\nname = \"blocked\"\nrole = \"waiting\"\n\n[[column]]\nname = \"done\"\nrole = \"closed\"\n"
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

// withConfig writes config.toml and opens the store again.
func (e *env) withConfig(c *clock, text string) *core.Store {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.root, ".tix", "config.toml"), []byte(text), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return e.openAt(c)
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

func mkIn(t *testing.T, s *core.Store, title, column string) string {
	t.Helper()
	r, err := s.Create(title, "proj", column, "tester")
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	return r.ID
}

// mkCrit creates a task in a column and writes three criteria.
func mkCrit(t *testing.T, s *core.Store, title, column string) string {
	t.Helper()
	r, err := s.Create(title, "proj", column, "tester")
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	if _, err := s.Edit(r.ID, r.Rev, critBody, "tester"); err != nil {
		t.Fatalf("Edit %s: %v", r.ID, err)
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
	l.ensure(strings.Contains(r.Info, "todo > doing"), "Info = %q, want `todo > doing  @... until ...`", r.Info)
	l.ensure(get(l.t, l.s, l.id).Status == "doing" && strings.Contains(l.e.text(l.id), "claim: claude:a until 2026-10-20T15:00"),
		"after Take there is no status doing and a 3-hour claim:\n%s", l.e.text(l.id))
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
	l.ensure(tk.Status == "todo", "status after Release %q, want todo: active means someone works on it", tk.Status)
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
	l := &lifecycle{t: t, e: e, clk: clk, s: s, id: mkIn(t, s, "Work", "todo")}
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
	closed := mkIn(t, s, "Old", "done")
	s = e.withConfig(clk, reviewConfig)
	norole := mkIn(t, s, "Review", "review")
	ready := mkIn(t, s, "Ready", "todo")

	n := e.count()
	before := e.text(ready)
	for _, c := range []struct{ name, id string }{{"inbox", inbox}, {"closed", closed}, {"column without a role", norole}} {
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
	crit := mkCrit(t, s, "Planned", "idea")
	_, err = s.Take(crit, core.Take, "claude:a")
	if err == nil || strings.Contains(err.Error(), "no criteria") {
		t.Errorf("Take from inbox with criteria = %v, want the short hint `tix move`", err)
	}
}

// Release of a waiting task drops the claim and keeps the column.
func TestTake_Release_KeepsNonActiveColumn(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	id := mkCrit(t, s, "A", "todo")
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
	id := mkCrit(t, s, "A", "todo")
	mkCrit(t, s, "B", "todo")
	if _, err := s.Take(id, core.Take, "claude:a"); err != nil {
		t.Fatal(err)
	}
	clk.add(4 * time.Hour)
	if _, err := s.Take(id, core.Release, "claude:b"); err != nil {
		t.Fatalf("Release of an expired foreign claim: %v", err)
	}
	tk := get(t, s, id)
	if _, ok := tk.ClaimInfo(); ok || tk.Status != "todo" {
		t.Errorf("after Release: status %q, claim kept %v; want todo without a claim", tk.Status, ok)
	}
	if got := order(t, s, "todo"); len(got) == 0 || got[0] != id {
		t.Errorf("todo order %v, want %s first: the released task stays next", got, id)
	}
}

// Closing clears ask: a closed task does not keep the yellow star.
func TestMove_Close_ClearsAsk(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	id := mkIn(t, s, "A", "doing")
	if _, err := s.Set(id, []core.Field{{Name: "ask", Value: "which port?"}}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(id, "blocked", core.MoveOpts{Message: "need key"}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if tk := get(t, s, id); tk.Ask != "which port?" {
		t.Errorf("ask after a move to blocked %q, want it kept", tk.Ask)
	}
	if _, err := s.Move(id, "done", core.MoveOpts{Message: "shipped"}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if tk := get(t, s, id); tk.Ask != "" {
		t.Errorf("ask after done %q, want empty", tk.Ask)
	}
}

// move refusal; waiting; repeat; leaving waiting clears the field (D52).
func TestMove_Rules(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	s := e.openAt(clk)
	bare := mkIn(t, s, "NoCriteria", "idea")
	s = e.withConfig(clk, reviewConfig)
	norole := mkIn(t, s, "NoRole", "review")
	good := mkCrit(t, s, "Good", "idea")
	act := mkCrit(t, s, "Active", "doing")

	n := e.count()
	_, err := s.Move(bare, "todo", core.MoveOpts{}, "claude:a")
	wantRule(t, "to ready without a criterion", err, "ready needs a criterion")
	_, err = s.Move(good, "review", core.MoveOpts{}, "claude:a")
	wantRule(t, "to a column without a role", err, "review has no role")
	_, err = s.Move(norole, "todo", core.MoveOpts{}, "claude:a")
	wantRule(t, "from a column without a role", err, "review has no role")
	_, err = s.Move(act, "blocked", core.MoveOpts{}, "claude:a")
	wantRule(t, "to waiting without a field and -m", err, "waiting needs a reason")
	_, err = s.Move(act, "todo", core.MoveOpts{Message: "x"}, "claude:a")
	wantRule(t, "-m outside waiting and closed", err, "")
	if e.count() != n {
		t.Fatalf("Move refusals added commits: %d, was %d", e.count(), n)
	}

	if _, err := s.Move(good, "todo", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatalf("Move to ready with a criterion: %v", err)
	}
	if got := e.subject(); got != good+": move idea > todo" {
		t.Errorf("subject %q, want `%s: move idea > todo`", got, good)
	}
	// Repeat: the same column - ok without a commit.
	n = e.count()
	if _, err := s.Move(good, "todo", core.MoveOpts{}, "claude:a"); err != nil || e.count() != n {
		t.Errorf("Move to the same column: err %v, commits %d, was %d", err, e.count(), n)
	}

	// waiting: -m writes the field; leaving clears it.
	if _, err := s.Move(act, "blocked", core.MoveOpts{Message: "need key"}, "claude:a"); err != nil {
		t.Fatalf("Move to waiting with -m: %v", err)
	}
	if w := get(t, s, act).Waiting; w != "need key" {
		t.Errorf("waiting field = %q, want `need key`", w)
	}
	if _, err := s.Move(act, "todo", core.MoveOpts{}, "claude:a"); err != nil {
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
	if tk := get(t, s, act); tk.Waiting != "" || tk.Status != "doing" {
		t.Errorf("after Take from waiting: status %q, waiting %q", tk.Status, tk.Waiting)
	}
	// Leaving active drops the claim.
	if _, err := s.Move(act, "todo", core.MoveOpts{}, "claude:a"); err != nil {
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
	a := mkCrit(t, s, "A", "doing")
	if _, err := s.Check(a, []int{1}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	n := e.count()
	_, err := s.Move(a, "done", core.MoveOpts{}, "claude:a")
	wantRule(t, "closing with unchecked", err, "1/3 criteria checked")
	if err != nil && !strings.Contains(err.Error(), "tix check "+a+" 2 3") {
		t.Errorf("text %q, want the hint `tix check %s 2 3`", err, a)
	}
	_, err = s.Move(a, "done", core.MoveOpts{Force: true}, "claude:a")
	wantRule(t, "--force without -m", err, "")
	if e.count() != n {
		t.Fatalf("refusals changed the history: %d, was %d", e.count(), n)
	}
	if _, err := s.Move(a, "done", core.MoveOpts{Force: true, Message: "skip rest"}, "claude:a"); err != nil {
		t.Fatalf("--force -m: %v", err)
	}
	if got := e.subject(); got != a+": done force" {
		t.Errorf("subject %q, want `%s: done force`", got, a)
	}
	if !strings.Contains(e.text(a), "Result: skip rest") {
		t.Errorf("no comment `Result: skip rest`:\n%s", e.text(a))
	}

	// All checked: closing without -m; the claim is dropped.
	b := mkCrit(t, s, "B", "todo")
	if _, err := s.Take(b, core.Take, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(b, []int{1, 2, 3}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(b, "done", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatalf("closing with checked: %v", err)
	}
	if got := e.subject(); got != b+": done" {
		t.Errorf("subject %q, want %q", got, b+": done")
	}
	if tk := get(t, s, b); tk.Status != "done" {
		t.Errorf("status %q, want done", tk.Status)
	} else if _, ok := tk.ClaimInfo(); ok {
		t.Error("closing did not drop the claim")
	}

	// Without criteria: -m is required; a repeated -m on a closed task does not write a second result.
	c := mkIn(t, s, "C", "doing")
	_, err = s.Move(c, "done", core.MoveOpts{}, "claude:a")
	wantRule(t, "0 criteria without -m", err, "no criteria")
	if _, err := s.Move(c, "done", core.MoveOpts{Message: "shipped"}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	n = e.count()
	if _, err := s.Move(c, "done", core.MoveOpts{Message: "shipped again"}, "claude:a"); err != nil {
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
	a, b, c := mkIn(t, s, "A", "todo"), mkIn(t, s, "B", "todo"), mkIn(t, s, "C", "todo")
	foreign := mkIn(t, s, "F", "idea")
	for _, id := range []string{a, b, c} {
		if _, err := s.Move(id, "doing", core.MoveOpts{}, "x:1"); err != nil {
			t.Fatal(err)
		}
	}
	want := func(ids ...string) {
		t.Helper()
		if got := order(t, s, "doing"); strings.Join(got, ",") != strings.Join(ids, ",") {
			t.Errorf("doing order %v, want %v", got, ids)
		}
	}
	want(a, b, c)
	d := mkIn(t, s, "D", "todo")
	if _, err := s.Move(d, "doing", core.MoveOpts{Place: core.Place{First: true}}, "x:1"); err != nil {
		t.Fatal(err)
	}
	want(d, a, b, c)
	if _, err := s.Move(c, "doing", core.MoveOpts{Place: core.Place{After: a}}, "x:1"); err != nil {
		t.Fatal(err)
	}
	want(d, a, c, b)
	_, err := s.Move(b, "doing", core.MoveOpts{Place: core.Place{After: foreign}}, "x:1")
	wantRule(t, "--after a task from another column", err, "")
	want(d, a, c, b)

	// The same column: without a place - ok without a commit; with --first and --after the order changes by a commit (TUI, S4).
	n := e.count()
	if _, err := s.Move(b, "doing", core.MoveOpts{}, "x:1"); err != nil || e.count() != n {
		t.Errorf("move to the same column without a place: err %v, commits %d, was %d", err, e.count(), n)
	}
	want(d, a, c, b)
	if _, err := s.Move(b, "doing", core.MoveOpts{Place: core.Place{First: true}}, "x:1"); err != nil {
		t.Fatalf("--first in the same column: %v", err)
	}
	if e.count() != n+1 {
		t.Errorf("--first in the same column: commits %d, want %d", e.count(), n+1)
	}
	want(b, d, a, c)
	if _, err := s.Move(b, "doing", core.MoveOpts{Place: core.Place{After: a}}, "x:1"); err != nil {
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
	anchor := mkIn(t, s, "Anchor", "doing")
	var ids []string
	for i := 0; i < 100; i++ {
		ids = append(ids, mkIn(t, s, fmt.Sprintf("N%d", i), "todo"))
	}
	for _, id := range ids {
		if _, err := s.Move(id, "doing", core.MoveOpts{Place: core.Place{After: anchor}}, "x:1"); err != nil {
			t.Fatalf("Move %s after %s: %v", id, anchor, err)
		}
	}
	want := []string{anchor}
	for i := len(ids) - 1; i >= 0; i-- {
		want = append(want, ids[i])
	}
	got := order(t, s, "doing")
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order after 100 inserts is broken:\n%v\nwant:\n%v", got, want)
	}
	all, errs := s.List()
	if len(errs) != 0 {
		t.Fatalf("after renumbering the files do not parse: %v", errs)
	}
	seen := map[float64]bool{}
	for _, tk := range all {
		if tk.Status == "doing" && seen[tk.Rank] {
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
	id := mkIn(t, s, "Work", "todo")
	w := mkIn(t, s, "Wait", "todo")
	if _, err := s.Move(w, "blocked", core.MoveOpts{Message: "reason"}, "x:1"); err != nil {
		t.Fatal(err)
	}
	n, before := e.count(), e.text(id)
	for _, name := range []string{"status", "claim", "rank"} {
		_, err := s.Set(id, []core.Field{{Name: name, Value: "x"}}, "x:1")
		wantRule(t, "Set "+name, err, "tix move")
	}
	for _, name := range []string{"id", "created", "nosuch"} {
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

	if _, err := s.Set(id, []core.Field{{Name: "title", Value: "New"}, {Name: "tags", Value: "a,b"}, {Name: "summary", Value: "x=y"}}, "x:1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	tk := get(t, s, id)
	if tk.Title != "New" || strings.Join(tk.Tags, ",") != "a,b" || tk.Summary() != "x=y" {
		t.Errorf("after Set: title %q tags %v summary %q", tk.Title, tk.Tags, tk.Summary())
	}
	subj := e.subject()
	if !strings.HasPrefix(subj, id+": set ") || !strings.Contains(subj, "title") || !strings.Contains(subj, "summary") {
		t.Errorf("subject %q, want `%s: set title tags summary`", subj, id)
	}
	n = e.count()
	if _, err := s.Set(id, []core.Field{{Name: "title", Value: "New"}}, "x:1"); err != nil || e.count() != n {
		t.Errorf("Set with the same value: err %v, commits %d, was %d", err, e.count(), n)
	}
}

func TestCheck_InfoSubjectRepeatBadNumber(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	id := mkCrit(t, s, "Work", "doing")
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

// Backup: bare, an edit around the core, a broken path, no origin, no store.
func newBareDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.CommandContext(context.Background(), gitBin, "init", "--bare", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("init --bare: %v\n%s", err, out)
	}
	return dir
}

func (e *env) bareMatchesLocal(bare string) (local, remote string) {
	e.t.Helper()
	local = strings.TrimSpace(e.git("rev-parse", "HEAD"))
	br := strings.TrimSpace(e.git("symbolic-ref", "--short", "HEAD"))
	out, _ := exec.CommandContext(context.Background(), gitBin, "-C", bare, "rev-parse", "refs/heads/"+br).CombinedOutput()
	return local, strings.TrimSpace(string(out))
}

func TestBackup_PushesHeadAfterCommittingOutsideEdit(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	id := mkIn(t, s, "Work", "todo")
	bare := newBareDir(t)
	e.git("remote", "add", "origin", bare)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := s.Backup(ctx); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if local, remote := e.bareMatchesLocal(bare); local != remote {
		t.Fatalf("HEAD bare %q != local %q", remote, local)
	}

	// An edit around the core: Backup commits it and pushes.
	f := e.file(id)
	d, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(d, []byte("edited in Obsidian\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(ctx); err != nil {
		t.Fatalf("Backup with an external edit: %v", err)
	}
	if st := strings.TrimSpace(e.git("status", "--porcelain")); st != "" {
		t.Errorf("after Backup the tree is not clean:\n%s", st)
	}
	if local, remote := e.bareMatchesLocal(bare); local != remote {
		t.Errorf("HEAD bare %q != local %q after an external edit", remote, local)
	}
}

func TestBackup_NoOriginBadPathBadCredentials_ErrorsOneKind(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	id := mkIn(t, s, "Work", "todo")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := s.Backup(ctx); !errors.Is(err, core.ErrNoRemote) || !strings.Contains(err.Error(), "git remote add origin") {
		t.Errorf("Backup without origin = %v, want ErrNoRemote with the hint `git remote add origin <url>`", err)
	}

	// A broken path: an error, but the edit around the core is still committed (the lock is released before push).
	e.git("remote", "add", "origin", filepath.Join(e.root, "no", "such.git"))
	f := e.file(id)
	d, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(d, []byte("outside\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(ctx); err == nil || errors.Is(err, core.ErrNoRemote) {
		t.Errorf("Backup to a broken path = %v, want a push error", err)
	}
	if st := strings.TrimSpace(e.git("status", "--porcelain")); st != "" {
		t.Errorf("on a push failure the edit is not committed:\n%s", st)
	}

	// user:pass@ in the error text is cut out.
	e.git("remote", "set-url", "origin", "http://user:s3cretpw@127.0.0.1:1/r.git")
	_, err := s.Backup(ctx)
	if err == nil {
		t.Fatal("Backup to an unreachable address passed")
	}
	if strings.Contains(err.Error(), "s3cretpw") {
		t.Errorf("remote password in the error text: %v", err)
	}
}

func TestBackup_NoBase_Error(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	if _, err := s.Backup(context.Background()); err == nil {
		t.Error("Backup without a store passed")
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
	id := mkIn(t, s, "Work", "todo")
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

// move by role name: there is no waiting column, there is blocked with role waiting - the task goes to blocked.
func TestMove_RoleName_GoesToColumnWithRole(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	a := mkCrit(t, s, "A", "doing")
	if _, err := s.Move(a, "waiting", core.MoveOpts{Message: "waiting for a reply"}, "claude:a"); err != nil {
		t.Fatalf("move %s waiting: %v", a, err)
	}
	got, _, err := s.Get(a)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "blocked" {
		t.Errorf("status %q, want blocked", got.Status)
	}
	if subj := e.subject(); !strings.Contains(subj, "move doing > blocked") {
		t.Errorf("commit subject %q, want the column name: move doing > blocked", subj)
	}
	_, err = s.Move(a, "nowhere", core.MoveOpts{}, "claude:a")
	wantRule(t, "unknown name", err, `unknown column "nowhere"; columns: idea=inbox todo=ready`)
	_, err = s.Move(a, "", core.MoveOpts{}, "claude:a")
	wantRule(t, "empty name", err, `unknown column ""`)
}

// A column name outranks a role: column "waiting" with role ready takes the task, not blocked.
func TestMove_ColumnNameBeatsRole(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	clk := newClock()
	mkIn(t, e.openAt(clk), "Seed", "idea")
	s := e.withConfig(clk, "[[column]]\nname = \"idea\"\nrole = \"inbox\"\n\n[[column]]\nname = \"waiting\"\nrole = \"ready\"\n\n"+
		"[[column]]\nname = \"doing\"\nrole = \"active\"\n\n[[column]]\nname = \"blocked\"\nrole = \"waiting\"\n\n"+
		"[[column]]\nname = \"done\"\nrole = \"closed\"\n")
	a := mkCrit(t, s, "A", "doing")
	if _, err := s.Move(a, "waiting", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatalf("move %s waiting: %v", a, err)
	}
	if got, _, _ := s.Get(a); got == nil || got.Status != "waiting" {
		t.Errorf("the task did not go to column waiting: %+v", got)
	}
}
