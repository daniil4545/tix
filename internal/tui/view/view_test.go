package view_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/daniil4545/tix/internal/history"
	"github.com/daniil4545/tix/internal/tui/view"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata")

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

// plain убирает цветовые коды: тесты проверяют текст, а не палитру.
func plain(s string) string { return ansiRe.ReplaceAllString(s, "") }

// fits проверяет границы экрана: строк не больше h, каждая строка не шире w.
func fits(t *testing.T, name, out string, w, h int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > h {
		t.Errorf("%s: %d строк при высоте %d", name, len(lines), h)
	}
	for i, ln := range lines {
		if lw := lipgloss.Width(ln); lw > w {
			t.Errorf("%s: строка %d шириной %d при ширине %d: %q", name, i, lw, w, plain(ln))
			return
		}
	}
}

var colNames = []string{"idea", "todo", "doing", "blocked", "done"}

func bigBoard(rowsPerCol int) view.Board {
	b := view.Board{Total: rowsPerCol * len(colNames), Col: 1, Row: 0}
	n := 0
	for _, name := range colNames {
		c := view.Col{Name: name}
		for i := 0; i < rowsPerCol; i++ {
			n++
			c.Rows = append(c.Rows, view.Row{
				ID: fmt.Sprintf("T-%d", n), Project: "proj", Title: fmt.Sprintf("Заголовок %d 日本語 long long title", n),
				Summary: "первая строка сводки, довольно длинная", Meta: "1/3 до 28.10 @claude",
			})
		}
		b.Cols = append(b.Cols, c)
	}
	return b
}

func TestRenderBoard_AnySize_FitsScreen(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{200, 60}, {120, 40}, {80, 24}, {60, 12}, {40, 8}} {
		for _, rows := range []int{0, 3, 60} {
			b := bigBoard(rows)
			b.W, b.H = sz.w, sz.h
			b.Status = "status line that is rather long to check truncation of the bottom line as well"
			fits(t, fmt.Sprintf("доска %dx%d, %d строк в колонке", sz.w, sz.h, rows), view.RenderBoard(b), sz.w, sz.h)
		}
	}
}

func TestRenderBoard_WideScreen_ShowsColumnsAndRowParts(t *testing.T) {
	b := view.Board{Project: "alpha", Total: 2, W: 200, H: 40, Cols: []view.Col{
		{Name: "idea"},
		{Name: "todo", Rows: []view.Row{
			{ID: "T-7", Project: "alpha", Title: "Fix login", Summary: "see notes", Meta: "1/3 28.10"},
		}},
		{Name: "doing"}, {Name: "blocked"}, {Name: "done"},
	}}
	out := plain(view.RenderBoard(b))
	for _, want := range colNames {
		if !strings.Contains(out, want) {
			t.Errorf("доска без колонки %q:\n%s", want, out)
		}
	}
	for _, want := range []string{"T-7", "alpha", "Fix login", "see notes", "1/3 28.10"} {
		if !strings.Contains(out, want) {
			t.Errorf("доска без %q:\n%s", want, out)
		}
	}
}

func TestRenderBoard_StarOnlyOnRowThatNeedsYou(t *testing.T) {
	b := view.Board{W: 120, H: 30, Total: 2, Cols: []view.Col{{Name: "todo", Rows: []view.Row{
		{ID: "T-1", Project: "p", Title: "Quiet"},
		{ID: "T-2", Project: "p", Title: "Needs you", Star: true},
	}}}}
	for _, ln := range strings.Split(plain(view.RenderBoard(b)), "\n") {
		hasStar := strings.Contains(ln, "*")
		if strings.Contains(ln, "T-2") && !hasStar {
			t.Errorf("строка T-2 без звёздочки: %q", ln)
		}
		if strings.Contains(ln, "T-1") && hasStar {
			t.Errorf("строка T-1 со звёздочкой: %q", ln)
		}
	}
}

func TestRenderBoard_SelectedColumnStaysVisibleWhenWindowIsNarrow(t *testing.T) {
	// Колонки по 24 знака: в 60 знаков влезают две, окно идёт за курсором.
	b := bigBoard(3)
	b.W, b.H = 60, 30
	for col, name := range colNames {
		b.Col, b.Row = col, 2
		out := plain(view.RenderBoard(b))
		if !strings.Contains(out, name) {
			t.Errorf("курсор в колонке %q: её нет на экране:\n%s", name, out)
		}
		id := b.Cols[col].Rows[2].ID + " "
		if !strings.Contains(out, id) {
			t.Errorf("курсор на %s: строки нет на экране:\n%s", id, out)
		}
	}
}

func TestRenderBoard_SelectedRowStaysVisibleInLongColumn(t *testing.T) {
	b := bigBoard(80)
	b.W, b.H = 120, 20
	b.Col, b.Row = 2, 70
	out := plain(view.RenderBoard(b))
	if id := b.Cols[2].Rows[70].ID + " "; !strings.Contains(out, id) {
		t.Errorf("курсор на %s в длинной колонке: строки нет на экране:\n%s", id, out)
	}
}

func TestRenderBoard_SelectionAndErrorLookDifferent(t *testing.T) {
	base := bigBoard(3)
	base.W, base.H = 120, 30
	a, b := base, base
	b.Row = 1
	if view.RenderBoard(a) == view.RenderBoard(b) {
		t.Error("выбор первой и второй строки рисуется одинаково")
	}
	ok, bad := base, base
	ok.Status, bad.Status = "write failed", "write failed"
	bad.IsErr = true
	if view.RenderBoard(ok) == view.RenderBoard(bad) {
		t.Error("ошибка в строке состояния выглядит как обычный статус")
	}
	if !strings.Contains(plain(view.RenderBoard(bad)), "write failed") {
		t.Error("текст строки состояния не показан")
	}
}

func card() view.Card {
	at := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	return view.Card{
		ID: "T-12", Title: "Report on calls", Column: "doing", Fields: "alpha   due 28.10   @claude",
		Abstract: "abstract text", Summary: "summary text", Details: "details text", Checks: "1/3",
		History: []history.Entry{
			{At: at, Actor: "claude", Op: "new"},
			{At: at.Add(time.Hour), Actor: "claude", Op: "set", Text: "3 edits", Folded: []history.Entry{
				{Op: "set"}, {Op: "set"}, {Op: "set"}}},
			{At: at.Add(2 * time.Hour), Actor: "daniil", Op: "comment", Text: "use a dash for empty values"},
		},
		W: 100, H: 40,
	}
}

func TestRenderCard_ShowsEverySectionAndHistory(t *testing.T) {
	out := plain(view.RenderCard(card()))
	for _, want := range []string{"T-12", "Report on calls", "doing", "alpha", "abstract text", "summary text",
		"details text", "claude", "new", "3 edits", "daniil", "use a dash for empty values"} {
		if !strings.Contains(out, want) {
			t.Errorf("карточка без %q:\n%s", want, out)
		}
	}
}

func TestRenderCard_AskShownOnlyWhenSet(t *testing.T) {
	c := card()
	if out := plain(view.RenderCard(c)); strings.Contains(out, "needs you") {
		t.Errorf("пустое ask показано:\n%s", out)
	}
	c.Ask = "pick A or B"
	if out := plain(view.RenderCard(c)); !strings.Contains(out, "* needs you: pick A or B") {
		t.Errorf("ask не показано строкой `* needs you: <ask>`:\n%s", out)
	}
}

func TestRenderCard_AnySize_FitsScreen(t *testing.T) {
	long := strings.Repeat("длинная строка без переносов 日本語 ", 12)
	for _, sz := range []struct{ w, h int }{{120, 50}, {80, 24}, {60, 16}} {
		for _, n := range []int{0, 5, 200} {
			c := card()
			c.W, c.H = sz.w, sz.h
			c.Title, c.Abstract, c.Summary, c.Details, c.Fields = long, long, long, long+"\n"+long, long
			c.Ask, c.Status = long, long
			c.History = nil
			for i := 0; i < n; i++ {
				c.History = append(c.History, history.Entry{Actor: "claude", Op: "comment", Text: long})
			}
			for _, cur := range []int{0, n / 2, max(n-1, 0)} {
				c.Cursor, c.Scroll = cur, cur/2
				fits(t, fmt.Sprintf("карточка %dx%d, история %d, курсор %d", sz.w, sz.h, n, cur), view.RenderCard(c), sz.w, sz.h)
			}
		}
	}
}

func TestRenderInput_PromptAndTailOfLongText(t *testing.T) {
	short := plain(view.RenderInput("waiting for:", "vendor", 60))
	if !strings.Contains(short, "waiting for:") || !strings.Contains(short, "vendor") {
		t.Errorf("строка ввода без подсказки или текста: %q", short)
	}
	long := strings.Repeat("0123456789", 20) + "END"
	out := view.RenderInput("close reason:", long, 40)
	fits(t, "строка ввода", out, 40, 1)
	if p := plain(out); !strings.Contains(p, "END") || !strings.Contains(p, "close reason:") {
		t.Errorf("длинный текст: не видны подсказка и конец ввода: %q", p)
	}
}

// golden сверяет текст кадра с файлом в testdata; -update переписывает файл.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	got = plain(got)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil { //nolint:gosec // эталон в репозитории
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // путь собран из имени теста
	if err != nil {
		t.Fatalf("нет эталона %s: запусти `go test ./internal/tui/view -update` и проверь diff глазами", path)
	}
	if got != string(want) {
		t.Errorf("кадр %s разошёлся с эталоном.\nполучили:\n%s\nждали:\n%s", name, got, want)
	}
}

func TestGolden_BoardAndCard(t *testing.T) {
	b := view.Board{Project: "alpha", Total: 4, Col: 1, Row: 0, W: 130, H: 14, Status: "ok T-7 todo > doing", Cols: []view.Col{
		{Name: "idea"},
		{Name: "todo", Rows: []view.Row{
			{ID: "T-7", Project: "alpha", Title: "Fix login", Summary: "see notes", Meta: "1/3 28.10"},
			{ID: "T-9", Project: "alpha", Title: "Write docs", Star: true},
		}},
		{Name: "doing", Rows: []view.Row{{ID: "T-12", Project: "beta", Title: "Report on calls", Meta: "@claude"}}},
		{Name: "blocked", Rows: []view.Row{{ID: "T-15", Project: "beta", Title: "GPU access", Summary: "waiting for: invoice"}}},
		{Name: "done"},
	}}
	golden(t, "board", view.RenderBoard(b))
	golden(t, "card", view.RenderCard(card()))
}
