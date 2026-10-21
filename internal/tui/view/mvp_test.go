package view_test

// Тесты доводки MVP (docs/plans/mvp-finish.md, разделы 3, 4.A, 4.C). Зона A: поля Row.Ask, Row.Note,
// строка подсказки, цвет звёздочки. Пишутся по спеке, без чтения кода зоны A.

import (
	"fmt"
	"strings"
	"testing"

	"tracker/internal/tui/view"
)

const (
	yellowFg = "38;2;233;207;106" // #E9CF6A
	selBg    = "48;2;127;209;195" // #7FD1C3, подсветка выбранной строки
	hintHead = "arrows select  space grab  enter open"
)

// frameLines рисует доску и режет кадр на строки (текст без цвета).
func frameLines(b view.Board) []string {
	return strings.Split(strings.TrimRight(plain(view.RenderBoard(b)), "\n"), "\n")
}

// lineWith возвращает сырую строку (с цветовыми кодами), чей текст содержит sub.
func lineWith(t *testing.T, raw, sub string) string {
	t.Helper()
	for _, ln := range strings.Split(raw, "\n") {
		if strings.Contains(plain(ln), sub) {
			return ln
		}
	}
	t.Fatalf("в кадре нет строки с %q:\n%s", sub, plain(raw))
	return ""
}

func oneCol(rows ...view.Row) view.Col { return view.Col{Name: "doing", Rows: rows} }

// Сценарий 4.C.1: подсказка - всегда последняя строка, строка состояния над ней.
func TestRenderBoard_StatusSet_HintStaysLastLine(t *testing.T) {
	for _, status := range []string{"", "moved T-1 to doing"} {
		t.Run(fmt.Sprintf("status=%q", status), func(t *testing.T) {
			b := view.Board{Total: 1, W: 120, H: 24, Status: status,
				Cols: []view.Col{oneCol(view.Row{ID: "T-1", Title: "Alpha", Summary: "s"})}}
			ls := frameLines(b)
			if len(ls) != 24 {
				t.Fatalf("строк в кадре %d, ждали 24 (высота окна)", len(ls))
			}
			if last := ls[len(ls)-1]; !strings.HasPrefix(strings.TrimSpace(last), hintHead) {
				t.Errorf("последняя строка не подсказка: %q", last)
			}
			if got := strings.TrimSpace(ls[len(ls)-2]); got != status {
				t.Errorf("строка состояния над подсказкой: %q, ждали %q", got, status)
			}
		})
	}
}

func TestRenderBoard_NarrowWidth_HintTruncatedNotWrapped(t *testing.T) {
	b := view.Board{Total: 1, W: 40, H: 12, Status: "something happened",
		Cols: []view.Col{oneCol(view.Row{ID: "T-1", Title: "Alpha"})}}
	out := view.RenderBoard(b)
	fits(t, "доска 40x12", out, 40, 12)
	ls := frameLines(b)
	if last := ls[len(ls)-1]; !strings.HasPrefix(last, "arrows select") {
		t.Errorf("на ширине 40 подсказка пропала: %q", last)
	}
}

// Сценарий 4.C.2: звёздочка ask жёлтая и в выбранной, и в невыбранной строке.
func TestRenderBoard_Ask_YellowSelectedAndNot(t *testing.T) {
	b := view.Board{Total: 3, W: 120, H: 40, Col: 0, Row: 0, Cols: []view.Col{oneCol(
		view.Row{ID: "T-1", Title: "Alpha", Ask: "alpha-question", Meta: "m"},
		view.Row{ID: "T-2", Title: "Bravo", Ask: "bravo-question", Meta: "m"},
		view.Row{ID: "T-3", Title: "Charlie", Summary: "charlie-summary", Meta: "m"},
	)}}
	raw := view.RenderBoard(b)

	sel := lineWith(t, raw, "alpha-question")
	if !strings.Contains(sel, yellowFg) {
		t.Errorf("выбранная строка с ask без жёлтого переднего плана %s: %q", yellowFg, sel)
	}
	if !strings.Contains(sel, selBg) {
		t.Errorf("выбранная строка не подсвечена фоном %s: %q", selBg, sel)
	}
	unsel := lineWith(t, raw, "bravo-question")
	if !strings.Contains(unsel, yellowFg) {
		t.Errorf("невыбранная строка с ask без жёлтого %s: %q", yellowFg, unsel)
	}
	if strings.Contains(unsel, selBg) {
		t.Errorf("невыбранная строка подсвечена: %q", unsel)
	}
	if plainLine := lineWith(t, raw, "charlie-summary"); strings.Contains(plainLine, yellowFg) {
		t.Errorf("строка без ask окрашена жёлтым: %q", plainLine)
	}
}

// Сценарий 4.C.3 (вид): третью строку выбирает view: ask, затем note, затем summary.
func TestRenderBoard_ThirdLine_AskThenNoteThenSummary(t *testing.T) {
	cases := []struct {
		name    string
		row     view.Row
		want    string
		notWant []string
	}{
		{"ask важнее заметки и сводки", view.Row{Ask: "pick A or B", Note: "3h did things", Summary: "the summary"},
			"? pick A or B", []string{"did things", "the summary"}},
		{"нет ask - заметка вместо сводки", view.Row{Note: "3h did things", Summary: "the summary"},
			"3h did things", []string{"the summary", "?"}},
		{"нет ask и заметки - сводка", view.Row{Summary: "the summary"},
			"the summary", []string{"?"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.row.ID, c.row.Title, c.row.Meta = "T-1", "Alpha", "1/2"
			b := view.Board{Total: 1, W: 120, H: 24, Cols: []view.Col{oneCol(c.row)}}
			ls := frameLines(b)
			// карточка 0 занимает строки 4..8; третья строка карточки - строка 6 (раздел 3)
			if !strings.Contains(ls[6], c.want) {
				t.Errorf("строка 6 = %q, ждали %q\nкадр:\n%s", ls[6], c.want, strings.Join(ls, "\n"))
			}
			body := strings.Join(ls[4:9], "\n")
			for _, nw := range c.notWant {
				if strings.Contains(body, nw) {
					t.Errorf("карточка содержит %q, а не должна:\n%s", nw, body)
				}
			}
		})
	}
}

// Раздел 3: раскладка. Тесты зоны B считают попадание по ней же.
func TestRenderBoard_Layout_ColumnsAndCardsOnFixedCoordinates(t *testing.T) {
	var cols []view.Col
	for i, name := range []string{"c0", "c1", "c2"} {
		c := view.Col{Name: name}
		for r := 0; r < 3; r++ {
			c.Rows = append(c.Rows, view.Row{ID: fmt.Sprintf("T-%d%d", i, r), Title: fmt.Sprintf("title%d%d", i, r), Meta: "m"})
		}
		cols = append(cols, c)
	}
	b := view.Board{Total: 9, W: 120, H: 30, Cols: cols}
	ls := frameLines(b)
	for i, c := range cols {
		x := i * (24 + 2)
		if got := strings.Index(ls[2], c.Name); got < x || got > x+23 {
			t.Errorf("имя колонки %s на x=%d, ждали в [%d,%d]: %q", c.Name, got, x, x+23, ls[2])
		}
		for r := range c.Rows {
			checkCardCell(t, ls, i, r)
		}
	}
}

// checkCardCell: карточка r колонки i лежит в строках 4+r*5..+4 и в знаках слота i.
func checkCardCell(t *testing.T, ls []string, i, r int) {
	t.Helper()
	x, y := i*(24+2), 4+r*5
	title := fmt.Sprintf("title%d%d", i, r)
	if !strings.Contains(strings.Join(ls[y:y+5], "\n"), title) {
		t.Errorf("карточка %d/%d не в строках %d..%d:\n%s", i, r, y, y+4, strings.Join(ls, "\n"))
		return
	}
	for _, ln := range ls[y : y+5] {
		if idx := strings.Index(ln, title); idx >= 0 && (idx < x || idx > x+23) {
			t.Errorf("карточка %d/%d на x=%d, ждали в [%d,%d]", i, r, idx, x, x+23)
		}
	}
}

// Сценарий 4.C.6 (вид): окно колонок сдвигается, но слоты остаются i*26 от левого края.
func TestRenderBoard_ColumnWindowShifted_SlotsFromLeftEdge(t *testing.T) {
	b := bigBoard(1)
	b.Col, b.W, b.H = 4, 80, 24
	ls := frameLines(b)
	if strings.Contains(ls[2], colNames[0]) {
		t.Fatalf("при курсоре на последней колонке и ширине 80 первая колонка не должна быть видна: %q", ls[2])
	}
	if !strings.Contains(ls[2], colNames[4]) {
		t.Fatalf("колонка с курсором не видна: %q", ls[2])
	}
	// видимая колонка с индексом k стоит в слоте s = (позиция в окне), x = s*26
	for _, name := range colNames {
		if idx := strings.Index(ls[2], name); idx >= 0 && idx%26 > 1 {
			t.Errorf("имя %q на x=%d: не в начале слота шириной 26", name, idx)
		}
	}
}

// Окно колонки по высоте: карточка с курсором видна, карточки идут по 5 строк.
func TestRenderBoard_RowWindowScrolled_CursorCardVisibleOnGrid(t *testing.T) {
	b := bigBoard(30)
	b.Col, b.Row, b.W, b.H = 1, 17, 120, 24
	ls := frameLines(b)
	want := fmt.Sprintf("T-%d", 30+17+1) // колонка todo: номера 31..60
	seenAt := -1
	for y, ln := range ls {
		if strings.Contains(ln, want) {
			seenAt = y
		}
	}
	if seenAt < 0 {
		t.Fatalf("карточка с курсором %s не видна:\n%s", want, strings.Join(ls, "\n"))
	}
	if (seenAt-4)%5 != 0 {
		t.Errorf("карточка с курсором начинается в строке %d, ждали 4+5k", seenAt)
	}
}
