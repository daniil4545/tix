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

	"tracker/internal/core"
	"tracker/internal/task"
)

// Рабочий цикл агента, З2: docs/plans/z2-agent-cycle.md, разделы 3 и 5.

const (
	critBody = "## Details\n- [ ] one\n- [ ] two\n- [ ] three\n"
	// reviewConfig: колонка review без роли; остальные роли как по умолчанию.
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

// withConfig пишет config.toml и открывает базу заново.
func (e *env) withConfig(c *clock, text string) *core.Store {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.root, ".tracker", "config.toml"), []byte(text), 0o644); err != nil {
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

// mkCrit создаёт задачу в колонке и записывает три критерия.
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
		t.Errorf("%s: ошибка %v, ждали ErrRule", what, err)
		return
	}
	if substr != "" && !strings.Contains(err.Error(), substr) {
		t.Errorf("%s: текст %q, ждали подстроку %q", what, err.Error(), substr)
	}
}

// lifecycle - один захват задачи на фейковых часах; фазы идут друг за другом.
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
	l.ensure(strings.Contains(r.Info, "todo > doing"), "Info = %q, ждали `todo > doing  @... until ...`", r.Info)
	l.ensure(get(l.t, l.s, l.id).Status == "doing" && strings.Contains(l.e.text(l.id), "claim: claude:a until 2026-10-20T15:00"),
		"после Take нет статуса doing и claim на 3 часа:\n%s", l.e.text(l.id))
	l.ensure(l.e.subject() == l.id+": take", "тема коммита %q, ждали %q", l.e.subject(), l.id+": take")

	// Чужой на живой claim: отказ с владельцем и подсказкой, история цела.
	n := l.e.count()
	_, err = l.s.Take(l.id, core.Take, "claude:b")
	l.ensure(errors.Is(err, core.ErrClaimed) && strings.Contains(err.Error(), "claude:a") &&
		strings.Contains(err.Error(), "--steal") && strings.Contains(err.Error(), "15:00"),
		"Take чужого = %v, ждали ErrClaimed с владельцем, сроком 15:00 и `--steal`", err)
	l.ensure(l.e.count() == n, "отказ Take добавил коммит")
}

// extend: владелец изменил файл - срок now+3 ч в том же коммите; note и чужой не двигают срок.
func (l *lifecycle) extend() {
	l.t.Helper()
	n := l.e.count()
	l.clk.add(time.Hour)
	if _, err := l.s.Comment(l.id, "progress", "claude:a"); err != nil {
		l.t.Fatal(err)
	}
	l.ensure(strings.Contains(l.e.text(l.id), "until 2026-10-20T16:00"), "comment владельца не продлил claim до 16:00:\n%s", l.e.text(l.id))
	l.ensure(l.e.count() == n+1, "продление ушло отдельным коммитом: %d коммитов, ждали %d", l.e.count(), n+1)
	l.clk.add(30 * time.Minute)
	if _, err := l.s.Note(l.id, "step", "claude:a"); err != nil {
		l.t.Fatal(err)
	}
	if _, err := l.s.Comment(l.id, "from stranger", "claude:b"); err != nil {
		l.t.Fatal(err)
	}
	l.ensure(strings.Contains(l.e.text(l.id), "until 2026-10-20T16:00"), "note или чужой comment сдвинули claim:\n%s", l.e.text(l.id))

	// Повтор: свой take - без коммита, срок прежний.
	n = l.e.count()
	_, err := l.s.Take(l.id, core.Take, "claude:a")
	l.ensure(err == nil && l.e.count() == n && strings.Contains(l.e.text(l.id), "until 2026-10-20T16:00"),
		"повторный Take: err %v, коммитов %d (было %d)", err, l.e.count(), n)
}

func (l *lifecycle) stealAndRelease() {
	l.t.Helper()
	if _, err := l.s.Take(l.id, core.Steal, "claude:b"); err != nil {
		l.t.Fatalf("Steal: %v", err)
	}
	subj := l.e.subject()
	l.ensure(strings.HasPrefix(subj, l.id+": steal ") && strings.Contains(subj, "claude:a"), "тема перехвата %q, ждали `%s: steal claude:a`", subj, l.id)
	l.ensure(strings.Contains(l.e.text(l.id), "claim: claude:b"), "после Steal владелец не b:\n%s", l.e.text(l.id))

	// Чужой снять не может; владелец снимает; повтор снятия - без коммита.
	_, err := l.s.Take(l.id, core.Release, "claude:a")
	l.ensure(errors.Is(err, core.ErrClaimed), "Release чужого живого claim = %v, ждали ErrClaimed", err)
	if _, err := l.s.Take(l.id, core.Release, "claude:b"); err != nil {
		l.t.Fatalf("Release владельца: %v", err)
	}
	_, ok := get(l.t, l.s, l.id).ClaimInfo()
	l.ensure(!ok, "после Release claim остался")
	l.ensure(l.e.subject() == l.id+": release", "тема %q, ждали %q", l.e.subject(), l.id+": release")
	n := l.e.count()
	_, err = l.s.Take(l.id, core.Release, "claude:b")
	l.ensure(err == nil && l.e.count() == n, "повторный Release: err %v, коммитов %d, было %d", err, l.e.count(), n)
}

// expired: истёкший claim свободен, take без --steal, тема без steal.
func (l *lifecycle) expired() {
	l.t.Helper()
	if _, err := l.s.Take(l.id, core.Take, "claude:c"); err != nil {
		l.t.Fatal(err)
	}
	l.clk.add(4 * time.Hour)
	if _, err := l.s.Take(l.id, core.Take, "claude:d"); err != nil {
		l.t.Fatalf("Take истёкшего claim: %v", err)
	}
	l.ensure(l.e.subject() == l.id+": take", "тема после истёкшего claim %q, ждали %q (steal только при живом чужом)", l.e.subject(), l.id+": take")
}

// Гонка, истёк, продление; перехват и снятие (раздел 3, строки 1 и 4).
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
	for _, c := range []struct{ name, id string }{{"inbox", inbox}, {"closed", closed}, {"колонка без роли", norole}} {
		_, err := s.Take(c.id, core.Take, "claude:a")
		wantRule(t, "Take из "+c.name, err, "tk move")
	}
	for _, who := range []string{"", "unknown"} {
		if _, err := s.Take(ready, core.Take, who); !errors.Is(err, core.ErrNoIdentity) {
			t.Errorf("Take от %q = %v, ждали ErrNoIdentity", who, err)
		}
	}
	if e.count() != n || e.text(ready) != before {
		t.Error("отказ Take изменил историю или файл")
	}
}

// Раздел 3: move отказ; waiting; повтор; выход из waiting чистит поле (Р52).
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
	wantRule(t, "в ready без критерия", err, "ready needs a criterion")
	_, err = s.Move(good, "review", core.MoveOpts{}, "claude:a")
	wantRule(t, "в колонку без роли", err, "review has no role")
	_, err = s.Move(norole, "todo", core.MoveOpts{}, "claude:a")
	wantRule(t, "из колонки без роли", err, "review has no role")
	_, err = s.Move(act, "blocked", core.MoveOpts{}, "claude:a")
	wantRule(t, "в waiting без поля и -m", err, "waiting needs a reason")
	_, err = s.Move(act, "todo", core.MoveOpts{Message: "x"}, "claude:a")
	wantRule(t, "-m вне waiting и closed", err, "")
	if e.count() != n {
		t.Fatalf("отказы Move добавили коммиты: %d, было %d", e.count(), n)
	}

	if _, err := s.Move(good, "todo", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatalf("Move с критерием в ready: %v", err)
	}
	if got := e.subject(); got != good+": move idea > todo" {
		t.Errorf("тема %q, ждали `%s: move idea > todo`", got, good)
	}
	// Повтор: та же колонка - ok без коммита.
	n = e.count()
	if _, err := s.Move(good, "todo", core.MoveOpts{}, "claude:a"); err != nil || e.count() != n {
		t.Errorf("Move в ту же колонку: err %v, коммитов %d, было %d", err, e.count(), n)
	}

	// waiting: -m пишет поле; выход чистит его.
	if _, err := s.Move(act, "blocked", core.MoveOpts{Message: "need key"}, "claude:a"); err != nil {
		t.Fatalf("Move в waiting с -m: %v", err)
	}
	if w := get(t, s, act).Waiting; w != "need key" {
		t.Errorf("поле waiting = %q, ждали `need key`", w)
	}
	if _, err := s.Move(act, "todo", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if w := get(t, s, act).Waiting; w != "" {
		t.Errorf("после выхода из waiting поле = %q, ждали пусто", w)
	}
	// Поле уже стоит: перенос в waiting проходит без -m.
	if _, err := s.Set(act, []core.Field{{Name: "waiting", Value: "from field"}}, "claude:a"); err != nil {
		t.Fatalf("Set waiting вне waiting: %v", err)
	}
	if _, err := s.Move(act, "blocked", core.MoveOpts{}, "claude:a"); err != nil {
		t.Errorf("Move в waiting с готовым полем: %v", err)
	}
	// Take из waiting чистит поле (Р52).
	if _, err := s.Take(act, core.Take, "claude:a"); err != nil {
		t.Fatalf("Take из waiting: %v", err)
	}
	if tk := get(t, s, act); tk.Waiting != "" || tk.Status != "doing" {
		t.Errorf("после Take из waiting: статус %q, waiting %q", tk.Status, tk.Waiting)
	}
	// Выход из active снимает claim.
	if _, err := s.Move(act, "todo", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := get(t, s, act).ClaimInfo(); ok {
		t.Error("выход из active не снял claim")
	}
}

// Раздел 3: закрытие с неотмеченными; done -m дважды; 0 критериев (Р40).
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
	wantRule(t, "закрытие с неотмеченными", err, "1/3 criteria checked")
	if err != nil && !strings.Contains(err.Error(), "tk check "+a+" 2 3") {
		t.Errorf("текст %q, ждали подсказку `tk check %s 2 3`", err, a)
	}
	_, err = s.Move(a, "done", core.MoveOpts{Force: true}, "claude:a")
	wantRule(t, "--force без -m", err, "")
	if e.count() != n {
		t.Fatalf("отказы изменили историю: %d, было %d", e.count(), n)
	}
	if _, err := s.Move(a, "done", core.MoveOpts{Force: true, Message: "skip rest"}, "claude:a"); err != nil {
		t.Fatalf("--force -m: %v", err)
	}
	if got := e.subject(); got != a+": done force" {
		t.Errorf("тема %q, ждали `%s: done force`", got, a)
	}
	if !strings.Contains(e.text(a), "Result: skip rest") {
		t.Errorf("нет комментария `Result: skip rest`:\n%s", e.text(a))
	}

	// Все отмечены: закрытие без -m; claim снят.
	b := mkCrit(t, s, "B", "todo")
	if _, err := s.Take(b, core.Take, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(b, []int{1, 2, 3}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(b, "done", core.MoveOpts{}, "claude:a"); err != nil {
		t.Fatalf("закрытие с отмеченными: %v", err)
	}
	if got := e.subject(); got != b+": done" {
		t.Errorf("тема %q, ждали %q", got, b+": done")
	}
	if tk := get(t, s, b); tk.Status != "done" {
		t.Errorf("статус %q, ждали done", tk.Status)
	} else if _, ok := tk.ClaimInfo(); ok {
		t.Error("закрытие не сняло claim")
	}

	// Без критериев: нужен -m; повтор -m на закрытой не пишет второй итог.
	c := mkIn(t, s, "C", "doing")
	_, err = s.Move(c, "done", core.MoveOpts{}, "claude:a")
	wantRule(t, "0 критериев без -m", err, "no criteria")
	if _, err := s.Move(c, "done", core.MoveOpts{Message: "shipped"}, "claude:a"); err != nil {
		t.Fatal(err)
	}
	n = e.count()
	if _, err := s.Move(c, "done", core.MoveOpts{Message: "shipped again"}, "claude:a"); err != nil {
		t.Fatalf("повторный done: %v", err)
	}
	if e.count() != n || strings.Contains(e.text(c), "shipped again") {
		t.Errorf("повтор done: коммитов %d (было %d), текст:\n%s", e.count(), n, e.text(c))
	}
}

// Раздел 3: порядок --first, --after (чужая колонка), перенумерация.
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
			t.Errorf("порядок doing %v, ждали %v", got, ids)
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
	wantRule(t, "--after задачи из другой колонки", err, "")
	want(d, a, c, b)

	// Та же колонка: без места - ok без коммита; с --first и --after порядок меняется коммитом (TUI, З4).
	n := e.count()
	if _, err := s.Move(b, "doing", core.MoveOpts{}, "x:1"); err != nil || e.count() != n {
		t.Errorf("перенос в ту же колонку без места: err %v, коммитов %d, было %d", err, e.count(), n)
	}
	want(d, a, c, b)
	if _, err := s.Move(b, "doing", core.MoveOpts{Place: core.Place{First: true}}, "x:1"); err != nil {
		t.Fatalf("--first в той же колонке: %v", err)
	}
	if e.count() != n+1 {
		t.Errorf("--first в той же колонке: коммитов %d, ждали %d", e.count(), n+1)
	}
	want(b, d, a, c)
	if _, err := s.Move(b, "doing", core.MoveOpts{Place: core.Place{After: a}}, "x:1"); err != nil {
		t.Fatalf("--after в той же колонке: %v", err)
	}
	if e.count() != n+2 {
		t.Errorf("--after в той же колонке: коммитов %d, ждали %d", e.count(), n+2)
	}
	want(d, a, b, c)
}

func TestMove_100InsertsAfterSameAnchor_OrderStaysTotal(t *testing.T) {
	if testing.Short() {
		t.Skip("долгий ярус: 100 коммитов")
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
		t.Fatalf("порядок после 100 вставок нарушен:\n%v\nждали:\n%v", got, want)
	}
	all, errs := s.List()
	if len(errs) != 0 {
		t.Fatalf("после перенумерации файлы не разбираются: %v", errs)
	}
	seen := map[float64]bool{}
	for _, tk := range all {
		if tk.Status == "doing" && seen[tk.Rank] {
			t.Fatalf("rank %v повторяется: порядок держится случайно", tk.Rank)
		}
		seen[tk.Rank] = true
	}
}

// Раздел 3: set status=; правила Set.
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
		wantRule(t, "Set "+name, err, "tk move")
	}
	for _, name := range []string{"id", "created", "nosuch"} {
		if _, err := s.Set(id, []core.Field{{Name: name, Value: "x"}}, "x:1"); err == nil {
			t.Errorf("Set %s принят", name)
		}
	}
	if _, err := s.Set(id, []core.Field{{Name: "nosuch", Value: "x"}}, "x:1"); err == nil || !strings.Contains(err.Error(), "allowed:") {
		t.Errorf("Set неизвестного поля: %v, ждали подсказку `allowed: ...`", err)
	}
	_, err := s.Set(id, []core.Field{{Name: "title", Value: ""}}, "x:1")
	wantRule(t, "пустой title", err, "")
	_, err = s.Set(w, []core.Field{{Name: "waiting", Value: ""}}, "x:1")
	wantRule(t, "waiting= в waiting", err, "")
	if e.count() != n || e.text(id) != before {
		t.Fatal("отказы Set изменили историю или файл")
	}

	if _, err := s.Set(id, []core.Field{{Name: "title", Value: "New"}, {Name: "tags", Value: "a,b"}, {Name: "summary", Value: "x=y"}}, "x:1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	tk := get(t, s, id)
	if tk.Title != "New" || strings.Join(tk.Tags, ",") != "a,b" || tk.Summary() != "x=y" {
		t.Errorf("после Set: title %q tags %v summary %q", tk.Title, tk.Tags, tk.Summary())
	}
	subj := e.subject()
	if !strings.HasPrefix(subj, id+": set ") || !strings.Contains(subj, "title") || !strings.Contains(subj, "summary") {
		t.Errorf("тема %q, ждали `%s: set title tags summary`", subj, id)
	}
	n = e.count()
	if _, err := s.Set(id, []core.Field{{Name: "title", Value: "New"}}, "x:1"); err != nil || e.count() != n {
		t.Errorf("Set тем же значением: err %v, коммитов %d, было %d", err, e.count(), n)
	}
}

func TestCheck_InfoSubjectRepeatBadNumber(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	id := mkCrit(t, s, "Work", "doing")
	r, err := s.Check(id, []int{1, 2}, "x:1")
	if err != nil || r.Info != "2/3" {
		t.Fatalf("Check 1 2: info %q, err %v; ждали `2/3`", r.Info, err)
	}
	if got := e.subject(); got != id+": check 1 2" {
		t.Errorf("тема %q, ждали %q", got, id+": check 1 2")
	}
	n, before := e.count(), e.text(id)
	if _, err := s.Check(id, []int{1}, "x:1"); err != nil || e.count() != n {
		t.Errorf("повторный Check: err %v, коммитов %d, было %d", err, e.count(), n)
	}
	if _, err := s.Check(id, []int{9}, "x:1"); err == nil {
		t.Error("Check несуществующего критерия принят")
	}
	if e.count() != n || e.text(id) != before {
		t.Error("отказ Check изменил историю или файл")
	}
}

// Backup: bare, правка мимо ядра, битый путь, нет origin, нет базы.
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
		t.Fatalf("HEAD bare %q != локальный %q", remote, local)
	}

	// Правка мимо ядра: Backup коммитит её и отправляет.
	f := e.file(id)
	d, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(d, []byte("edited in Obsidian\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(ctx); err != nil {
		t.Fatalf("Backup с внешней правкой: %v", err)
	}
	if st := strings.TrimSpace(e.git("status", "--porcelain")); st != "" {
		t.Errorf("после Backup дерево не чисто:\n%s", st)
	}
	if local, remote := e.bareMatchesLocal(bare); local != remote {
		t.Errorf("HEAD bare %q != локальный %q после внешней правки", remote, local)
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
		t.Errorf("Backup без origin = %v, ждали ErrNoRemote с подсказкой `git remote add origin <url>`", err)
	}

	// Битый путь: ошибка, но правка мимо ядра всё равно закоммичена (лок отпущен до push).
	e.git("remote", "add", "origin", filepath.Join(e.root, "no", "such.git"))
	f := e.file(id)
	d, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(d, []byte("outside\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(ctx); err == nil || errors.Is(err, core.ErrNoRemote) {
		t.Errorf("Backup в битый путь = %v, ждали ошибку push", err)
	}
	if st := strings.TrimSpace(e.git("status", "--porcelain")); st != "" {
		t.Errorf("при сбое push правка не закоммичена:\n%s", st)
	}

	// user:pass@ в тексте ошибки вырезается.
	e.git("remote", "set-url", "origin", "http://user:s3cretpw@127.0.0.1:1/r.git")
	_, err := s.Backup(ctx)
	if err == nil {
		t.Fatal("Backup на недоступный адрес прошёл")
	}
	if strings.Contains(err.Error(), "s3cretpw") {
		t.Errorf("пароль remote в тексте ошибки: %v", err)
	}
}

func TestBackup_NoBase_Error(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	if _, err := s.Backup(context.Background()); err == nil {
		t.Error("Backup без базы прошёл")
	}
}

// Раздел 8: каждые 300 коммитов после лока в фоне запускается gc. Подмена - обёртка git:
// gc и commit-graph не выполняются, а пишутся в gc.log; порог проверяется с запасом.
func TestGC_EveryThreeHundredCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("долгий ярус: 320 коммитов")
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
	time.Sleep(200 * time.Millisecond) // фон мог бы стартовать: отрицательный результат ждёт чуть дольше
	if got := gcLog(); got != "" {
		t.Fatalf("gc запущен до порога (250 коммитов): %q", got)
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
		t.Errorf("после 320 коммитов gc.log = %q, ждали один gc и commit-graph", got)
	}
}
