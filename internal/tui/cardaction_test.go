package tui

// Действия карточки и переход в сессию: docs/plans/mvp-actions.md, разделы 4 и 4a.
// Модель ведётся напрямую через Update, без терминала.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/core"
)

var cardANSI = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

func caEnter(m *Model) { feed(m, tea.KeyPressMsg{Code: tea.KeyEnter}) }

// frameLines - текст экрана без escape-кодов по строкам.
func caLines(m *Model) []string {
	return strings.Split(cardANSI.ReplaceAllString(m.View().Content, ""), "\n")
}

// xyOf ищет подстроку на экране: клетка её первого знака.
func caXY(t *testing.T, m *Model, sub string) (x, y int) {
	t.Helper()
	for y, ln := range caLines(m) {
		if i := strings.Index(ln, sub); i >= 0 {
			return len([]rune(ln[:i])), y
		}
	}
	t.Fatalf("на экране нет %q:\n%s", sub, strings.Join(caLines(m), "\n"))
	return 0, 0
}

func (e *mvEnv) caTask(id string) (ask, due, status string, comments []string) {
	e.t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		e.t.Fatalf("Get %s: %v", id, err)
	}
	for _, c := range tk.Comments {
		comments = append(comments, c.Text)
	}
	return tk.Ask, tk.Due, tk.Status, comments
}

func caStatus(m *Model) string { s, _ := m.statusText(); return s }

func TestAnswer_WritesCommentThenClearsAsk(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.set(id, "ask", "which db?")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	typeKeys(m, "a")
	if m.input == nil || m.input.label != "answer:" {
		t.Fatalf("после a строка ввода %+v, ждали answer:", m.input)
	}
	typeKeys(m, "postgres")
	caEnter(m)
	ask, _, _, comments := e.caTask(id)
	if ask != "" || len(comments) != 1 || comments[0] != "Answer: postgres" {
		t.Errorf("ask=%q comments=%q; ждали пустой ask и один комментарий Answer: postgres", ask, comments)
	}
}

func TestAnswer_AskChangedMeanwhile_QuestionStays(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.set(id, "ask", "which db?")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	typeKeys(m, "a")
	typeKeys(m, "postgres")
	e.set(id, "ask", "which version?") // агент успел задать новый вопрос
	caEnter(m)
	ask, _, _, comments := e.caTask(id)
	if ask != "which version?" {
		t.Errorf("ask = %q, ждали новый вопрос нетронутым", ask)
	}
	if len(comments) != 1 {
		t.Errorf("комментариев %d, ждали ответ на старый вопрос", len(comments))
	}
	if got := caStatus(m); got != id+": new question" {
		t.Errorf("статус %q, ждали %q", got, id+": new question")
	}
}

func TestAnswer_NoQuestionOrEmpty_NothingWritten(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	typeKeys(m, "a")
	if got := caStatus(m); got != id+": no question" || m.input != nil {
		t.Errorf("без вопроса: статус %q, input %v", got, m.input)
	}
	e.set(id, "ask", "q?")
	before := e.commits()
	caReload(m)
	typeKeys(m, "a")
	caEnter(m) // пусто - отмена
	if e.commits() != before {
		t.Error("пустой ответ оставил коммит")
	}
}

func caOpenCard(m *Model) {
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		m.Update(cmd()) // история карточки приходит сообщением
	}
}

func TestField_SetClearUnchangedRefused(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)

	typeKeys(m, "fd2026-11-01")
	caEnter(m)
	if _, due, _, _ := e.caTask(id); due != "2026-11-01" {
		t.Fatalf("due = %q, ждали 2026-11-01", due)
	}

	caReload(m)
	n := e.commits()
	typeKeys(m, "fd") // начальное значение в строке - текущее
	caEnter(m)
	if e.commits() != n {
		t.Error("то же значение записано повторно")
	}

	typeKeys(m, "fd")
	feed(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	caEnter(m)
	if _, due, _, _ := e.caTask(id); due != "" {
		t.Errorf("пустое значение не очистило срок: %q", due)
	}

	typeKeys(m, "fdnot-a-date")
	caEnter(m)
	if got := caStatus(m); !strings.Contains(got, "due") {
		t.Errorf("отказ ядра не виден: статус %q", got)
	}
}

func TestField_SummaryWithSeveralLines_RefusesToEditInline(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.summary(id, "line one\nline two")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	typeKeys(m, "fs")
	if got := caStatus(m); got != id+": summary has several lines, use e" || m.input != nil {
		t.Errorf("статус %q, input %v", got, m.input)
	}
}

func TestCardClick_AskRowFieldsRowAndButtons(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.set(id, "ask", "which db?")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)

	x, y := caXY(t, m, "* needs you:")
	leftClick(m, x, y)
	if m.input == nil || m.input.label != "answer:" {
		t.Fatalf("клик по вопросу: input %+v", m.input)
	}
	feed(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	leftClick(m, 0, 1) // строка полей: сразу под заголовком
	if m.input == nil || m.input.pick == nil {
		t.Fatalf("клик по строке полей: input %+v", m.input)
	}
	// Клик по паре выбора равен её клавише.
	x, y = caXY(t, m, "d due")
	leftClick(m, x, y)
	if m.input == nil || m.input.label != "due:" {
		t.Fatalf("клик по d due: input %+v", m.input)
	}
	feed(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	x, y = caXY(t, m, "c comment")
	leftClick(m, x, y)
	if m.input == nil || m.input.label != "comment:" {
		t.Fatalf("клик по c comment: input %+v", m.input)
	}
	feed(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	x, y = caXY(t, m, "set ask")
	leftClick(m, x, y) // тело карточки без кнопки
	if m.input != nil {
		t.Errorf("клик по пустому месту открыл строку ввода %+v", m.input)
	}
	x, y = caXY(t, m, "esc back")
	leftClick(m, x, y)
	if m.showing {
		t.Error("клик по esc back не закрыл карточку")
	}
}

func TestFocus_G_OpensOnlyValidURLOfLiveClaim(t *testing.T) {
	var opened []string

	const good = "warp://session/0123456789abcdef0123456789abcdef"
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	m.acts.open = func(url string) error { opened = append(opened, url); return nil }

	typeKeys(m, "g")
	if got := caStatus(m); got != id+": no session to open" || len(opened) != 0 {
		t.Fatalf("без захвата: статус %q, open %v", got, opened)
	}

	if _, err := e.store.Take(id, core.Take, "claude:abcd1234"); err != nil {
		t.Fatal(err)
	}
	caReload(m)
	typeKeys(m, "g")
	if len(opened) != 0 {
		t.Fatalf("захват есть, файла фокуса нет, а open вызван: %v", opened)
	}

	dir := t.TempDir()
	t.Setenv("TRACKER_FOCUS_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"-a Calculator\n", good + "\nextra\n", "warp://session/ZZ\n", ""} {
		if err := os.WriteFile(filepath.Join(dir, "claude-abcd1234"), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		typeKeys(m, "g")
		if len(opened) != 0 {
			t.Fatalf("файл %q прошёл проверку, open %v", bad, opened)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "claude-abcd1234"), []byte(good+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	typeKeys(m, "g")
	if len(opened) != 1 || opened[0] != good {
		t.Fatalf("open вызван с %v, ждали [%s]", opened, good)
	}
	caOpenCard(m)
	x, y := caXY(t, m, "g session")
	leftClick(m, x, y)
	if len(opened) != 2 {
		t.Errorf("кнопка g session не открыла сессию: %v", opened)
	}
}

// caReload перечитывает базу, как это делает наблюдатель файлов.
func caReload(m *Model) {
	tasks, errs := m.d.Store.List()
	m.apply(tasks, len(errs))
}

func (e *mvEnv) caBody(id, body string) {
	e.t.Helper()
	_, rev, err := e.store.Get(id)
	if err == nil {
		_, err = e.store.Edit(id, rev, body, "seed")
	}
	if err != nil {
		e.t.Fatalf("body %s: %v", id, err)
	}
}

func TestCriteria_SelectedByCursor_EnterChecksAndUnchecks(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	const body = "## Details\nplain line\n- [ ] one\n- [x] two\n"
	e.caBody(id, body)
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	if m.card.nCrit != 2 {
		t.Fatalf("критериев в выборе %d, ждали 2", m.card.nCrit)
	}
	// Enter на записи истории не трогает критерии.
	caEnter(m)
	if tk, _, _ := e.store.Get(id); tk.Body != body {
		t.Fatalf("enter на истории изменил тело: %q", tk.Body)
	}

	m.card.step(-100) // первый критерий
	caEnter(m)
	tk, _, _ := e.store.Get(id)
	if c := tk.Criteria(); !c[0].Done || !c[1].Done {
		t.Fatalf("после отметки: %+v", c)
	}
	caReload(m)
	m.card.step(1) // второй критерий, он отмечен
	caEnter(m)
	tk, _, _ = e.store.Get(id)
	if c := tk.Criteria(); !c[0].Done || c[1].Done {
		t.Fatalf("после снятия: %+v", c)
	}
	if want := "## Details\nplain line\n- [x] one\n- [ ] two\n"; tk.Body != want {
		t.Errorf("тело после снятия = %q, ждали %q (прочее не менялось)", tk.Body, want)
	}
}

func TestCriteria_ClickOnRowToggles(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.caBody(id, "## Details\n- [ ] first\n")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	x, y := caXY(t, m, "- [ ] first")
	leftClick(m, x, y)
	tk, _, _ := e.store.Get(id)
	if !tk.Criteria()[0].Done {
		t.Errorf("клик по критерию не отметил его: %q", tk.Body)
	}
}

func TestCriteria_ChangedMeanwhile_NotToggled(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	e.caBody(id, "## Details\n- [ ] first\n")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	e.caBody(id, "## Details\n- [ ] other\n") // модель ещё не перечитала файл
	m.card.step(-100)
	caEnter(m)
	tk, _, _ := e.store.Get(id)
	if tk.Criteria()[0].Done {
		t.Error("отмечен критерий, которого пользователь не видел")
	}
}

func TestDelete_ConfirmByY_AnyOtherKeyCancels(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	caOpenCard(m)
	feed(m, tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.input == nil || m.input.label != "delete "+id+"? y/n" {
		t.Fatalf("input %+v", m.input)
	}
	typeKeys(m, "n")
	if _, _, err := e.store.Get(id); err != nil || m.input != nil {
		t.Fatalf("n удалил задачу или оставил строку: err=%v input=%v", err, m.input)
	}
	feed(m, tea.KeyPressMsg{Code: tea.KeyDelete})
	typeKeys(m, "y")
	if _, _, err := e.store.Get(id); err == nil {
		t.Error("y не удалил задачу")
	}
	if m.showing {
		t.Error("после удаления из карточки осталась карточка")
	}
}

func TestDelete_SecondDeleteOrBackspace_Cancels(t *testing.T) {
	for _, second := range []tea.KeyPressMsg{{Code: tea.KeyDelete}, {Code: tea.KeyBackspace}} {
		e := mvNewEnv(t, "")
		id := e.create("job", "todo")
		m := newMouseModel(t, e, 120, 30)
		m.selectTask(id)
		feed(m, tea.KeyPressMsg{Code: tea.KeyDelete})
		feed(m, second)
		if _, _, err := e.store.Get(id); err != nil || m.input != nil {
			t.Errorf("%v при открытом вопросе: err=%v input=%v; ждали отмену без удаления", second, err, m.input)
		}
	}
}

func TestFocus_G_ExpiredClaim_NoSession(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "todo")
	if _, err := e.store.Take(id, core.Take, "claude:abcd1234"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("TRACKER_FOCUS_DIR", dir)
	url := "warp://session/0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(filepath.Join(dir, "claude-abcd1234"), []byte(url+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	var opened []string
	m.acts.open = func(u string) error { opened = append(opened, u); return nil }
	m.now = func() time.Time { return time.Now().Add(10000 * time.Hour) }
	typeKeys(m, "g")
	if got := caStatus(m); got != id+": no session to open" || len(opened) != 0 {
		t.Errorf("истёкший захват: статус %q, open %v", got, opened)
	}
}
