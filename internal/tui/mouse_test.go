package tui

// Мышь и липкие окна: попадание берёт то же окно, что нарисовано, клик окно не двигает.
// Модель ведётся напрямую через Update: без терминала и таймеров.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/core"
	"tracker/internal/tui/view"
)

// Роли разложены так, чтобы перенос между соседями не спрашивал причину.
const stickyCfg = `[[column]]
name = "c1"
role = "waiting"

[[column]]
name = "c2"
role = "closed"

[[column]]
name = "c3"
role = "inbox"

[[column]]
name = "c4"
role = "active"

[[column]]
name = "c5"
role = "ready"
`

const twoColCfg = `[[column]]
name = "b"
role = "active"

[[column]]
name = "c"
role = "waiting"
`

func newMouseModel(t *testing.T, e *mvEnv, w, h int) *Model {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := New(ctx, Deps{Store: e.store, Root: e.root, Actor: "human"})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// feed отдаёт модели сообщение и выполняет возвращённую команду записи, если она есть.
func feed(m *Model, msg tea.Msg) {
	_, cmd := m.Update(msg)
	if cmd == nil {
		return
	}
	if done, ok := cmd().(opDone); ok {
		m.Update(done)
	}
}

func typeKeys(m *Model, keys string) {
	for _, r := range keys {
		feed(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func leftClick(m *Model, x, y int) { feed(m, tea.MouseClickMsg(mvM(x, y, tea.MouseLeft))) }
func leftRelease(m *Model, x, y int) {
	feed(m, tea.MouseReleaseMsg(mvM(x, y, tea.MouseLeft)))
}

func (e *mvEnv) statusOf(t *testing.T, id string) string {
	t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return tk.Status
}

func firstShownColumn(m *Model) int {
	first, _ := view.FitWindow(m.layout())
	return first
}

// Риск 1: ширина 80, курсор на дальней колонке, окно [c3 c4 c5]. Нажатие на крайнюю левую
// колонку двигает курсор, но окно остаётся: отпускание над c4 переносит в c4, а не в колонку,
// которая оказалась бы там после прыжка окна на [c1 c2 c3].
func TestMouse_Drag_ColumnWindowStaysAfterPress_MovesToDrawnColumn(t *testing.T) {
	e := mvNewEnv(t, stickyCfg)
	for i := 1; i <= 5; i++ {
		e.create(fmt.Sprintf("task-c%d", i), fmt.Sprintf("c%d", i))
	}
	m := newMouseModel(t, e, 80, 30)
	typeKeys(m, "llll")
	if first := firstShownColumn(m); first != 2 {
		t.Fatalf("окно [c3 c4 c5] ждали, первая колонка %d", first)
	}
	leftClick(m, mvX(0), mvY(0)) // task-c3
	if first := firstShownColumn(m); first != 2 {
		t.Fatalf("нажатие сдвинуло окно колонок: первая колонка %d, ждали 2", first)
	}
	leftRelease(m, mvX(1), mvY(0)) // над нарисованной c4
	if st := e.statusOf(t, "T-3"); st != "c4" {
		t.Errorf("T-3 в %q, ждали c4 (строка ввода=%v, статус %q)", st, m.input != nil, m.status)
	}
}

// То же по вертикали: клик по верхней видимой карточке прокрученной колонки не сдвигает окно.
func TestMouse_Click_RowWindowStaysInScrolledColumn(t *testing.T) {
	e := mvNewEnv(t, stickyCfg)
	for i := 1; i <= 8; i++ {
		e.create(fmt.Sprintf("task-%d", i), "c1")
	}
	m := newMouseModel(t, e, 80, 24) // тело 17 строк: три карточки
	typeKeys(m, "jjjjjjj")
	if top := view.HitBoard(m.layout(), mvX(0), mvY(0)); top.Row != 5 {
		t.Fatalf("после прокрутки наверху карточка 5, а не %d", top.Row)
	}
	leftClick(m, mvX(0), mvY(0))
	leftRelease(m, mvX(0), mvY(0))
	if got := view.HitBoard(m.layout(), mvX(0), mvY(0)); got.Row != 5 {
		t.Errorf("клик сдвинул окно строк: наверху карточка %d, ждали 5", got.Row)
	}
	if sel := m.selected(); sel == nil || sel.ID != "T-6" {
		t.Errorf("выбрана %v, ждали T-6", sel)
	}
	// Клавиша за верх окна окно сдвигает.
	typeKeys(m, "kk")
	if got := view.HitBoard(m.layout(), mvX(0), mvY(0)); got.Row != 3 {
		t.Errorf("k за верх окна: наверху карточка %d, ждали 3", got.Row)
	}
}

// reopenWith переписывает config.toml и открывает ядро заново: задачи вне конфига уходят в колонку "?".
func reopenWith(t *testing.T, e *mvEnv, cfg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.root, ".tracker", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := core.Open(e.root, core.Options{GitBin: mvGit(t)})
	if err != nil {
		t.Fatal(err)
	}
	e.store = s
}

func TestMouse_Drag_ToUnknownColumn_Refused(t *testing.T) {
	e := mvNewEnv(t, "[[column]]\nname = \"a\"\nrole = \"inbox\"\n\n"+twoColCfg)
	e.create("lost", "a")
	e.create("kept", "b")
	reopenWith(t, e, twoColCfg)
	m := newMouseModel(t, e, 140, 30) // колонки b, c, ?
	leftClick(m, mvX(0), mvY(0))      // T-2 в b
	leftRelease(m, mvX(2), mvY(0))
	if m.status != "T-2: unknown column" || !m.isErr {
		t.Errorf("статус %q (ошибка=%v), ждали T-2: unknown column", m.status, m.isErr)
	}
	if st := e.statusOf(t, "T-2"); m.input != nil || st != "b" {
		t.Errorf("перенос в ? не отказан: строка ввода=%v, статус задачи %s", m.input != nil, st)
	}
}

// Задача сменила колонку между нажатием и отпусканием: отпускание над её новой колонкой - только выбор.
func TestMouse_Drag_TaskMovedMeanwhile_ReleaseOverItsColumnIsNoMove(t *testing.T) {
	e := mvNewEnv(t, twoColCfg)
	e.create("Alpha", "b")
	m := newMouseModel(t, e, 140, 30)
	leftClick(m, mvX(0), mvY(0))
	if _, err := e.store.Move("T-1", "c", core.MoveOpts{Message: "agent waits"}, "agent"); err != nil {
		t.Fatal(err)
	}
	tasks, errs := e.store.List()
	m.apply(tasks, len(errs))
	leftRelease(m, mvX(1), mvY(0))
	if m.input != nil {
		t.Errorf("отпускание над текущей колонкой задачи открыло строку %q", m.input.label)
	}
	if st := e.statusOf(t, "T-1"); st != "c" || strings.Contains(m.status, "T-1") {
		t.Errorf("T-1: %s, статус %q: ждали задачу на месте и пустой статус", st, m.status)
	}
}

// Терминал шлёт клик как нажатие и отпускание; двойной клик - две такие пары подряд.
func TestMouse_DoubleClick_WithReleases_OpensCard(t *testing.T) {
	e := mvNewEnv(t, twoColCfg)
	e.create("Alpha", "b")
	m := newMouseModel(t, e, 80, 24)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	leftClick(m, mvX(0), mvY(0))
	leftRelease(m, mvX(0), mvY(0))
	now = now.Add(150 * time.Millisecond)
	leftClick(m, mvX(0), mvY(0))
	leftRelease(m, mvX(0), mvY(0))
	if !m.showing {
		t.Fatal("двойной клик с отпусканиями не открыл карточку")
	}
}
