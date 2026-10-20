package view

import (
	"fmt"
	"strings"
)

const (
	colWidth   = 24
	colGap     = 2
	cardLines  = 5 // ID и проект, название, сводка, мета, пустая
	boardFrame = 6 // шапка, линия, имена колонок, линии колонок, линия, состояние
	boardHint  = "h l column  j k card  enter open  m M move  J K order  p project  c comment  q quit"
)

// RenderBoard рисует доску: колонки по 24 знака, окно колонок за курсором.
func RenderBoard(b Board) string {
	first, last := columnWindow(len(b.Cols), b.Col, b.W)
	shown := last - first
	width := shown*colWidth + max(shown-1, 0)*colGap
	if b.W > 0 {
		width = min(width, b.W)
	}

	bodyHeight := 0
	if b.H > 0 {
		bodyHeight = max(b.H-boardFrame, 0)
	}
	cells := make([][]string, 0, shown)
	for i := first; i < last; i++ {
		cells = append(cells, columnCells(b, i, bodyHeight))
	}
	if b.H <= 0 {
		for _, c := range cells {
			bodyHeight = max(bodyHeight, len(c))
		}
	}

	lines := []string{boardHeader(b, first, last), ruleLine(width), columnNames(b, first, last)}
	rules := make([]string, shown)
	for i := range rules {
		rules[i] = ruleLine(colWidth)
	}
	lines = append(lines, strings.Join(rules, strings.Repeat(" ", colGap)))
	blank := strings.Repeat(" ", colWidth)
	for r := 0; r < bodyHeight; r++ {
		row := make([]string, shown)
		for i, c := range cells {
			row[i] = blank
			if r < len(c) {
				row[i] = c[r]
			}
		}
		lines = append(lines, strings.Join(row, strings.Repeat(" ", colGap)))
	}
	lines = append(lines, ruleLine(width), statusLine(b.Status, boardHint, b.IsErr, b.W))
	if b.H > 0 && len(lines) > b.H {
		lines = lines[:b.H]
	}
	return strings.Join(lines, "\n")
}

// columnWindow выбирает показанные колонки [first, last): страница, на которой стоит курсор.
func columnWindow(count, cursor, w int) (first, last int) {
	if count == 0 {
		return 0, 0
	}
	visible := count
	if w > 0 {
		visible = min(max((w+colGap)/(colWidth+colGap), 1), count)
	}
	cursor = min(max(cursor, 0), count-1)
	first = min(cursor/visible*visible, count-visible)
	return first, first + visible
}

func boardHeader(b Board, first, last int) string {
	project := b.Project
	if project == "" {
		project = "all"
	}
	noun := "tasks"
	if b.Total == 1 {
		noun = "task"
	}
	left := accentStyle.Render("tracker") + dimStyle.Render("  project: ") +
		accentStyle.Render(truncate(clean(project), colWidth)) + dimStyle.Render(fmt.Sprintf("  %d %s", b.Total, noun))
	var hidden []string
	if first > 0 {
		hidden = append(hidden, fmt.Sprintf("<%d", first))
	}
	if last < len(b.Cols) {
		hidden = append(hidden, fmt.Sprintf("%d>", len(b.Cols)-last))
	}
	return joinEnds(left, dimStyle.Render(strings.Join(hidden, " ")), b.W)
}

func columnNames(b Board, first, last int) string {
	names := make([]string, 0, last-first)
	for i := first; i < last; i++ {
		name := fit(fmt.Sprintf("%s %d", clean(b.Cols[i].Name), len(b.Cols[i].Rows)), colWidth)
		if i == b.Col {
			name = accentStyle.Render(name)
		}
		names = append(names, name)
	}
	return strings.Join(names, strings.Repeat(" ", colGap))
}

// columnCells рисует строки одной колонки; у выбранной колонки окно по высоте идёт за курсором.
func columnCells(b Board, index, bodyHeight int) []string {
	rows := b.Cols[index].Rows
	selected := index == b.Col
	offset, end := 0, len(rows)
	if bodyHeight > 0 {
		capacity := max((bodyHeight+1)/cardLines, 1)
		if selected {
			offset = max(b.Row-capacity+1, 0)
		}
		end = min(offset+capacity, len(rows))
	}
	var out []string
	for r := offset; r < end; r++ {
		out = append(out, rowCells(rows[r], selected && r == b.Row)...)
	}
	return out
}

func rowCells(row Row, selected bool) []string {
	marker := " "
	if selected {
		marker = ">"
	}
	head := marker + clean(row.ID+" "+row.Project)
	title := " " + clean(row.Title)
	summary := " " + clean(firstLine(row.Summary))
	meta := " " + clean(row.Meta)
	blank := strings.Repeat(" ", colWidth)

	if selected {
		star := ""
		if row.Star {
			star = " *"
		}
		return []string{
			selStyle.Render(fit(fit(head, colWidth-len(star))+star, colWidth)),
			selStyle.Render(fit(title, colWidth)),
			selStyle.Render(fit(summary, colWidth)),
			selStyle.Render(fit(meta, colWidth)),
			blank,
		}
	}
	first := fit(head, colWidth)
	if row.Star {
		first = fit(head, colWidth-2) + " " + askStyle.Render("*")
	}
	return []string{first, fit(title, colWidth), dimStyle.Render(fit(summary, colWidth)), dimStyle.Render(fit(meta, colWidth)), blank}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
