package view

const (
	// columnsTop - the first board frame line where a column accepts the mouse: the column names.
	// Lines 0 and 1 are the header and the line under it.
	columnsTop = 2
	// bodyTop - the first line of the column bodies: column names and lines come before it.
	bodyTop = 4
)

// Hit - where a screen cell landed on a board frame. Col - the column index in Board.Cols,
// Row - the card index in its Rows; -1 if the cell is outside a column or not on a card.
// Plus - the cell of the new task button: the last character of the column name.
type Hit struct {
	Col, Row int
	Plus     bool
}

// HitBoard finds the column and card under cell (x, y) of the RenderBoard(b) frame: the same column
// and card windows and the same layout constants as in drawing, so the window shift is accounted for.
// A column accepts cells from the names line to the end of the body, including the empty space under the cards;
// the gap between columns, the header and the bottom lines belong to nobody. The fifth card line is empty and
// counts as the same card.
func HitBoard(b Board, x, y int) Hit {
	none := Hit{Col: -1, Row: -1}
	if x < 0 || y < columnsTop || (b.H > 0 && y >= b.H) {
		return none
	}
	first, last := columnWindow(len(b.Cols), b.Col, b.W, b.FirstCol)
	stride := colWidth + colGap
	slot := x / stride
	if x%stride >= colWidth || first+slot >= last {
		return none
	}
	col := first + slot
	bodyHeight := boardBodyHeight(b.H)
	// The frame height is set: the body takes bodyHeight lines, on a low frame there is none.
	if b.H > 0 && y >= bodyTop+bodyHeight {
		return none
	}
	if y < bodyTop {
		plus := y == columnsTop && x%stride == colWidth-1 && b.Cols[col].Name != UnknownColumn
		return Hit{Col: col, Row: -1, Plus: plus}
	}
	offset, end := rowWindow(b, col, bodyHeight)
	if row := offset + (y-bodyTop)/cardLines; row < end {
		return Hit{Col: col, Row: row}
	}
	return Hit{Col: col, Row: -1}
}

// HitHint finds the hint-line button under cell (x, y) of the RenderBoard(b) frame and returns
// the name of its key (as in tea.KeyPressMsg.String). A pair cut by the width is not a button.
// Frame height not set or lower than the board frame: no hints.
func HitHint(b Board, x, y int) (key string, ok bool) {
	if b.H < boardFrame || y != b.H-1 {
		return "", false
	}
	for _, s := range hintSpans {
		if s.key != "" && x >= s.x0 && x < s.x1 && (b.W <= 0 || s.x1 <= b.W) {
			return s.key, true
		}
	}
	return "", false
}
