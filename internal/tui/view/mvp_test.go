package view_test

// Tests of the MVP finishing work. Zone A: the fields Row.Ask, Row.Note,
// the hint line, the star color. Written from the spec, without reading the code of zone A.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/tui/view"
)

const (
	yellowFg = "38;2;233;207;106" // #E9CF6A
	selBg    = "48;2;127;209;195" // #7FD1C3, highlight of the selected line
	hintHead = "arrows select  space grab  enter open"
)

// frameLines draws the board and cuts the frame into lines (text without color).
func frameLines(b view.Board) []string {
	return strings.Split(strings.TrimRight(plain(view.RenderBoard(b)), "\n"), "\n")
}

// lineWith returns the raw line (with color codes) whose text contains sub.
func lineWith(t *testing.T, raw, sub string) string {
	t.Helper()
	for _, ln := range strings.Split(raw, "\n") {
		if strings.Contains(plain(ln), sub) {
			return ln
		}
	}
	t.Fatalf("no line with %q in the frame:\n%s", sub, plain(raw))
	return ""
}

func oneCol(rows ...view.Row) view.Col { return view.Col{Name: "doing", Rows: rows} }

// Scenario: the hint is always the last line, the status line is above it.
func TestRenderBoard_StatusSet_HintStaysLastLine(t *testing.T) {
	for _, status := range []string{"", "moved T-1 to doing"} {
		t.Run(fmt.Sprintf("status=%q", status), func(t *testing.T) {
			b := view.Board{Total: 1, W: 120, H: 24, Status: status,
				Cols: []view.Col{oneCol(view.Row{ID: "T-1", Title: "Alpha", Summary: "s"})}}
			ls := frameLines(b)
			if len(ls) != 24 {
				t.Fatalf("lines in the frame %d, expected 24 (window height)", len(ls))
			}
			if last := ls[len(ls)-1]; !strings.HasPrefix(strings.TrimSpace(last), hintHead) {
				t.Errorf("the last line is not the hint: %q", last)
			}
			if got := strings.TrimSpace(ls[len(ls)-2]); got != status {
				t.Errorf("status line above the hint: %q, expected %q", got, status)
			}
		})
	}
}

func TestRenderBoard_NarrowWidth_HintTruncatedNotWrapped(t *testing.T) {
	b := view.Board{Total: 1, W: 40, H: 12, Status: "something happened",
		Cols: []view.Col{oneCol(view.Row{ID: "T-1", Title: "Alpha"})}}
	out := view.RenderBoard(b)
	fits(t, "board 40x12", out, 40, 12)
	ls := frameLines(b)
	if last := ls[len(ls)-1]; !strings.HasPrefix(last, "arrows select") {
		t.Errorf("at width 40 the hint vanished: %q", last)
	}
}

// Scenario: the ask star is yellow in both the selected and the unselected line.
func TestRenderBoard_Ask_YellowSelectedAndNot(t *testing.T) {
	b := view.Board{Total: 3, W: 120, H: 40, Col: 0, Row: 0, Cols: []view.Col{oneCol(
		view.Row{ID: "T-1", Title: "Alpha", Ask: "alpha-question", Meta: "m"},
		view.Row{ID: "T-2", Title: "Bravo", Ask: "bravo-question", Meta: "m"},
		view.Row{ID: "T-3", Title: "Charlie", Summary: "charlie-summary", Meta: "m"},
	)}}
	raw := view.RenderBoard(b)

	sel := lineWith(t, raw, "alpha-question")
	if !strings.Contains(sel, yellowFg) {
		t.Errorf("selected line with ask without yellow foreground %s: %q", yellowFg, sel)
	}
	if !strings.Contains(sel, selBg) {
		t.Errorf("selected line is not highlighted with background %s: %q", selBg, sel)
	}
	unsel := lineWith(t, raw, "bravo-question")
	if !strings.Contains(unsel, yellowFg) {
		t.Errorf("unselected line with ask without yellow %s: %q", yellowFg, unsel)
	}
	if strings.Contains(unsel, selBg) {
		t.Errorf("unselected line is highlighted: %q", unsel)
	}
	if plainLine := lineWith(t, raw, "charlie-summary"); strings.Contains(plainLine, yellowFg) {
		t.Errorf("line without ask is colored yellow: %q", plainLine)
	}
}

// Scenario (view): the third line is chosen by view: ask, then note, then summary.
func TestRenderBoard_ThirdLine_AskThenNoteThenSummary(t *testing.T) {
	cases := []struct {
		name    string
		row     view.Row
		want    string
		notWant []string
	}{
		{"ask outranks the note and the summary", view.Row{Ask: "pick A or B", Note: "3h did things", Summary: "the summary"},
			"? pick A or B", []string{"did things", "the summary"}},
		{"no ask - the note instead of the summary", view.Row{Note: "3h did things", Summary: "the summary"},
			"3h did things", []string{"the summary", "?"}},
		{"no ask and no note - the summary", view.Row{Summary: "the summary"},
			"the summary", []string{"?"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.row.ID, c.row.Title, c.row.Meta = "T-1", "Alpha", "1/2"
			b := view.Board{Total: 1, W: 120, H: 24, Cols: []view.Col{oneCol(c.row)}}
			ls := frameLines(b)
			// card 0 takes lines 4..8; the third card line is line 6
			if !strings.Contains(ls[6], c.want) {
				t.Errorf("line 6 = %q, expected %q\nframe:\n%s", ls[6], c.want, strings.Join(ls, "\n"))
			}
			body := strings.Join(ls[4:9], "\n")
			for _, nw := range c.notWant {
				if strings.Contains(body, nw) {
					t.Errorf("the card contains %q but must not:\n%s", nw, body)
				}
			}
		})
	}
}

// Layout. The zone B tests compute hits by it.
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
			t.Errorf("column name %s at x=%d, expected in [%d,%d]: %q", c.Name, got, x, x+23, ls[2])
		}
		for r := range c.Rows {
			checkCardCell(t, ls, i, r)
		}
	}
}

// checkCardCell: card r of column i lies in lines 4+r*5..+4 and in the characters of slot i.
func checkCardCell(t *testing.T, ls []string, i, r int) {
	t.Helper()
	x, y := i*(24+2), 4+r*5
	title := fmt.Sprintf("title%d%d", i, r)
	if !strings.Contains(strings.Join(ls[y:y+5], "\n"), title) {
		t.Errorf("card %d/%d is not in lines %d..%d:\n%s", i, r, y, y+4, strings.Join(ls, "\n"))
		return
	}
	for _, ln := range ls[y : y+5] {
		if idx := strings.Index(ln, title); idx >= 0 && (idx < x || idx > x+23) {
			t.Errorf("card %d/%d at x=%d, expected in [%d,%d]", i, r, idx, x, x+23)
		}
	}
}

// Scenario (view): the column window shifts, but the slots stay i*26 from the left edge.
func TestRenderBoard_ColumnWindowShifted_SlotsFromLeftEdge(t *testing.T) {
	b := bigBoard(1)
	b.Col, b.W, b.H = 4, 80, 24
	ls := frameLines(b)
	if strings.Contains(ls[2], colNames[0]) {
		t.Fatalf("with the cursor on the last column and width 80 the first column must not be visible: %q", ls[2])
	}
	if !strings.Contains(ls[2], colNames[4]) {
		t.Fatalf("the column with the cursor is not visible: %q", ls[2])
	}
	// a visible column with index k stands in slot s = (position in the window), x = s*26
	for _, name := range colNames {
		if idx := strings.Index(ls[2], name); idx >= 0 && idx%26 > 1 {
			t.Errorf("name %q at x=%d: not at the start of a 26-wide slot", name, idx)
		}
	}
}

// The column window by height: the card with the cursor is visible, cards go by 5 lines.
func TestRenderBoard_RowWindowScrolled_CursorCardVisibleOnGrid(t *testing.T) {
	b := bigBoard(30)
	b.Col, b.Row, b.W, b.H = 1, 17, 120, 24
	ls := frameLines(b)
	want := fmt.Sprintf("T-%d", 30+17+1) // column todo: numbers 31..60
	seenAt := -1
	for y, ln := range ls {
		if strings.Contains(ln, want) {
			seenAt = y
		}
	}
	if seenAt < 0 {
		t.Fatalf("the card with the cursor %s is not visible:\n%s", want, strings.Join(ls, "\n"))
	}
	if (seenAt-4)%5 != 0 {
		t.Errorf("the card with the cursor starts at line %d, expected 4+5k", seenAt)
	}
}
