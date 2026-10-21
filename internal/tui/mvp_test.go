package tui

// Тесты доводки MVP (docs/plans/mvp-finish.md, разделы 3, 4.A, 4.B, 4.C, 5).
// Пакет внутренний: часы модели (поле now) недоступны снаружи. Свой мини-стенд с префиксом mv:
// общий стенд лежит в пакете tui_test. Среда (HOME, git) - TestMain из harness_test.go.
// Пишутся по спеке, без чтения кода зон A и B.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"tracker/internal/core"
)

var mvANSI = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

// раскладка раздела 3
const (
	mvColStep  = 24 + 2
	mvFirstRow = 4
	mvCardRows = 5
)

func mvX(slot int) int { return slot*mvColStep + 1 }
func mvY(row int) int  { return mvFirstRow + row*mvCardRows + 1 }

const (
	mvCfgWork = `[[column]]
name = "work"
role = "active"

[[column]]
name = "wait"
role = "waiting"

[[column]]
name = "fin"
role = "closed"
`
)

type mvPing struct{}

type mvProbe struct {
	tea.Model
	e *mvEnv
}

func (p mvProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := p.Model.Update(msg)
	p.Model = m
	switch msg.(type) {
	case tea.MouseMsg:
		p.e.mouseSeen.Add(1)
	case mvPing:
		p.e.pingSeen.Add(1)
		p.e.viewsAtPing.Store(p.e.views.Load())
	}
	return p, cmd
}

func (p mvProbe) View() tea.View {
	v := p.Model.View()
	p.e.mu.Lock()
	p.e.screen = mvANSI.ReplaceAllString(v.Content, "")
	p.e.mu.Unlock()
	p.e.views.Add(1)
	return v
}

type mvEnv struct {
	t     *testing.T
	root  string
	store *core.Store
	model *Model
	tm    *teatest.TestModel

	mu     sync.Mutex
	screen string

	clock                                             atomic.Int64
	mouseSent                                         int
	mouseSeen, pingSeen, pingSent, views, viewsAtPing atomic.Int64
}

func mvGit(t *testing.T) string {
	t.Helper()
	g, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("git not found")
	}
	return g
}

func mvNewEnv(t *testing.T, config string) *mvEnv {
	t.Helper()
	return mvNewEnvGit(t, config, mvGit(t))
}

func mvNewEnvGit(t *testing.T, config, gitBin string) *mvEnv {
	t.Helper()
	base, err := os.MkdirTemp("", "tk-mvp-")
	if err != nil {
		t.Fatal(err)
	}
	// очередь записи может дописывать коммит, пока тест уже вышел: ждём, пока git отпустит
	// лок индекса, и удаляем с повтором до срока
	t.Cleanup(func() {
		lock := filepath.Join(base, "tasks", ".git", "index.lock")
		for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(lock); err == nil {
				continue
			}
			if os.RemoveAll(base) == nil {
				return
			}
		}
		t.Errorf("не удалось убрать %s", base)
	})
	root := filepath.Join(base, "tasks")
	if config != "" {
		if err := os.MkdirAll(filepath.Join(root, ".tracker"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".tracker", "config.toml"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := core.Open(root, core.Options{GitBin: gitBin})
	if err != nil {
		t.Fatal(err)
	}
	e := &mvEnv{t: t, root: root, store: s}
	e.clock.Store(time.Now().UnixNano())
	return e
}

func (e *mvEnv) create(title, column string) string {
	e.t.Helper()
	r, err := e.store.Create(title, "p", column, "seed")
	if err != nil {
		e.t.Fatalf("Create(%q): %v", title, err)
	}
	return r.ID
}

func (e *mvEnv) set(id, name, value string) {
	e.t.Helper()
	if _, err := e.store.Set(id, []core.Field{{Name: name, Value: value}}, "seed"); err != nil {
		e.t.Fatalf("Set %s %s: %v", id, name, err)
	}
}

func (e *mvEnv) summary(id, text string) {
	e.t.Helper()
	_, rev, err := e.store.Get(id)
	if err == nil {
		_, err = e.store.Edit(id, rev, "## Summary\n"+text+"\n", "seed")
	}
	if err != nil {
		e.t.Fatalf("summary %s: %v", id, err)
	}
}

func (e *mvEnv) note(id, text string) {
	e.t.Helper()
	if _, err := e.store.Note(id, text, "agent"); err != nil {
		e.t.Fatalf("Note %s: %v", id, err)
	}
}

// setClock подставляет время модели. Вызывать, когда все посланные события обработаны (mouse и settle ждут).
func (e *mvEnv) setClock(t time.Time) { e.clock.Store(t.UnixNano()) }

func (e *mvEnv) start(w, h int) {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.t.Cleanup(cancel)
	e.model = New(ctx, Deps{Store: e.store, Root: e.root, Actor: "human"})
	e.model.now = func() time.Time { return time.Unix(0, e.clock.Load()) }
	e.tm = teatest.NewTestModel(e.t, mvProbe{Model: e.model, e: e}, teatest.WithInitialTermSize(w, h))
	e.t.Cleanup(func() {
		_ = e.tm.Quit()
		e.tm.WaitFinished(e.t, teatest.WithFinalTimeout(5*time.Second))
	})
}

func (e *mvEnv) text() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.screen
}

func (e *mvEnv) lines() []string { return strings.Split(e.text(), "\n") }

// yOf - строка экрана с первым вхождением sub; -1, если нет.
func (e *mvEnv) yOf(sub string) int {
	for y, ln := range e.lines() {
		if strings.Contains(ln, sub) {
			return y
		}
	}
	return -1
}

func (e *mvEnv) until(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("не дождались: %s\nэкран:\n%s", what, e.text())
}

func (e *mvEnv) screenHas(subs ...string) {
	e.t.Helper()
	e.until(fmt.Sprint("на экране ", subs), func() bool {
		s := e.text()
		for _, sub := range subs {
			if !strings.Contains(s, sub) {
				return false
			}
		}
		return true
	})
}

// settle ждёт, пока модель обработает всё посланное ранее и экран перерисован после этого.
func (e *mvEnv) settle() {
	e.t.Helper()
	want := e.pingSent.Add(1)
	e.tm.Send(mvPing{})
	e.until("пинг обработан", func() bool { return e.pingSeen.Load() >= want })
	base := e.viewsAtPing.Load()
	e.until("кадр после пинга", func() bool { return e.views.Load() > base })
}

func (e *mvEnv) key(keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			e.tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			e.tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
		default:
			for _, r := range k {
				e.tm.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
}

// mouse шлёт событие мыши и ждёт, пока модель его обработает (часы можно менять после возврата).
func (e *mvEnv) mouse(msg tea.MouseMsg) {
	e.t.Helper()
	e.mouseSent++
	e.tm.Send(msg)
	want := int64(e.mouseSent)
	e.until("событие мыши обработано", func() bool { return e.mouseSeen.Load() >= want })
}

func mvM(x, y int, b tea.MouseButton) tea.Mouse { return tea.Mouse{X: x, Y: y, Button: b} }

// click - нажатие и отпускание левой в одной точке.
func (e *mvEnv) click(x, y int) {
	e.t.Helper()
	e.mouse(tea.MouseClickMsg(mvM(x, y, tea.MouseLeft)))
	e.mouse(tea.MouseReleaseMsg(mvM(x, y, tea.MouseLeft)))
}

func (e *mvEnv) wheel(x, y int, down bool) {
	e.t.Helper()
	b := tea.MouseWheelUp
	if down {
		b = tea.MouseWheelDown
	}
	e.mouse(tea.MouseWheelMsg(mvM(x, y, b)))
}

// drag - нажатие левой в (x1,y1), движение и отпускание в (x2,y2).
func (e *mvEnv) press(x, y int) { e.t.Helper(); e.mouse(tea.MouseClickMsg(mvM(x, y, tea.MouseLeft))) }
func (e *mvEnv) release(x, y int) {
	e.t.Helper()
	e.mouse(tea.MouseMotionMsg(mvM(x, y, tea.MouseLeft)))
	e.mouse(tea.MouseReleaseMsg(mvM(x, y, tea.MouseLeft)))
}

func (e *mvEnv) status(id string) (status, waiting string) {
	e.t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		e.t.Fatalf("Get %s: %v", id, err)
	}
	return tk.Status, tk.Waiting
}

func (e *mvEnv) git(args ...string) string {
	e.t.Helper()
	out, err := exec.CommandContext(e.t.Context(), mvGit(e.t), append([]string{"-C", e.root}, args...)...).CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (e *mvEnv) commits() string { return e.git("rev-list", "--count", "HEAD") }

// opened: открыта карточка задачи title, доски нет.
func (e *mvEnv) opened(title string, others ...string) {
	e.t.Helper()
	e.until("открыта карточка "+title, func() bool {
		s := e.text()
		if !strings.Contains(s, title) {
			return false
		}
		for _, o := range others {
			if strings.Contains(s, o) {
				return false
			}
		}
		return true
	})
}

// onBoard: на экране доска со всеми перечисленными карточками (проверка "карточка не открылась").
func (e *mvEnv) onBoard(titles ...string) {
	e.t.Helper()
	e.settle()
	s := e.text()
	for _, ti := range append([]string{"enter open"}, titles...) {
		if !strings.Contains(s, ti) {
			e.t.Fatalf("ждали доску с %q, экран:\n%s", ti, s)
		}
	}
}

// selectedIs открывает выбранную карточку клавишей enter и проверяет её, затем возвращается на доску.
func (e *mvEnv) selectedIs(title string, others ...string) {
	e.t.Helper()
	e.settle()
	e.key("enter")
	e.opened(title, others...)
	e.key("esc")
	e.screenHas("enter open")
}

// awaitWatcher ждёт, пока наблюдатель запущен (он стартует после первого кадра): правит заглавие
// служебной задачи, пока доска не покажет новое. Без этого внешняя правка сразу после start теряется.
func (e *mvEnv) awaitWatcher(canary string) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for i := 0; time.Now().Before(deadline); i++ {
		title := fmt.Sprintf("canary%d", i)
		e.set(canary, "title", title)
		for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if strings.Contains(e.text(), title) {
				return
			}
		}
	}
	e.t.Fatalf("наблюдатель не заметил правку файла\nэкран:\n%s", e.text())
}

// ---------------------------------------------------------------------------------------------
// Режим мыши
// ---------------------------------------------------------------------------------------------

// 4.B: режим мыши - CellMotion на доске и в карточке.
func TestMouse_BoardAndCard_CellMotionMode(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := New(ctx, Deps{Store: e.store, Root: e.root, Actor: "human"})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("доска: MouseMode=%v, ждали CellMotion", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.showing {
		t.Fatal("enter не открыл карточку")
	}
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("карточка: MouseMode=%v, ждали CellMotion", got)
	}
}

// 4.C.5: клик выбирает карточку (колонка и строка); любая из пяти строк карточки и оба края колонки - её же.
func TestMouse_Click_SelectsCardByGrid(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	e.create("Charlie", "todo")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo", "Charlie")

	cases := []struct {
		name, want string
		x, y       int
	}{
		{"другая колонка, строка названия", "Charlie", mvX(1), mvY(0)},
		{"вторая карточка, пятая (пустая) строка", "Bravo", 3, mvFirstRow + mvCardRows + 4},
		{"первая карточка, правый край колонки", "Alpha", mvColStep*0 + 23, mvFirstRow},
		{"левый край второй колонки", "Charlie", mvColStep, mvFirstRow + 2},
	}
	for _, c := range cases {
		e.click(c.x, c.y)
		e.selectedIs(c.want)
		if t.Failed() {
			t.Fatalf("случай %q: клик (%d,%d)", c.name, c.x, c.y)
		}
	}
}

// 4.B: клик по пустому месту, промежутку, шапке; правая и средняя кнопки; отпускание без нажатия - ничего.
func TestMouse_NonActions_KeepSelectionAndCommits(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.key("j") // выбран Bravo
	e.settle()
	before := e.commits()

	steps := []struct {
		name string
		do   func()
	}{
		{"промежуток между колонками", func() { e.click(24, mvY(0)) }},
		{"промежуток, второй знак", func() { e.click(25, mvY(0)) }},
		{"шапка", func() { e.click(5, 0) }},
		{"линия под шапкой", func() { e.click(5, 1) }},
		{"пусто под карточками", func() { e.click(3, mvY(2)) }},
		{"пустая колонка", func() { e.click(mvX(4), mvY(0)) }},
		{"правая кнопка на Alpha", func() {
			e.mouse(tea.MouseClickMsg(mvM(3, mvY(0), tea.MouseRight)))
			e.mouse(tea.MouseReleaseMsg(mvM(3, mvY(0), tea.MouseRight)))
		}},
		{"средняя кнопка на Alpha", func() {
			e.mouse(tea.MouseClickMsg(mvM(3, mvY(0), tea.MouseMiddle)))
			e.mouse(tea.MouseReleaseMsg(mvM(3, mvY(0), tea.MouseMiddle)))
		}},
		{"отпускание без нажатия на Alpha", func() { e.mouse(tea.MouseReleaseMsg(mvM(3, mvY(0), tea.MouseLeft))) }},
	}
	for _, s := range steps {
		s.do()
		e.settle()
		if !strings.Contains(e.text(), "enter open") {
			t.Fatalf("%s: доска пропала, экран:\n%s", s.name, e.text())
		}
	}
	e.selectedIs("Bravo", "Alpha")
	if after := e.commits(); after != before {
		t.Errorf("мышь без действия записала коммиты: %s -> %s", before, after)
	}
}

// 4.C.5 и риск 2: двойной клик открывает только ту же карточку и только в пределах 400 мс по часам модели.
func TestMouse_DoubleClick_ClockAndSameCard(t *testing.T) {
	type click struct {
		after time.Duration // пауза после предыдущего клика
		slot  int           // колонка
		row   int
	}
	cases := []struct {
		name   string
		clicks []click
		open   string // "" - карточка не открылась
		sel    string // выбранная в итоге
	}{
		{"те же клики за 100 мс", []click{{0, 0, 0}, {100 * time.Millisecond, 0, 0}}, "Alpha", "Alpha"},
		{"пауза 500 мс", []click{{0, 0, 0}, {500 * time.Millisecond, 0, 0}}, "", "Alpha"},
		{"пауза 3 с", []click{{0, 0, 0}, {3 * time.Second, 0, 0}}, "", "Alpha"},
		{"две разные карточки за 50 мс", []click{{0, 0, 0}, {50 * time.Millisecond, 0, 1}}, "", "Bravo"},
		{"карточки в разных колонках за 50 мс", []click{{0, 0, 0}, {50 * time.Millisecond, 1, 0}}, "", "Charlie"},
		{"A, B, A быстро", []click{{0, 0, 0}, {50 * time.Millisecond, 0, 1}, {50 * time.Millisecond, 0, 0}}, "", "Alpha"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := mvNewEnv(t, "")
			e.create("Alpha", "idea")
			e.create("Bravo", "idea")
			e.create("Charlie", "todo")
			e.start(140, 30)
			e.screenHas("Alpha", "Bravo", "Charlie")
			now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
			e.setClock(now)
			for _, k := range c.clicks {
				now = now.Add(k.after)
				e.setClock(now)
				e.click(mvX(k.slot), mvY(k.row))
			}
			if c.open != "" {
				e.opened(c.open)
				return
			}
			e.onBoard("Alpha", "Bravo", "Charlie")
			others := []string{"Alpha", "Bravo", "Charlie"}
			e.selectedIs(c.sel, func() []string {
				var o []string
				for _, x := range others {
					if x != c.sel {
						o = append(o, x)
					}
				}
				return o
			}()...)
		})
	}
}

// 4.C.5 и риск 1: колёсико в прокрученной колонке двигает выбор на одну карточку, а не по позиции указателя.
func TestMouse_Wheel_ScrolledColumn_StepsOneCard(t *testing.T) {
	e := mvNewEnv(t, "")
	for i := 1; i <= 12; i++ {
		e.create(fmt.Sprintf("Card%02d", i), "idea")
	}
	e.start(140, 24)
	e.screenHas("Card01")
	e.key("j", "j", "j", "j") // Card05: окно колонки прокручено
	e.settle()
	e.selectedIs("Card05")

	steps := []struct {
		down bool
		want string
	}{{true, "Card06"}, {false, "Card05"}, {false, "Card04"}, {true, "Card05"}}
	for _, s := range steps {
		e.wheel(3, mvFirstRow+1, s.down)
		e.selectedIs(s.want)
		if t.Failed() {
			t.Fatalf("после колёсика down=%v ждали выбор %s", s.down, s.want)
		}
	}
}

// 4.B: колёсико над невыбранной колонкой - сначала выбор колонки, затем шаг.
func TestMouse_Wheel_OtherColumn_SelectsColumnThenSteps(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Beta1", "todo")
	e.create("Beta2", "todo")
	e.create("Beta3", "todo")
	e.start(140, 30)
	e.screenHas("Alpha", "Beta3")
	e.wheel(mvX(1), mvY(0), true)
	e.selectedIs("Beta2", "Alpha", "Beta1", "Beta3")
}

// 4.B: на экране карточки колёсико вниз и вверх = j и k; клик ничего не делает.
func TestMouse_CardScreen_WheelScrollsLikeJK_ClickIgnored(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	for i := 1; i <= 30; i++ {
		if _, err := e.store.Comment("T-1", fmt.Sprintf("remark-%02d", i), "agent"); err != nil {
			t.Fatal(err)
		}
	}
	e.start(100, 14)
	e.screenHas("Alpha")
	e.key("enter")
	e.opened("Alpha")
	e.screenHas("remark-")
	e.settle()
	s0 := e.text()

	e.key(strings.Repeat("k", 15)) // история открывается на новейшей записи: k уходит вверх
	e.settle()
	s1 := e.text()
	if s1 == s0 {
		t.Fatal("15 нажатий k не сдвинули историю: сценарий не проверяет прокрутку")
	}
	for i := 0; i < 15; i++ {
		e.wheel(10, 6, true) // вниз = j
	}
	e.settle()
	if got := e.text(); got != s0 {
		t.Errorf("15 раз колёсико вниз не вернуло экран к началу (как 15 j):\n%s\nждали:\n%s", got, s0)
	}
	for i := 0; i < 15; i++ {
		e.wheel(10, 6, false) // вверх = k
	}
	e.settle()
	if got := e.text(); got != s1 {
		t.Errorf("15 раз колёсико вверх не повторило 15 раз k:\n%s\nждали:\n%s", got, s1)
	}
	e.click(10, 6)
	e.settle()
	if got := e.text(); got != s1 {
		t.Errorf("клик в карточке изменил экран:\n%s", got)
	}
}

// 4.C.5: перетаскивание в waiting спрашивает причину; esc - задача на месте; ввод - задача перенесена.
func TestMouse_Drag_ToWaiting_AsksReason(t *testing.T) {
	t.Run("esc оставляет задачу на месте", func(t *testing.T) {
		e := mvNewEnv(t, mvCfgWork)
		e.create("Alpha", "work")
		e.create("Bravo", "work")
		e.start(140, 30)
		e.screenHas("Alpha", "Bravo")
		before := e.commits()
		e.press(3, mvY(0))
		e.release(mvX(1)+2, mvY(0))
		e.screenHas("waiting for:")
		e.key("esc")
		e.until("строка ввода закрыта", func() bool { return !strings.Contains(e.text(), "waiting for:") })
		if st, _ := e.status("T-1"); st != "work" || e.commits() != before {
			t.Errorf("отмена изменила задачу: статус %s, коммитов %s -> %s", st, before, e.commits())
		}
	})
	t.Run("причина и enter переносят", func(t *testing.T) {
		e := mvNewEnv(t, mvCfgWork)
		e.create("Alpha", "work")
		e.create("Bravo", "work")
		e.start(140, 30)
		e.screenHas("Alpha", "Bravo")
		e.press(3, mvY(1)) // Bravo
		e.release(mvX(1), mvY(0))
		e.screenHas("waiting for:")
		e.tm.Type("vendor reply")
		e.key("enter")
		e.until("T-2 в wait с причиной", func() bool { st, w := e.status("T-2"); return st == "wait" && w == "vendor reply" })
		if st, _ := e.status("T-1"); st != "work" {
			t.Errorf("T-1 сдвинута вместе с T-2: %s", st)
		}
	})
}

// 4.C.5: отпускание над той же колонкой или вне колонок - переноса нет; правая кнопка не тащит.
func TestMouse_Drag_NoTarget_NoCommit(t *testing.T) {
	cases := []struct {
		name               string
		pressX             int
		pressY             int
		releaseX, releaseY int
		btn                tea.MouseButton
	}{
		{"та же колонка, другая карточка", 3, mvY(0), 3, mvY(1), tea.MouseLeft},
		{"промежуток между колонками", 3, mvY(0), 25, mvY(0), tea.MouseLeft},
		{"шапка", 3, mvY(0), mvX(1), 0, tea.MouseLeft},
		{"строка состояния под колонками", 3, mvY(0), mvX(1), 28, tea.MouseLeft},
		{"правая кнопка в другую колонку", 3, mvY(0), mvX(1), mvY(0), tea.MouseRight},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := mvNewEnv(t, mvCfgWork)
			e.create("Alpha", "work")
			e.create("Bravo", "work")
			e.start(140, 30)
			e.screenHas("Alpha", "Bravo")
			before := e.commits()
			e.mouse(tea.MouseClickMsg(mvM(c.pressX, c.pressY, c.btn)))
			e.mouse(tea.MouseReleaseMsg(mvM(c.releaseX, c.releaseY, c.btn)))
			e.settle()
			if strings.Contains(e.text(), "waiting for:") || strings.Contains(e.text(), "close reason:") {
				t.Errorf("появилась строка ввода переноса:\n%s", e.text())
			}
			for _, id := range []string{"T-1", "T-2"} {
				if st, _ := e.status(id); st != "work" {
					t.Errorf("%s перенесена в %s", id, st)
				}
			}
			if after := e.commits(); after != before {
				t.Errorf("коммиты %s -> %s", before, after)
			}
		})
	}
}

// 4.B: задачи уже нет к моменту отпускания - T-N removed, коммитов от нас нет.
func TestMouse_Drag_TaskRemovedMeanwhile_ReportsRemoved(t *testing.T) {
	e := mvNewEnv(t, mvCfgWork)
	e.create("Alpha", "work")
	e.create("Bravo", "work")
	canary := e.create("canary", "fin")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.awaitWatcher(canary)
	e.press(3, mvY(0))
	if _, err := e.store.Remove("T-1", "agent"); err != nil {
		t.Fatal(err)
	}
	e.until("Alpha ушла с доски", func() bool { return !strings.Contains(e.text(), "Alpha") })
	after := e.commits()
	e.release(mvX(1), mvY(0))
	e.screenHas("T-1 removed")
	if e.commits() != after {
		t.Errorf("перенос удалённой задачи записал коммит")
	}
	if st, _ := e.status("T-2"); st != "work" {
		t.Errorf("T-2 перенесена вместо удалённой: %s", st)
	}
}

// Риск 1: переносится задача, запомненная при нажатии, а не та, что оказалась на месте к отпусканию.
func TestMouse_Drag_BoardShiftsBetweenPressAndRelease_MovesPressedTask(t *testing.T) {
	e := mvNewEnv(t, mvCfgWork)
	e.create("Alpha", "work")
	e.create("Bravo", "work")
	canary := e.create("canary", "fin")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.awaitWatcher(canary)
	e.press(3, mvY(1)) // Bravo
	if _, err := e.store.Move("T-1", "fin", core.MoveOpts{Message: "done by agent", Force: true}, "agent"); err != nil {
		t.Fatal(err)
	}
	e.until("Bravo поднялась на первое место", func() bool {
		ls := e.lines()
		return len(ls) > mvY(0) && strings.Contains(ls[mvY(0)], "Bravo")
	})
	e.release(mvX(1), mvY(0))
	e.screenHas("waiting for:")
	e.tm.Type("why")
	e.key("enter")
	e.until("T-2 в wait", func() bool { st, _ := e.status("T-2"); return st == "wait" })
	if st, _ := e.status("T-1"); st != "fin" {
		t.Errorf("T-1: %s, ждали fin (не должна двигаться)", st)
	}
}

var mvDefaultCols = []string{"idea", "todo", "doing", "blocked", "done"}

// visibleColumns - имена колонок по умолчанию из шапки (строка 2), в порядке на экране.
func (e *mvEnv) visibleColumns() []string {
	hdr := e.lines()[2]
	var names []string
	for x := 0; x < len(hdr); x++ {
		for _, n := range mvDefaultCols {
			if strings.HasPrefix(hdr[x:], n+" ") && (x == 0 || hdr[x-1] == ' ') {
				names = append(names, n)
			}
		}
	}
	return names
}

// 4.C.6 и риск 1: окно колонок сдвинуто (ширина 80, курсор на дальней колонке) - попадание по слоту окна.
// Окно зависит от курсора, поэтому после каждого действия шапка читается заново: ждём то, что нарисовано.
func TestMouse_ColumnWindowShifted_ClickAndDragUseVisibleSlots(t *testing.T) {
	ids := map[string]string{}
	setup := func(t *testing.T) *mvEnv {
		e := mvNewEnv(t, "")
		for _, n := range mvDefaultCols {
			ids[n] = e.create("task-"+n, n)
		}
		e.start(80, 24)
		e.screenHas("task-idea")
		e.key("l", "l", "l", "l") // курсор на done
		e.settle()
		e.until("колонка done видна", func() bool { return len(e.visibleColumns()) > 0 && strings.Contains(e.lines()[2], "done") })
		if names := e.visibleColumns(); len(names) < 2 || names[0] == "idea" {
			t.Fatalf("окно не сдвинулось: видны %v, шапка %q", names, e.lines()[2])
		}
		return e
	}

	t.Run("клик выбирает нарисованную колонку", func(t *testing.T) {
		e := setup(t)
		for slot := 0; slot < 3; slot++ {
			names := e.visibleColumns()
			if slot >= len(names) {
				break
			}
			want := "task-" + names[slot]
			e.click(mvX(slot), mvY(0))
			e.settle()
			e.key("enter")
			e.opened(want)
			e.key("esc")
			e.screenHas("enter open")
		}
	})
	t.Run("перенос из нарисованной колонки в нарисованную", func(t *testing.T) {
		e := setup(t)
		before := e.visibleColumns()
		src, dst := before[0], before[1]
		e.press(mvX(0), mvY(0))
		e.settle()
		// окно липкое: нажатие его не двигает, цель - та колонка, что нарисована в слоте 1
		if now := e.visibleColumns(); strings.Join(now, " ") != strings.Join(before, " ") {
			t.Fatalf("нажатие сдвинуло окно колонок: было %v, стало %v", before, now)
		}
		e.release(mvX(1), mvY(0))
		e.screenHas("waiting for:") // dst - blocked, роль waiting: спрашивает причину
		e.tm.Type("why")
		e.key("enter")
		e.until(ids[src]+" в "+dst, func() bool { st, _ := e.status(ids[src]); return st == dst })
	})
}

// 4.B: при строке ввода мышь игнорируется, ввод доходит до задачи, у которой он начат.
func TestMouse_InputLineOpen_MouseIgnored(t *testing.T) {
	e := mvNewEnv(t, mvCfgWork)
	e.create("Alpha", "work")
	e.create("Bravo", "work")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.key(" ", "l", " ") // Alpha -> wait: строка причины
	e.screenHas("waiting for:")
	before := e.commits()
	e.click(3, mvY(1)) // клик по Bravo
	e.press(3, mvY(1)) // и перетаскивание Bravo в fin
	e.release(mvX(2), mvY(0))
	e.settle()
	if !strings.Contains(e.text(), "waiting for:") {
		t.Fatalf("мышь закрыла строку ввода:\n%s", e.text())
	}
	if e.commits() != before {
		t.Error("мышь при строке ввода записала коммит")
	}
	e.tm.Type("reason")
	e.key("enter")
	e.until("T-1 в wait с причиной", func() bool { st, w := e.status("T-1"); return st == "wait" && w == "reason" })
	if st, _ := e.status("T-2"); st != "work" {
		t.Errorf("T-2 сдвинута мышью при строке ввода: %s", st)
	}
}

// ---------------------------------------------------------------------------------------------
// Пульс, порядок
// ---------------------------------------------------------------------------------------------

// 4.C.3: ask важнее заметки; заметка только в колонке active; без заметок - summary.
func TestPulse_AskBeatsNote_NoteOnlyInActive_ElseSummary(t *testing.T) {
	e := mvNewEnv(t, "")
	asked := e.create("Asked", "doing")
	noted := e.create("Noted", "doing")
	plain := e.create("Plain", "doing")
	parked := e.create("Parked", "todo")
	for id, s := range map[string]string{asked: "sum-asked", noted: "sum-noted", plain: "sum-plain", parked: "sum-parked"} {
		e.summary(id, s)
	}
	e.set(asked, "ask", "pick A or B")
	e.note(asked, "note-asked")
	e.note(noted, "note-noted")
	e.note(parked, "note-parked")
	e.start(140, 40)

	e.screenHas("note-noted", "? pick A or B", "sum-plain", "sum-parked")
	s := e.text()
	for _, bad := range []string{"note-asked", "sum-asked", "sum-noted", "note-parked"} {
		if strings.Contains(s, bad) {
			t.Errorf("на доске лишнее %q:\n%s", bad, s)
		}
	}
}

// 4.A.4: возраст заметки 5m, 3h, 2d по часам модели; у задачи видна самая новая заметка.
func TestPulse_NoteAge_UsesModelClock_LatestNoteWins(t *testing.T) {
	cases := []struct {
		offset time.Duration
		want   string
	}{
		{5*time.Minute + 20*time.Second, "5m"},
		{3*time.Hour + 5*time.Minute, "3h"},
		{50*time.Hour + 5*time.Minute, "2d"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			e := mvNewEnv(t, "")
			id := e.create("Noted", "doing")
			e.note(id, "old-note")
			e.note(id, "new-note")
			sec, err := strconv.ParseInt(e.git("log", "-1", "--format=%ct"), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			e.setClock(time.Unix(sec, 0).Add(c.offset))
			e.start(140, 30)
			e.screenHas(c.want + " new-note")
			if strings.Contains(e.text(), "old-note") {
				t.Errorf("показана не последняя заметка:\n%s", e.text())
			}
		})
	}
}

// 4.A.4: tk note - пустой коммит без правки файла; наблюдатель замечает его по .git/logs/HEAD.
func TestPulse_NoteWrittenWhileOpen_AppearsWithoutKeypress(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("Noted", "doing")
	e.summary(id, "sum-noted")
	e.start(140, 30)
	e.screenHas("sum-noted")
	// наблюдатель стартует после первого кадра: заметку повторяем, пока он не включится
	deadline := time.Now().Add(5 * time.Second)
	for i := 0; time.Now().Before(deadline) && !strings.Contains(e.text(), "fresh-news"); i++ {
		e.note(id, "fresh-news")
		for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end) && !strings.Contains(e.text(), "fresh-news"); {
			time.Sleep(10 * time.Millisecond)
		}
	}
	e.screenHas("fresh-news")
}

// Риск 3: число вызовов git на перечитывание пульса не растёт с числом задач.
func TestPulse_GitCalls_DoNotGrowWithTaskCount(t *testing.T) {
	realGit := mvGit(t)
	calls := func(tasks int) int {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "calls.log")
		wrapper := filepath.Join(dir, "git")
		script := "#!/bin/sh\necho \"$@\" >> \"" + logPath + "\"\nexec " + realGit + " \"$@\"\n"
		if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil { //nolint:gosec // тестовый скрипт
			t.Fatal(err)
		}
		e := mvNewEnvGit(t, "", wrapper)
		for i := 1; i <= tasks; i++ {
			id := e.create(fmt.Sprintf("Task%02d", i), "doing")
			e.note(id, fmt.Sprintf("note-of-%02d", i))
		}
		if err := os.WriteFile(logPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		e.start(140, 40)
		e.screenHas("note-of-01")
		e.settle()
		data, err := os.ReadFile(logPath) //nolint:gosec // путь теста
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, ln := range strings.Split(string(data), "\n") {
			for _, f := range strings.Fields(ln) {
				if f == "log" {
					n++
					break
				}
			}
		}
		return n
	}
	small, large := calls(3), calls(40)
	if large > small {
		t.Errorf("вызовов git log: %d на 3 задачах, %d на 40 - растут с числом задач", small, large)
	}
}

// 4.C.4: o переключает порядок по сроку (затем без срока по rank); J в этом режиме не пишет коммит.
func TestOrder_O_ByDue_JDoesNotWrite(t *testing.T) {
	e := mvNewEnv(t, "")
	a := e.create("task-aaa", "idea")
	e.create("task-bbb", "idea")
	c := e.create("task-ccc", "idea")
	e.set(a, "due", "2026-12-01")
	e.set(c, "due", "2026-11-01")
	e.start(140, 30)
	e.screenHas("task-aaa", "task-bbb", "task-ccc")
	order := func() []string {
		type p struct {
			y    int
			name string
		}
		var ps []p
		for _, n := range []string{"task-aaa", "task-bbb", "task-ccc"} {
			ps = append(ps, p{e.yOf(n), n})
		}
		for i := range ps {
			for j := i + 1; j < len(ps); j++ {
				if ps[j].y < ps[i].y {
					ps[i], ps[j] = ps[j], ps[i]
				}
			}
		}
		var out []string
		for _, x := range ps {
			out = append(out, x.name)
		}
		return out
	}
	if got := strings.Join(order(), " "); got != "task-aaa task-bbb task-ccc" {
		t.Fatalf("порядок по rank: %s", got)
	}
	e.key("o")
	e.screenHas("order: due")
	e.settle()
	if got := strings.Join(order(), " "); got != "task-ccc task-aaa task-bbb" {
		t.Errorf("порядок по сроку: %s, ждали ccc (11-01), aaa (12-01), bbb (без срока)", got)
	}
	before := e.commits()
	e.key("J")
	e.screenHas("order by due: J K off")
	e.settle()
	if e.commits() != before {
		t.Errorf("J в режиме по сроку записал коммит: %s -> %s", before, e.commits())
	}
	if got := strings.Join(order(), " "); got != "task-ccc task-aaa task-bbb" {
		t.Errorf("J изменил порядок в режиме по сроку: %s", got)
	}
	e.key("o")
	e.until("режим по rank", func() bool {
		return !strings.Contains(e.text(), "order: due") && strings.Join(order(), " ") == "task-aaa task-bbb task-ccc"
	})
}
