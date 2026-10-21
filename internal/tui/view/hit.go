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
// Plus - клетка кнопки новой задачи: последний знак имени колонки.
type Hit struct {
	Col, Row int
	Plus     bool
}

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
		plus := y == columnsTop && x%stride == colWidth-1 && b.Cols[col].Name != UnknownColumn
		return Hit{Col: col, Row: -1, Plus: plus}
	}
	offset, end := rowWindow(b, col, bodyHeight)
	if row := offset + (y-bodyTop)/cardLines; row < end {
		return Hit{Col: col, Row: row}
	}
	return Hit{Col: col, Row: -1}
}

// HitHint находит кнопку строки подсказки под клеткой (x, y) кадра RenderBoard(b) и возвращает
// имя её клавиши (как у tea.KeyPressMsg.String). Пара, обрезанная по ширине, не кнопка.
// Высота кадра не задана или ниже рамки доски: подсказки нет.
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
