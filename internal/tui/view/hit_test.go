package view_test

import (
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/tui/view"
)

// hitCases - sizes and cursor positions where the column and card windows are shifted differently.
var hitCases = []struct {
	name     string
	rows     int
	w, h     int
	col, row int
}{
	{"wide", 3, 140, 40, 1, 0},
	{"narrow cursor far", 4, 80, 30, 4, 2},
	{"narrow cursor first", 4, 80, 30, 0, 0},
	{"scrolled rows", 12, 100, 24, 2, 9},
	{"tiny", 5, 40, 12, 3, 4},
	{"short frame", 5, 80, 6, 1, 3},
}

// cellText - the frame line characters in cells [from, from+n).
func cellText(line string, from, n int) string {
	r := []rune(line)
	if from >= len(r) {
		return ""
	}
	return string(r[from:min(from+n, len(r))])
}

// Every cell that HitBoard gives to a card lies over the text of exactly that card
// in the drawn frame: a hit does not diverge from drawing with shifted windows.
func TestHitBoard_CardCell_MatchesRenderedCard(t *testing.T) {
	for _, tc := range hitCases {
		t.Run(tc.name, func(t *testing.T) {
			b := bigBoard(tc.rows)
			b.W, b.H, b.Col, b.Row = tc.w, tc.h, tc.col, tc.row
			lines := strings.Split(plain(view.RenderBoard(b)), "\n")
			for y := 0; y < tc.h; y++ {
				for x := 0; x < tc.w; x++ {
					h := view.HitBoard(b, x, y)
					if h.Row < 0 {
						continue
					}
					head := 4 + (y-4)/5*5
					want := b.Cols[h.Col].Rows[h.Row].ID + " "
					slot := x / 26 * 26
					if head >= len(lines) || !strings.Contains(cellText(lines[head], slot, 24), want) {
						t.Fatalf("(%d,%d) -> col %d row %d, but frame line %d at x=%d has %q, want %q",
							x, y, h.Col, h.Row, head, slot, cellText(lines[head], slot, 24), want)
					}
				}
			}
		})
	}
}

// headCells - the drawn first card lines: the cell where the ID starts and the ID itself.
func headCells(lines []string) (cells [][2]int, ids []string) {
	for y := 4; y < len(lines); y += 5 {
		for slot := 0; slot*26 < len([]rune(lines[y])); slot++ {
			cell := strings.TrimSpace(cellText(lines[y], slot*26, 24))
			if id, _, ok := strings.Cut(strings.TrimPrefix(cell, ">"), " "); ok && strings.HasPrefix(id, "T-") {
				cells = append(cells, [2]int{slot*26 + 1, y})
				ids = append(ids, id)
			}
		}
	}
	return cells, ids
}

// Every drawn card is found by the cell with its ID.
func TestHitBoard_RenderedCard_IsHitOnItsFirstLine(t *testing.T) {
	for _, tc := range hitCases {
		t.Run(tc.name, func(t *testing.T) {
			b := bigBoard(tc.rows)
			b.W, b.H, b.Col, b.Row = tc.w, tc.h, tc.col, tc.row
			cells, ids := headCells(strings.Split(plain(view.RenderBoard(b)), "\n"))
			if len(cells) == 0 && tc.h > 7 { // under 7 lines the body is not drawn
				t.Fatal("no card rendered")
			}
			for i, c := range cells {
				h := view.HitBoard(b, c[0], c[1])
				if h.Row < 0 || b.Cols[h.Col].Rows[h.Row].ID != ids[i] {
					t.Errorf("card %s drawn at (%d,%d), hit %+v", ids[i], c[0], c[1], h)
				}
			}
		})
	}
}

func TestHitBoard_NotACard_IsNone(t *testing.T) {
	b := bigBoard(2)
	b.W, b.H, b.Col, b.Row = 80, 30, 0, 0
	for _, tc := range []struct {
		name string
		x, y int
		want view.Hit
	}{
		{"header", 5, 0, view.Hit{Col: -1, Row: -1}},
		{"header rule", 5, 1, view.Hit{Col: -1, Row: -1}},
		{"gap between columns", 24, 5, view.Hit{Col: -1, Row: -1}},
		{"gap second", 25, 5, view.Hit{Col: -1, Row: -1}},
		{"right of last shown", 79, 5, view.Hit{Col: -1, Row: -1}},
		{"negative", -1, 5, view.Hit{Col: -1, Row: -1}},
		{"status line", 5, 28, view.Hit{Col: -1, Row: -1}},
		{"hint line", 5, 29, view.Hit{Col: -1, Row: -1}},
		{"column name", 5, 2, view.Hit{Col: 0, Row: -1}},
		{"first card top", 0, 4, view.Hit{Col: 0, Row: 0}},
		{"first card blank fifth line", 23, 8, view.Hit{Col: 0, Row: 0}},
		{"second card top", 3, 9, view.Hit{Col: 0, Row: 1}},
		{"empty space under cards", 3, 14, view.Hit{Col: 0, Row: -1}},
		{"second column", 26, 4, view.Hit{Col: 1, Row: 0}},
		{"third column last cell", 75, 4, view.Hit{Col: 2, Row: 0}},
	} {
		if got := view.HitBoard(b, tc.x, tc.y); got != tc.want {
			t.Errorf("%s (%d,%d): got %+v, want %+v", tc.name, tc.x, tc.y, got, tc.want)
		}
	}
}

func TestHitBoard_ShiftedColumnWindow_UsesAbsoluteIndex(t *testing.T) {
	b := bigBoard(2)
	b.W, b.H, b.Col, b.Row = 80, 30, 4, 0 // three columns per page, cursor on the fifth: 3 and 4 are shown
	got := view.HitBoard(b, 0, 4)
	if got.Col != 2 && got.Col != 3 {
		t.Fatalf("slot 0 of the shifted window hit column %d, want first shown column", got.Col)
	}
	if got.Col == 0 {
		t.Fatalf("hit ignores the column window shift")
	}
}

func TestHitBoard_NoColumns_IsNone(t *testing.T) {
	if got := view.HitBoard(view.Board{W: 80, H: 30}, 3, 5); got.Col != -1 || got.Row != -1 {
		t.Fatalf("got %+v", got)
	}
}

// A sticky window: while the cursor is inside the window, the previous first column and top card stay;
// when it leaves, the window is set by the cursor, as without a hint.
func TestFitWindow_CursorInside_KeepsWindow_OutsideMoves(t *testing.T) {
	b := bigBoard(8)
	b.W, b.H = 80, 24 // three columns, three cards
	for _, tc := range []struct {
		name                 string
		col, row, first, top int
		wantFirst, wantTop   int
	}{
		{"cursor inside both windows", 2, 6, 2, 5, 2, 5},
		{"cursor left of columns", 1, 6, 2, 5, 0, 5},
		{"cursor above rows", 2, 3, 2, 5, 2, 3},
		{"cursor below rows", 3, 7, 2, 4, 2, 5},
		{"no hint", 4, 0, 0, 0, 2, 0},
	} {
		b.Col, b.Row, b.FirstCol, b.RowTop = tc.col, tc.row, tc.first, tc.top
		first, top := view.FitWindow(b)
		if first != tc.wantFirst || top != tc.wantTop {
			t.Errorf("%s: got first=%d top=%d, want first=%d top=%d", tc.name, first, top, tc.wantFirst, tc.wantTop)
		}
	}
}
