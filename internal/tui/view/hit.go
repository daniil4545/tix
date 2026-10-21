package view

const (
	// columnsTop - первая строка кадра доски, где колонка принимает мышь: имена колонок.
	// Строки 0 и 1 - шапка и линия под ней.
	columnsTop = 2
	// bodyTop - первая строка тела колонок: имена и линии колонок идут перед ней.
	bodyTop = 4
)

// Hit - куда попала клетка экрана на кадре доски. Col - индекс колонки в Board.Cols,
// Row - индекс карточки в её Rows; -1, если клетка вне колонки или не на карточке.
type Hit struct{ Col, Row int }

// HitBoard находит колонку и карточку под клеткой (x, y) кадра RenderBoard(b): те же окна
// колонок и карточек и те же константы раскладки, что у рисования, поэтому сдвиг окон учтён.
// Колонка принимает клетки от строки имён до конца тела, включая пустое место под карточками;
// промежуток между колонками, шапка и нижние строки ничьи. Пятая строка карточки пустая и
// считается той же карточкой.
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
	// Высота кадра задана: тело занимает bodyHeight строк, на низком кадре его нет вовсе.
	if b.H > 0 && y >= bodyTop+bodyHeight {
		return none
	}
	if y < bodyTop {
		return Hit{Col: col, Row: -1}
	}
	offset, end := rowWindow(b, col, bodyHeight)
	if row := offset + (y-bodyTop)/cardLines; row < end {
		return Hit{Col: col, Row: row}
	}
	return Hit{Col: col, Row: -1}
}
