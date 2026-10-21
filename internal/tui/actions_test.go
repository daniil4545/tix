package tui

// Тесты основного пути человека на доске (docs/plans/mvp-actions.md, разделы 3, 4, 4a, 5).
// Пакет внутренний ради стенда mv* из mvp_test.go; свои хелперы с префиксом ac. Пишутся по спеке,
// без чтения кода зон F, G, H. Среда (HOME, git) - TestMain из harness_test.go.

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"tracker/internal/core"
	"tracker/internal/task"
)

const (
	acHint     = "arrows select  space grab  enter open  n new  a answer  e edit  c comment  o order  p project  ? help  q quit"
	acFieldRow = "t title  d due  g tags  a ask  w waiting  s summary  x external"
	acFocusURL = "warp://session/0123456789abcdef0123456789abcdef"
	acMoving   = "moving T-1: arrows move, space drop, esc cancel"
)

// ---------------------------------------------------------------------------------------------
// Хелперы стенда
// ---------------------------------------------------------------------------------------------

// acPress шлёт клавишу: имена стрелок и backspace - кодом, прочее - как e.key.
func (e *mvEnv) acPress(keys ...string) {
	for _, k := range keys {
		codes := map[string]rune{"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown, "backspace": tea.KeyBackspace}
		if c, ok := codes[k]; ok {
			e.tm.Send(tea.KeyPressMsg{Code: c})
			continue
		}
		e.key(k)
	}
}

func (e *mvEnv) acCommitCount() int {
	e.t.Helper()
	n, err := strconv.Atoi(e.commits())
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// acCommitsSettled ждёт (до срока), пока коммитов станет want, затем даёт очереди дописать лишнее
// и возвращает итоговое число: запись файла идёт раньше коммита.
func (e *mvEnv) acCommitsSettled(want int) int {
	e.t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end) && e.acCommitCount() < want; time.Sleep(10 * time.Millisecond) {
	}
	e.settle()
	return e.acCommitCount()
}

func (e *mvEnv) acTask(id string) *task.Task {
	e.t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		e.t.Fatalf("Get %s: %v", id, err)
	}
	return tk
}

func (e *mvEnv) acByTitle(title string) *task.Task {
	tasks, _ := e.store.List()
	for _, tk := range tasks {
		if tk.Title == title {
			return tk
		}
	}
	return nil
}

// acOrder - id задач колонки по rank, как их хранит ядро.
func (e *mvEnv) acOrder(column string) string {
	tasks, _ := e.store.List()
	var in []*task.Task
	for _, tk := range tasks {
		if tk.Status == column {
			in = append(in, tk)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Rank < in[j].Rank })
	ids := make([]string, len(in))
	for i, tk := range in {
		ids[i] = tk.ID
	}
	return strings.Join(ids, " ")
}

// acFind - клетка первого знака sub в нижней из строк экрана, где он есть.
func (e *mvEnv) acFind(sub string) (x, y int) {
	e.t.Helper()
	ls := e.lines()
	for y = len(ls) - 1; y >= 0; y-- {
		if i := strings.Index(ls[y], sub); i >= 0 {
			return utf8.RuneCountInString(ls[y][:i]), y
		}
	}
	e.t.Fatalf("на экране нет %q:\n%s", sub, e.text())
	return 0, 0
}

// acClickOn ждёт sub на экране и кликает в его середину.
func (e *mvEnv) acClickOn(sub string) {
	e.t.Helper()
	e.screenHas(sub)
	x, y := e.acFind(sub)
	e.click(x+utf8.RuneCountInString(sub)/2, y)
}

// acColX - колонка экрана, с которой начинается title в первой строке с ним.
func (e *mvEnv) acColX(title string) int {
	for _, ln := range e.lines() {
		if i := strings.Index(ln, title); i >= 0 {
			return utf8.RuneCountInString(ln[:i])
		}
	}
	return -1
}

// acWaitStatus ждёт, пока задача получит статус.
func (e *mvEnv) acWaitStatus(id, status string) {
	e.t.Helper()
	e.until(id+" в "+status, func() bool { st, _ := e.status(id); return st == status })
}

// acStillOn: после обработки всего посланного на экране есть sub (и нет лишнего).
func (e *mvEnv) acStillOn(sub string) {
	e.t.Helper()
	e.settle()
	if !strings.Contains(e.text(), sub) {
		e.t.Fatalf("на экране нет %q:\n%s", sub, e.text())
	}
}

// acStartIn запускает TUI с начальным проектом (фильтром) project.
func (e *mvEnv) acStartIn(project string, w, h int) {
	e.t.Helper()
	ctx := e.t.Context()
	e.model = New(ctx, Deps{Store: e.store, Root: e.root, Actor: "human", Project: project})
	e.model.now = func() time.Time { return time.Unix(0, e.clock.Load()) }
	e.tm = teatest.NewTestModel(e.t, mvProbe{Model: e.model, e: e}, teatest.WithInitialTermSize(w, h))
	e.t.Cleanup(func() {
		_ = e.tm.Quit()
		e.tm.WaitFinished(e.t, teatest.WithFinalTimeout(5*time.Second))
	})
}

// acOpenCard открывает карточку выбранной задачи.
func (e *mvEnv) acOpenCard(title string) {
	e.t.Helper()
	e.key("enter")
	e.opened(title)
	e.settle() // кадр карточки уже с размером окна: клики считаются по нему
}

// acEditor - заглушка $EDITOR: считает вызовы, копирует файл при запуске, выполняет script над файлом.
type acEditor struct {
	t   *testing.T
	dir string
}

const acEditorScript = `#!/bin/sh
d="$TK_AC_DIR"
n=$(( $(cat "$d/count" 2>/dev/null || echo 0) + 1 )); echo $n > "$d/count"
cp "$1" "$d/seen.$n"
if [ -f "$d/script" ]; then sh "$d/script" "$1"; fi
exit 0
`

func acStubEditor(t *testing.T, script string) *acEditor {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(bin, []byte(acEditorScript), 0o700); err != nil { //nolint:gosec // заглушка теста
		t.Fatal(err)
	}
	if script != "" {
		if err := os.WriteFile(filepath.Join(dir, "script"), []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("EDITOR", bin)
	t.Setenv("VISUAL", "")
	t.Setenv("TK_AC_DIR", dir)
	return &acEditor{t: t, dir: dir}
}

func (s *acEditor) calls() int {
	b, _ := os.ReadFile(filepath.Join(s.dir, "count")) //nolint:gosec // путь теста
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func (s *acEditor) seen(n int) string {
	b, _ := os.ReadFile(filepath.Join(s.dir, "seen."+strconv.Itoa(n))) //nolint:gosec // путь теста
	return string(b)
}

// acFakeOpen подменяет команду open скриптом в PATH; возвращает путь журнала вызовов (по строке на вызов).
func acFakeOpen(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "open.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TK_AC_OPEN_LOG\"\n"
	if err := os.WriteFile(filepath.Join(dir, "open"), []byte(script), 0o700); err != nil { //nolint:gosec // заглушка теста
		t.Fatal(err)
	}
	t.Setenv("TK_AC_OPEN_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func acRead(path string) string {
	b, _ := os.ReadFile(path) //nolint:gosec // путь теста
	return string(b)
}

// ---------------------------------------------------------------------------------------------
// Сценарий 1: подсказка-кнопки
// ---------------------------------------------------------------------------------------------

func acBoard(t *testing.T, w, h int) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.start(w, h)
	e.screenHas("Alpha")
	e.settle() // первый кадр рисуется до размера окна: клики считаются по кадру после него
	return e
}

// 3 "Подсказка-кнопки": последняя строка доски - новая подсказка.
func TestHint_BoardLastLine_IsNewHint(t *testing.T) {
	e := acBoard(t, 140, 30)
	e.screenHas("q quit")
	ls := e.lines()
	var last string
	for _, ln := range ls {
		if strings.TrimSpace(ln) != "" {
			last = strings.TrimSpace(ln)
		}
	}
	if last != acHint {
		t.Errorf("последняя строка доски %q, ждали %q", last, acHint)
	}
}

// 5.1: клик по паре равен нажатию клавиши.
func TestHint_ClickButton_ActsAsKey(t *testing.T) {
	cases := []struct{ pair, want string }{
		{"n new", "title:"},
		{"c comment", "comment:"},
		{"a answer", "T-1: no question"},
		{"o order", "order: due"},
	}
	for _, c := range cases {
		t.Run(c.pair, func(t *testing.T) {
			e := acBoard(t, 140, 30)
			e.acClickOn(c.pair)
			e.screenHas(c.want)
		})
	}
}

// 5.1: клик по q quit выходит.
func TestHint_ClickQuit_ExitsProgram(t *testing.T) {
	e := acBoard(t, 140, 30)
	e.screenHas("q quit")
	x, y := e.acFind("q quit")
	e.mouse(tea.MouseClickMsg(mvM(x+2, y, tea.MouseLeft))) // выход на нажатии: отпускание уже не придёт
	e.tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// 3: arrows select - не кнопка; клик по промежутку между парами тоже ничего не делает.
func TestHint_ClickNonButton_DoesNothing(t *testing.T) {
	e := acBoard(t, 140, 30)
	e.screenHas("arrows select")
	before := e.acCommitCount()
	e.acClickOn("arrows select")
	gap := strings.Index(acHint, "enter open  n new") + len("enter open") + 1 // второй пробел между парами
	_, y := e.acFind("p project")
	e.click(gap, y)
	e.settle()
	for _, bad := range []string{"title:", "comment:", "answer:", "no question", "order: due"} {
		if strings.Contains(e.text(), bad) {
			t.Errorf("клик не по кнопке вызвал %q:\n%s", bad, e.text())
		}
	}
	if !strings.Contains(e.text(), "enter open") || e.acCommitCount() != before {
		t.Errorf("клик не по кнопке изменил состояние:\n%s", e.text())
	}
}

// 5.1 и 3: обрезанная по ширине пара не кликается; целая рядом работает.
func TestHint_TruncatedPair_NotClickable(t *testing.T) {
	e := acBoard(t, len(acHint)-2, 30) // "q quit" обрезано до "q qu"
	e.until("подсказка обрезана по ширине", func() bool {
		return strings.Contains(e.text(), "p project") && !strings.Contains(e.text(), "q quit")
	})
	_, y := e.acFind("p project")
	e.click(strings.Index(acHint, "q quit"), y) // на букву q
	e.click(strings.Index(acHint, "q quit")+2, y)
	e.settle() // зависнет с ошибкой, если программа вышла
	if !strings.Contains(e.text(), "enter open") {
		t.Errorf("клик по обрезанной паре изменил экран:\n%s", e.text())
	}
	e.acClickOn("n new")
	e.screenHas("title:")
}

// ---------------------------------------------------------------------------------------------
// Сценарии 2 и 3: захват
// ---------------------------------------------------------------------------------------------

// acCriterion дописывает задаче один критерий: колонка ready принимает только задачи с критерием.
func (e *mvEnv) acCriterion(ids ...string) {
	e.t.Helper()
	for _, id := range ids {
		_, rev, err := e.store.Get(id)
		if err == nil {
			_, err = e.store.Edit(id, rev, "## Details\n- [ ] one\n", "seed")
		}
		if err != nil {
			e.t.Fatalf("criterion %s: %v", id, err)
		}
	}
}

func acGrabEnv(t *testing.T) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	e.create("Charlie", "idea")
	e.acCriterion("T-1", "T-2", "T-3")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo", "Charlie")
	return e
}

// 5.2 и 3.1-3.3: space, right, space и enter - карточка в соседней колонке, один коммит, до отпускания записи нет.
func TestGrab_RightThenDrop_MovesToNeighborWithOneCommit(t *testing.T) {
	for _, drop := range []string{" ", "enter"} {
		t.Run("drop="+strconv.Quote(drop), func(t *testing.T) {
			e := acGrabEnv(t)
			before := e.acCommitCount()
			e.key(" ")
			e.screenHas(acMoving)
			e.until("маркер '=' у захваченной карточки", func() bool {
				y := e.yOf("T-1 p")
				return y >= 0 && strings.Contains(e.lines()[y], "=T-1")
			})
			e.acPress("right")
			e.until("Alpha на экране в соседней колонке", func() bool { x := e.acColX("Alpha"); return x >= 26 && x <= 49 })
			if st, _ := e.status("T-1"); st != "idea" || e.acCommitCount() != before {
				t.Fatalf("до отпускания записано: статус %s, коммитов %d -> %d", st, before, e.acCommitCount())
			}
			e.key(drop)
			e.acWaitStatus("T-1", "todo")
			e.settle()
			if after := e.acCommitsSettled(before + 1); after != before+1 {
				t.Errorf("коммитов %d -> %d, ждали ровно один", before, after)
			}
			if strings.Contains(e.text(), "moving T-1") {
				t.Errorf("захват не снят после отпускания:\n%s", e.text())
			}
		})
	}
}

// 5.2 и 3.2-3.3: место в колонке; стрелки и h j k l равны; место пишется как First или After соседа сверху.
func TestGrab_UpDown_ChoosesPlaceInColumn(t *testing.T) {
	cases := []struct {
		name   string
		pick   int // сколько j до space
		keys   []string
		wanted string
	}{
		{"down", 0, []string{"down"}, "T-2 T-1 T-3"},
		{"j j to the end", 0, []string{"j", "j"}, "T-2 T-3 T-1"},
		{"up up to the top", 2, []string{"up", "up"}, "T-3 T-1 T-2"},
		{"k once", 2, []string{"k"}, "T-1 T-3 T-2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := acGrabEnv(t)
			e.key(strings.Repeat("j", c.pick))
			e.settle()
			before := e.acCommitCount()
			e.key(" ")
			e.screenHas("moving T-")
			e.acPress(c.keys...)
			e.settle()
			if got := e.acOrder("idea"); got != "T-1 T-2 T-3" || e.acCommitCount() != before {
				t.Fatalf("до отпускания записано: порядок %s, коммитов %d -> %d", got, before, e.acCommitCount())
			}
			e.key(" ")
			e.until("порядок "+c.wanted, func() bool { return e.acOrder("idea") == c.wanted })
			e.settle()
			if after := e.acCommitsSettled(before + 1); after != before+1 {
				t.Errorf("коммитов %d -> %d, ждали ровно один", before, after)
			}
		})
	}
}

// 3.3: та же колонка и то же место - ничего не пишется; захват снят.
func TestGrab_DropAtOriginalPlace_NoCommit(t *testing.T) {
	cases := []struct {
		name string
		keys []string
	}{
		{"сразу отпустить", nil},
		{"вправо и обратно", []string{"right", "left"}},
		{"вниз и обратно", []string{"down", "up"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := acGrabEnv(t)
			before := e.acCommitCount()
			e.key(" ")
			e.screenHas("moving T-1")
			e.acPress(c.keys...)
			e.key(" ")
			e.until("захват снят", func() bool { return !strings.Contains(e.text(), "moving T-1") })
			e.settle()
			if after := e.acCommitCount(); after != before {
				t.Errorf("коммитов %d -> %d, ждали без записи", before, after)
			}
			if got := e.acOrder("idea"); got != "T-1 T-2 T-3" {
				t.Errorf("порядок изменился: %s", got)
			}
		})
	}
}

// 5.2 и 3.4: esc - отмена, карточка на прежнем месте, коммита нет.
func TestGrab_Esc_CancelsWithoutCommit(t *testing.T) {
	e := acGrabEnv(t)
	before := e.acCommitCount()
	e.key(" ")
	e.screenHas("moving T-1")
	e.acPress("right")
	e.until("Alpha ушла в соседнюю колонку", func() bool { return e.acColX("Alpha") >= 26 })
	e.key("esc")
	e.until("Alpha вернулась в свою колонку", func() bool { x := e.acColX("Alpha"); return x >= 0 && x < 26 })
	e.settle()
	if strings.Contains(e.text(), "moving T-1") {
		t.Errorf("строка захвата осталась после esc:\n%s", e.text())
	}
	if st, _ := e.status("T-1"); st != "idea" || e.acCommitCount() != before {
		t.Errorf("отмена записала: статус %s, коммитов %d -> %d", st, before, e.acCommitCount())
	}
}

// 3.4: при захвате прочие клавиши и мышь игнорируются.
func TestGrab_OtherKeysAndMouse_Ignored(t *testing.T) {
	e := acGrabEnv(t)
	before := e.acCommitCount()
	e.key(" ")
	e.screenHas("moving T-1")
	e.key("n", "a", "c", "o", "p", "q")
	e.click(mvX(0)+3, mvY(1)) // клик по Bravo
	e.click(mvX(1)+22, 2)     // и по "+" колонки todo
	e.settle()
	for _, bad := range []string{"title:", "comment:", "answer:", "no question", "order: due"} {
		if strings.Contains(e.text(), bad) {
			t.Fatalf("при захвате сработало %q:\n%s", bad, e.text())
		}
	}
	if !strings.Contains(e.text(), "moving T-1") || e.acCommitCount() != before {
		t.Errorf("захват сбит прочими событиями, коммитов %d -> %d:\n%s", before, e.acCommitCount(), e.text())
	}
	e.key("esc")
}

// 5.2 и 3.3: перенос в waiting и closed спрашивает причину, как m; esc строки ввода оставляет задачу.
func TestGrab_ToWaitingAndClosed_AsksReason(t *testing.T) {
	setup := func(t *testing.T) *mvEnv {
		e := mvNewEnv(t, mvCfgWork)
		e.create("Alpha", "work")
		e.create("Bravo", "work")
		e.start(140, 30)
		e.screenHas("Alpha", "Bravo")
		return e
	}
	t.Run("waiting: причина переносит", func(t *testing.T) {
		e := setup(t)
		before := e.acCommitCount()
		e.key(" ")
		e.acPress("right")
		e.key(" ")
		e.screenHas("waiting for:")
		if st, _ := e.status("T-1"); st != "work" || e.acCommitCount() != before {
			t.Fatalf("до ответа записано: %s, коммитов %d -> %d", st, before, e.acCommitCount())
		}
		e.tm.Type("vendor reply")
		e.key("enter")
		e.until("T-1 в wait с причиной", func() bool { st, w := e.status("T-1"); return st == "wait" && w == "vendor reply" })
		e.settle()
		if after := e.acCommitsSettled(before + 1); after != before+1 {
			t.Errorf("коммитов %d -> %d, ждали один", before, after)
		}
	})
	t.Run("waiting: esc оставляет задачу", func(t *testing.T) {
		e := setup(t)
		before := e.acCommitCount()
		e.key(" ")
		e.acPress("right")
		e.key(" ")
		e.screenHas("waiting for:")
		e.key("esc")
		e.until("строка закрыта", func() bool { return !strings.Contains(e.text(), "waiting for:") })
		e.settle()
		if st, _ := e.status("T-1"); st != "work" || e.acCommitCount() != before {
			t.Errorf("esc записал: %s, коммитов %d -> %d", st, before, e.acCommitCount())
		}
	})
	t.Run("closed: close reason", func(t *testing.T) {
		e := setup(t)
		e.key(" ")
		e.acPress("right", "right")
		e.key(" ")
		e.screenHas("close reason:")
		e.tm.Type("shipped")
		e.key("enter")
		e.acWaitStatus("T-1", "fin")
	})
}

// 5.3 и 3.5: задачу удалили в другом окне - отмена и "T-1 removed", захват снят (space не роняет в запись).
func TestGrab_TaskRemovedMeanwhile_ReportsRemovedAndReleases(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	canary := e.create("canary", "done")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.awaitWatcher(canary)
	e.key(" ")
	e.screenHas("moving T-1")
	if _, err := e.store.Remove("T-1", "agent"); err != nil {
		t.Fatal(err)
	}
	e.screenHas("T-1 removed")
	if strings.Contains(e.text(), "moving T-1") {
		t.Errorf("захват удалённой задачи не снят:\n%s", e.text())
	}
	after := e.acCommitCount()
	e.key(" ") // захвата нет: это новый захват выбранной Bravo, а не отпускание
	e.screenHas("moving T-2")
	e.key("esc")
	e.settle()
	if e.acCommitCount() != after {
		t.Error("после удаления захваченной задачи что-то записано")
	}
	if st, _ := e.status("T-2"); st != "idea" {
		t.Errorf("T-2 перенесена: %s", st)
	}
}

// 3.5: иное изменение базы захват не снимает: он держится по id и доводит перенос.
func TestGrab_OtherChangeMeanwhile_HoldsById(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "idea")
	canary := e.create("canary", "done")
	e.acCriterion("T-1")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.awaitWatcher(canary)
	e.key(" ")
	e.screenHas("moving T-1")
	e.create("Delta", "idea") // агент добавил задачу выше нашей в колонке по rank не встанет, но база перечитана
	e.screenHas("Delta")
	e.acStillOn("moving T-1")
	e.acPress("right")
	e.key(" ")
	e.acWaitStatus("T-1", "todo")
}

// 3.6: порядок по сроку включён - захват меняет только колонку, строка состояния об этом говорит.
func TestGrab_OrderByDue_ColumnOnly(t *testing.T) {
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.create("Bravo", "todo")
	e.acCriterion("T-1")
	e.start(140, 30)
	e.screenHas("Alpha", "Bravo")
	e.key("o")
	e.screenHas("order: due")
	e.key(" ")
	e.screenHas("order by due: place off")
	before := e.acCommitCount()
	e.acPress("right")
	e.key(" ")
	e.acWaitStatus("T-1", "todo")
	e.settle()
	if after := e.acCommitsSettled(before + 1); after != before+1 {
		t.Errorf("коммитов %d -> %d, ждали один", before, after)
	}
}

// ---------------------------------------------------------------------------------------------
// Сценарий 4: новая задача
// ---------------------------------------------------------------------------------------------

// 5.4 и 3 "Новая задача": клик по "+" колонки todo, заголовок, редактор; курсор на новой задаче.
func TestNewTask_ClickPlus_CreatesInColumnAndOpensEditor(t *testing.T) {
	cases := []struct{ name, script, wantBody string }{
		{"редактор без правки", "", ""},
		{"редактор с правкой", `printf '## Abstract\nedited-abstract\n' > "$1"`, "edited-abstract"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := acStubEditor(t, c.script)
			e := mvNewEnv(t, "")
			e.create("Alpha", "idea")
			e.create("Beta", "todo")
			e.start(140, 30)
			e.screenHas("Alpha", "Beta")
			plusX := mvColStep + 23 // последний знак ячейки второй колонки
			if r := []rune(e.lines()[2]); len(r) <= plusX || r[plusX] != '+' {
				t.Fatalf("в строке имён колонок на x=%d нет '+': %q", plusX, e.lines()[2])
			}
			e.click(plusX, 2)
			e.screenHas("title:")
			e.tm.Type("Brand new")
			e.key("enter")
			e.until("редактор вызван", func() bool { return st.calls() == 1 && st.seen(1) != "" })
			e.until("задача создана", func() bool { return e.acByTitle("Brand new") != nil })
			tk := e.acByTitle("Brand new")
			if tk.Status != "todo" || tk.Project != "personal" {
				t.Errorf("задача: колонка %q, проект %q, ждали todo и personal", tk.Status, tk.Project)
			}
			acCheckTemplate(t, st.seen(1))
			e.settle()
			e.acCheckBody(tk.ID, c.wantBody)
			e.selectedIs("Brand new")
		})
	}
}

func acCheckTemplate(t *testing.T, seen string) {
	t.Helper()
	for _, part := range []string{"## Abstract", "## Summary", "## Details", "- [ ] "} {
		if !strings.Contains(seen, part) {
			t.Errorf("шаблон тела в редакторе без %q:\n%s", part, seen)
		}
	}
}

// acCheckBody: want пусто - тело пустое (выход без правки), иначе тело получает want.
func (e *mvEnv) acCheckBody(id, want string) {
	e.t.Helper()
	if want == "" {
		if body := e.acTask(id).Body; strings.TrimSpace(body) != "" {
			e.t.Errorf("выход без правки оставил тело %q, ждали пустое", body)
		}
		return
	}
	e.until("правка редактора в теле", func() bool { return strings.Contains(e.acTask(id).Body, want) })
}

// 3 "Новая задача": n - в выбранной колонке; проект - фильтр запуска, иначе personal.
func TestNewTask_N_UsesSelectedColumnAndProject(t *testing.T) {
	cases := []struct{ name, launch, seed, want string }{
		{"без фильтра", "", "p", "personal"},
		{"проект запуска", "launch", "launch", "launch"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			acStubEditor(t, "")
			e := mvNewEnv(t, "")
			if _, err := e.store.Create("Alpha", c.seed, "idea", "seed"); err != nil {
				t.Fatal(err)
			}
			if _, err := e.store.Create("Beta", c.seed, "todo", "seed"); err != nil {
				t.Fatal(err)
			}
			e.acStartIn(c.launch, 140, 30)
			e.screenHas("Alpha", "Beta")
			e.key("l", "n") // колонка todo
			e.screenHas("title:")
			e.tm.Type("From key")
			e.key("enter")
			e.until("задача создана", func() bool { return e.acByTitle("From key") != nil })
			tk := e.acByTitle("From key")
			if tk.Status != "todo" || tk.Project != c.want {
				t.Errorf("задача: колонка %q, проект %q, ждали todo и %q", tk.Status, tk.Project, c.want)
			}
		})
	}
}

// 3 "Новая задача" п.2: пустой заголовок или esc - отмена: ни задачи, ни коммита, ни редактора.
func TestNewTask_EmptyTitleOrEsc_Cancels(t *testing.T) {
	for _, cancel := range []string{"enter", "esc"} {
		t.Run(cancel, func(t *testing.T) {
			st := acStubEditor(t, "")
			e := acBoard(t, 140, 30)
			before := e.acCommitCount()
			e.key("n")
			e.screenHas("title:")
			e.key(cancel)
			e.until("строка закрыта", func() bool { return !strings.Contains(e.text(), "title:") })
			e.settle()
			if tasks, _ := e.store.List(); len(tasks) != 1 || e.acCommitCount() != before || st.calls() != 0 {
				t.Errorf("отмена оставила след: задач %d, коммитов %d -> %d, вызовов редактора %d",
					len(tasks), before, e.acCommitCount(), st.calls())
			}
		})
	}
}

// ---------------------------------------------------------------------------------------------
// Сценарий 5: ответ на вопрос агента
// ---------------------------------------------------------------------------------------------

func acAsked(t *testing.T, ask string) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	if ask != "" {
		e.set("T-1", "ask", ask)
	}
	e.start(140, 30)
	e.screenHas("Alpha")
	e.settle()
	return e
}

// 5.5: без вопроса - "no question", ничего не записано (доска и карточка).
func TestAnswer_NoQuestion_SaysNoQuestion(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		t.Run(where, func(t *testing.T) {
			e := acAsked(t, "")
			if where == "card" {
				e.acOpenCard("Alpha")
			}
			before := e.acCommitCount()
			e.key("a")
			e.screenHas("T-1: no question")
			e.settle()
			if strings.Contains(e.text(), "answer:") || e.acCommitCount() != before {
				t.Errorf("ответ без вопроса открыл строку или записал, коммитов %d -> %d", before, e.acCommitCount())
			}
		})
	}
}

// 5.5: с вопросом - комментарий "Answer: ..." от человека, ask очищен (доска и карточка).
func TestAnswer_WithQuestion_CommentsAndClearsAsk(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		t.Run(where, func(t *testing.T) {
			e := acAsked(t, "pick A or B")
			if where == "card" {
				e.acOpenCard("Alpha")
			}
			e.key("a")
			e.screenHas("answer:")
			e.tm.Type("use B")
			e.key("enter")
			e.until("комментарий и пустой ask", func() bool {
				tk := e.acTask("T-1")
				return tk.Ask == "" && len(tk.Comments) == 1
			})
			c := e.acTask("T-1").Comments[0]
			if c.Text != "Answer: use B" || c.Actor != "human" {
				t.Errorf("комментарий %q от %q, ждали %q от human", c.Text, c.Actor, "Answer: use B")
			}
		})
	}
}

// 4.2: агент успел задать новый вопрос - ответ записан, новый вопрос остался, строка "new question".
func TestAnswer_AgentAskedAgain_KeepsNewQuestion(t *testing.T) {
	e := acAsked(t, "pick A or B")
	e.key("a")
	e.screenHas("answer:")
	e.tm.Type("use B")
	e.set("T-1", "ask", "second question")
	e.key("enter")
	e.screenHas("T-1: new question")
	e.until("комментарий записан", func() bool { return len(e.acTask("T-1").Comments) == 1 })
	if tk := e.acTask("T-1"); tk.Ask != "second question" || tk.Comments[0].Text != "Answer: use B" {
		t.Errorf("ask %q, комментарий %q: ждали новый вопрос и записанный ответ", tk.Ask, tk.Comments[0].Text)
	}
}

// 4.2: пусто или esc - отмена без записи.
func TestAnswer_EmptyOrEsc_Cancels(t *testing.T) {
	for _, cancel := range []string{"enter", "esc"} {
		t.Run(cancel, func(t *testing.T) {
			e := acAsked(t, "pick A or B")
			before := e.acCommitCount()
			e.key("a")
			e.screenHas("answer:")
			e.key(cancel)
			e.until("строка закрыта", func() bool { return !strings.Contains(e.text(), "answer:") })
			e.settle()
			if tk := e.acTask("T-1"); tk.Ask != "pick A or B" || len(tk.Comments) != 0 || e.acCommitCount() != before {
				t.Errorf("отмена записала: ask %q, комментариев %d, коммитов %d -> %d",
					tk.Ask, len(tk.Comments), before, e.acCommitCount())
			}
		})
	}
}

// 5.5 и 4.2: клик по строке "* needs you:" в карточке открывает "answer:".
func TestAnswer_ClickNeedsYouLine_OpensAnswerLine(t *testing.T) {
	e := acAsked(t, "pick A or B")
	e.acOpenCard("Alpha")
	e.screenHas("needs you")
	_, y := e.acFind("needs you")
	e.click(4, y)
	e.screenHas("answer:")
}

// ---------------------------------------------------------------------------------------------
// Сценарий 6: поля
// ---------------------------------------------------------------------------------------------

// acFieldCard: карточка Alpha (проект projx) открыта; due - начальный срок.
func acFieldCard(t *testing.T, due string) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	if _, err := e.store.Create("Alpha", "projx", "idea", "seed"); err != nil {
		t.Fatal(err)
	}
	if due != "" {
		e.set("T-1", "due", due)
	}
	e.start(140, 30)
	e.screenHas("Alpha")
	e.acOpenCard("Alpha")
	return e
}

type acDueCase struct {
	name, initial, typed, wantDue, wantStatus string
	erase, wantCommit                         bool
}

// enterDue проходит f, d, правку значения и enter.
func (e *mvEnv) enterDue(c acDueCase) {
	e.t.Helper()
	e.key("f")
	e.screenHas(acFieldRow)
	e.key("d")
	e.screenHas("due:")
	if c.erase {
		e.acPress(strings.Split(strings.Repeat("backspace,", 30), ",")[:30]...)
	}
	if c.typed != "" {
		e.tm.Type(c.typed)
	}
	e.key("enter")
}

// 5.6 и 4.3: срок записан; пустое значение очищает; отказ ядра виден; без изменения записи нет.
func TestField_Due_SetClearRefusedUnchanged(t *testing.T) {
	cases := []acDueCase{
		{"записать", "", "2026-11-01", "2026-11-01", "", false, true},
		{"очистить пустым", "2026-10-01", "", "", "", true, true},
		{"отказ ядра", "", "not-a-date", "", "due must be", false, false},
		{"без изменения", "2026-10-01", "", "2026-10-01", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := acFieldCard(t, c.initial)
			before := e.acCommitCount()
			e.enterDue(c)
			if c.wantStatus != "" {
				e.screenHas(c.wantStatus)
			}
			want := before
			if c.wantCommit {
				want++
			}
			if after := e.acCommitsSettled(want); after != want {
				t.Errorf("коммитов %d -> %d, ждали %d", before, after, want)
			}
			if tk := e.acTask("T-1"); tk.Due != c.wantDue {
				t.Errorf("срок %q, ждали %q", tk.Due, c.wantDue)
			}
		})
	}
}

// 4.3: строка поля открывается с текущим значением (правка дописывает к нему).
func TestField_Title_PrefilledWithCurrentValue(t *testing.T) {
	e := acFieldCard(t, "")
	e.key("f")
	e.screenHas(acFieldRow)
	e.key("t")
	e.screenHas("title:")
	e.tm.Type("X")
	e.key("enter")
	e.until("заголовок AlphaX", func() bool { return e.acTask("T-1").Title == "AlphaX" })
}

// 4.3: поле из нескольких строк строкой не правится; из одной - правится.
func TestField_Summary_MultiLineRefusedSingleLineEdits(t *testing.T) {
	t.Run("несколько строк", func(t *testing.T) {
		e := mvNewEnv(t, "")
		e.create("Alpha", "idea")
		e.summary("T-1", "first line\nsecond line")
		e.start(140, 30)
		e.screenHas("Alpha")
		e.acOpenCard("Alpha")
		before := e.acCommitCount()
		e.key("f", "s")
		e.screenHas("T-1: summary has several lines, use e")
		e.settle()
		if strings.Contains(e.text(), "summary:") || e.acCommitCount() != before {
			t.Errorf("строка открылась или записано, коммитов %d -> %d", before, e.acCommitCount())
		}
	})
	t.Run("одна строка", func(t *testing.T) {
		e := mvNewEnv(t, "")
		e.create("Alpha", "idea")
		e.summary("T-1", "only line")
		e.start(140, 30)
		e.screenHas("Alpha")
		e.acOpenCard("Alpha")
		e.key("f", "s")
		e.screenHas("summary:")
	})
}

// 4.3: выбор поля - кликом по паре; клик по строке полей карточки открывает тот же выбор.
func TestField_SelectByClick_AndFieldsRowOpensSelect(t *testing.T) {
	e := acFieldCard(t, "")
	e.screenHas("projx")
	e.settle()
	_, y := e.acFind("projx") // строка полей: проект первым
	e.click(2, y)
	e.screenHas(acFieldRow)
	e.acClickOn("d due")
	e.screenHas("due:")
	e.tm.Type("2026-12-24")
	e.key("enter")
	e.until("срок записан", func() bool { return e.acTask("T-1").Due == "2026-12-24" })
}

// ---------------------------------------------------------------------------------------------
// Сценарий 7: строка действий карточки
// ---------------------------------------------------------------------------------------------

// lastNonEmpty - индекс последней непустой строки экрана.
func (e *mvEnv) acLastLine() int {
	ls := e.lines()
	for y := len(ls) - 1; y >= 0; y-- {
		if strings.TrimSpace(ls[y]) != "" {
			return y
		}
	}
	return -1
}

// 5.7 и 4.1: строка действий - последняя строка карточки и видна при непустой строке состояния;
// строка состояния над ней. Проверка на просторном и на низком экране с длинной историей.
func TestCardActions_RowVisibleWithStatusAbove(t *testing.T) {
	const row = "e edit  c comment  a answer  f field  g session  del delete  j k history  esc back"
	for _, tc := range []struct {
		name string
		w, h int
		n    int // комментариев в истории
	}{
		{"просторный экран", 140, 30, 0},
		{"низкий экран, длинная история", 100, 14, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := mvNewEnv(t, "")
			e.create("Alpha", "idea")
			for i := 0; i < tc.n; i++ {
				if _, err := e.store.Comment("T-1", "remark-"+strconv.Itoa(i), "agent"); err != nil {
					t.Fatal(err)
				}
			}
			e.start(tc.w, tc.h)
			e.screenHas("Alpha")
			e.acOpenCard("Alpha")
			e.key("a")
			e.screenHas("T-1: no question")
			e.settle()
			last := e.acLastLine()
			if last < 1 || strings.TrimSpace(e.lines()[last]) != row {
				t.Fatalf("последняя строка карточки %q, ждали %q:\n%s", e.lines()[max(last, 0)], row, e.text())
			}
			if !strings.Contains(e.lines()[last-1], "T-1: no question") {
				t.Errorf("над строкой действий %q, ждали строку состояния", e.lines()[last-1])
			}
		})
	}
}

// 5.7: клик по паре строки действий равен клавише.
func TestCardActions_ClickButton_ActsAsKey(t *testing.T) {
	cases := []struct{ pair, want string }{
		{"c comment", "comment:"},
		{"a answer", "T-1: no question"},
		{"f field", acFieldRow},
		{"g session", "T-1: no session to open"},
		{"esc back", "arrows select"},
	}
	for _, c := range cases {
		t.Run(c.pair, func(t *testing.T) {
			e := acAsked(t, "")
			e.acOpenCard("Alpha")
			e.acClickOn(c.pair)
			e.screenHas(c.want)
		})
	}
	t.Run("e edit", func(t *testing.T) {
		st := acStubEditor(t, "")
		e := acAsked(t, "")
		e.acOpenCard("Alpha")
		e.acClickOn("e edit")
		e.until("редактор вызван", func() bool { return st.calls() == 1 })
	})
}

// 4.4: клик по пустому месту карточки - ничего.
func TestCardActions_ClickEmptySpace_DoesNothing(t *testing.T) {
	e := acAsked(t, "")
	e.acOpenCard("Alpha")
	e.settle()
	before, commits := e.text(), e.acCommitCount()
	e.click(120, 8)
	e.click(60, 3)
	e.settle()
	if got := e.text(); got != before || e.acCommitCount() != commits {
		t.Errorf("клик по пустому месту изменил карточку:\n%s\nбыло:\n%s", got, before)
	}
}

// ---------------------------------------------------------------------------------------------
// Сценарий 8a: критерии в карточке
// ---------------------------------------------------------------------------------------------

// acCriteriaCard: карточка Alpha с двумя критериями открыта; курсор уведён к первому критерию
// (сначала критерии, потом история: k вверх по истории доходит до них и там упирается).
func acCriteriaCard(t *testing.T) *mvEnv {
	t.Helper()
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	_, rev, err := e.store.Get("T-1")
	if err == nil {
		_, err = e.store.Edit("T-1", rev, "## Details\n- [ ] one\n- [ ] two\n", "seed")
	}
	if err != nil {
		t.Fatal(err)
	}
	e.start(140, 30)
	e.screenHas("Alpha")
	e.acOpenCard("Alpha")
	e.screenHas("one", "two")
	e.key(strings.Repeat("k", 20))
	e.settle()
	return e
}

func (e *mvEnv) acDone() string {
	var out []string
	for _, c := range e.acTask("T-1").Criteria() {
		out = append(out, strconv.FormatBool(c.Done))
	}
	return strings.Join(out, " ")
}

// 5.8a и 4.4: j до пункта, enter - отмечен, enter ещё раз - снят (в файле снова "- [ ]").
func TestCriteria_JThenEnter_TogglesAndUntoggles(t *testing.T) {
	e := acCriteriaCard(t)
	e.key("j") // второй критерий
	e.settle()
	e.key("enter")
	e.until("отмечен второй критерий", func() bool { return e.acDone() == "false true" })
	if _, _, raw, err := e.store.Show("T-1"); err != nil || !strings.Contains(string(raw), "- [x] two") || !strings.Contains(string(raw), "- [ ] one") {
		t.Errorf("файл после отметки (err=%v):\n%s", err, raw)
	}
	e.key("enter")
	e.until("отметка снята", func() bool { return e.acDone() == "false false" })
	if _, _, raw, _ := e.store.Show("T-1"); !strings.Contains(string(raw), "- [ ] two") || strings.Contains(string(raw), "[x]") {
		t.Errorf("файл после снятия:\n%s", raw)
	}
}

// 5.8a: клик по строке критерия отмечает его; повторный клик снимает.
func TestCriteria_Click_TogglesCriterion(t *testing.T) {
	e := acCriteriaCard(t)
	x, y := e.acFind("[ ] one")
	e.click(x+1, y)
	e.until("отмечен первый критерий", func() bool { return e.acDone() == "true false" })
	e.screenHas("[x] one")
	x, y = e.acFind("[x] one")
	e.click(x+1, y)
	e.until("отметка снята", func() bool { return e.acDone() == "false false" })
}

// ---------------------------------------------------------------------------------------------
// Сценарии 8 (удаление, справка, m M убраны) и 9 (переход в сессию агента, сторона TUI)
// ---------------------------------------------------------------------------------------------

// Раздел 1: m и M удалены - ни строки причины, ни записи, на доске и в карточке.
func TestKeys_MAndShiftM_DoNothing(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		t.Run(where, func(t *testing.T) {
			e := mvNewEnv(t, mvCfgWork)
			e.create("Alpha", "work")
			e.start(140, 30)
			e.screenHas("Alpha")
			if where == "card" {
				e.acOpenCard("Alpha")
			}
			before := e.acCommitCount()
			e.key("m", "M")
			e.settle()
			if strings.Contains(e.text(), "waiting for:") || strings.Contains(e.text(), "close reason:") {
				t.Errorf("m или M открыли строку переноса:\n%s", e.text())
			}
			if st, _ := e.status("T-1"); st != "work" || e.acCommitCount() != before {
				t.Errorf("m M записали: статус %s, коммитов %d -> %d", st, before, e.acCommitCount())
			}
		})
	}
}

// acPressDelete шлёт клавишу Delete: delete или backspace.
func (e *mvEnv) acPressDelete(name string) {
	code := tea.KeyDelete
	if name == "backspace" {
		code = tea.KeyBackspace
	}
	e.tm.Send(tea.KeyPressMsg{Code: code})
}

func acDeleteEnv(t *testing.T, where string) *mvEnv {
	t.Helper()
	e := acAsked(t, "")
	if where == "card" {
		e.acOpenCard("Alpha")
	}
	return e
}

// 5.8 и раздел 1: Delete (delete и backspace) на доске и в карточке спрашивает "delete T-1? y/n";
// y удаляет одним коммитом.
func TestDelete_ConfirmYesRemoves(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		for _, del := range []string{"delete", "backspace"} {
			t.Run(where+"/"+del, func(t *testing.T) {
				e := acDeleteEnv(t, where)
				before := e.acCommitCount()
				e.acPressDelete(del)
				e.screenHas("delete T-1? y/n")
				if _, _, err := e.store.Get("T-1"); err != nil || e.acCommitCount() != before {
					t.Fatalf("до подтверждения удалено: err=%v, коммитов %d -> %d", err, before, e.acCommitCount())
				}
				e.key("y")
				e.until("задачи нет", func() bool { _, _, err := e.store.Get("T-1"); return err != nil })
				if after := e.acCommitsSettled(before + 1); after != before+1 {
					t.Errorf("коммитов %d -> %d, ждали один", before, after)
				}
			})
		}
	}
}

// 5.8: любая клавиша, кроме y, отменяет удаление.
func TestDelete_OtherKeyCancels(t *testing.T) {
	for _, where := range []string{"board", "card"} {
		for _, other := range []string{"n", "x", "esc", "enter"} {
			t.Run(where+"/"+other, func(t *testing.T) {
				e := acDeleteEnv(t, where)
				before := e.acCommitCount()
				e.acPressDelete("delete")
				e.screenHas("delete T-1? y/n")
				e.key(other)
				e.until("вопрос закрыт", func() bool { return !strings.Contains(e.text(), "delete T-1?") })
				e.settle()
				if _, _, err := e.store.Get("T-1"); err != nil || e.acCommitCount() != before {
					t.Errorf("отмена клавишей %q удалила: err=%v, коммитов %d -> %d", other, err, before, e.acCommitCount())
				}
			})
		}
	}
}

// 5.8: клик по кнопке "del delete" строки действий карточки равен клавише.
func TestDelete_CardButton_AsksConfirmation(t *testing.T) {
	e := acAsked(t, "")
	e.acOpenCard("Alpha")
	e.acClickOn("del delete")
	e.screenHas("delete T-1? y/n")
	e.key("y")
	e.until("задачи нет", func() bool { _, _, err := e.store.Get("T-1"); return err != nil })
}

// 5.8: ? открывает справку со всеми клавишами; esc или ? возвращают на доску; кнопка "? help" равна клавише.
func TestHelp_QuestionMark_OpensAndCloses(t *testing.T) {
	for _, closer := range []string{"esc", "?"} {
		t.Run("close="+closer, func(t *testing.T) {
			e := acBoard(t, 140, 30)
			board := e.text()
			e.key("?")
			e.until("справка вместо доски", func() bool {
				s := e.text()
				return s != board && strings.Contains(s, "grab") && strings.Contains(s, "delete")
			})
			e.key(closer)
			e.screenHas("arrows select", "Alpha")
		})
	}
	t.Run("click", func(t *testing.T) {
		e := acBoard(t, 140, 30)
		board := e.text()
		e.acClickOn("? help")
		e.until("справка вместо доски", func() bool { return e.text() != board && !strings.Contains(e.text(), "arrows select") })
	})
}

type acFocusCase struct {
	name     string
	claim    bool
	fileName string // "" - файла нет
	content  string
	wantOpen bool
	ways     []string
}

// acFocusEnv готовит задачу Alpha с захватом (если claim) и файлом фокуса в каталоге dir.
func acFocusEnv(t *testing.T, c acFocusCase, dir string) *mvEnv {
	t.Helper()
	steps := 0 // сколько колонок вправо от idea до задачи
	e := mvNewEnv(t, "")
	e.create("Alpha", "idea")
	e.acCriterion("T-1")
	if c.claim {
		// Take берёт задачу из ready и переносит в active: колонку курсора ищем по статусу
		if _, err := e.store.Move("T-1", "todo", core.MoveOpts{}, "seed"); err != nil {
			t.Fatalf("Move: %v", err)
		}
		if _, err := e.store.Take("T-1", core.Take, "claude:c1"); err != nil {
			t.Fatalf("Take: %v", err)
		}
		steps = slices.Index(mvDefaultCols, e.acTask("T-1").Status)
	}
	if c.fileName != "" {
		if err := os.WriteFile(filepath.Join(dir, c.fileName), []byte(c.content+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e.start(140, 30)
	e.screenHas("Alpha")
	e.key(strings.Repeat("l", steps))
	e.settle()
	return e
}

// 4a.2: g - только при живом захвате и файле фокуса его владельца, прошедшем проверку вида; иначе
// "no session to open". Вызов open - подменённой командой в PATH.
func TestFocus_G_OpensOnlyForLiveClaimWithValidFile(t *testing.T) {
	all := []string{"board", "card", "button"}
	cases := []acFocusCase{
		{"захват и файл владельца", true, "claude-c1", acFocusURL, true, all},
		{"нет захвата", false, "claude-c1", acFocusURL, false, all},
		{"захват есть, файла нет", true, "", "", false, all},
		{"файл чужого агента", true, "codex-x9", acFocusURL, false, []string{"board"}},
		{"значение не того вида", true, "claude-c1", "warp://session/XYZ", false, []string{"board"}},
		{"другая схема", true, "claude-c1", "https://example.com/s", false, []string{"board"}},
	}
	for _, c := range cases {
		for _, way := range c.ways {
			t.Run(c.name+"/"+way, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("TRACKER_FOCUS_DIR", dir)
				log := acFakeOpen(t)
				e := acFocusEnv(t, c, dir)
				e.acPressG(way)
				if c.wantOpen {
					e.until("open вызван с адресом", func() bool { return strings.TrimSpace(acRead(log)) == acFocusURL })
					return
				}
				e.screenHas("T-1: no session to open")
				e.settle()
				if got := acRead(log); got != "" {
					t.Errorf("open вызван без права: %q", got)
				}
			})
		}
	}
}

// acPressG: g на доске, g в карточке или кнопка "g session" карточки.
func (e *mvEnv) acPressG(way string) {
	e.t.Helper()
	if way != "board" {
		e.acOpenCard("Alpha")
	}
	if way == "button" {
		e.acClickOn("g session")
		return
	}
	e.key("g")
}
