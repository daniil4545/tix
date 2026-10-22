package view

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/daniil4545/tix/internal/task"
)

const (
	colWidth   = 24
	colGap     = 2
	cardLines  = 5   // ID and project, title, summary, meta, empty
	boardFrame = 7   // header, line, column names, column lines, line, state, hint
	plusMark   = "+" // the last character of a column name cell: the new task button
)

// hintItem - a hint pair: a key and a word. A button is any pair except arrows.
type hintItem struct{ key, word string }

var boardHints = []hintItem{
	{"arrows", "select"}, {"space", "grab"}, {"enter", "open"}, {"n", "new"}, {"a", "answer"},
	{"e", "edit"}, {"c", "comment"}, {"o", "order"}, {"p", "project"}, {"?", "help"}, {"q", "quit"},
}

// hintSpan - the place of a pair in the hint line: characters [x0, x1).
type hintSpan struct {
	key    string
	x0, x1 int
}

// boardHint and hintSpans are built once from boardHints: drawing and mouse hit testing share one layout.
var boardHint, hintSpans = layoutHints(boardHints)

func layoutHints(items []hintItem) (string, []hintSpan) {
	var sb strings.Builder
	spans := make([]hintSpan, 0, len(items))
	for i, it := range items {
		if i > 0 {
			sb.WriteString("  ")
		}
		x0 := lipgloss.Width(sb.String())
		sb.WriteString(it.key + " " + it.word)
		span := hintSpan{key: it.key, x0: x0, x1: lipgloss.Width(sb.String())}
		if it.key == "arrows" {
			span.key = ""
		}
		spans = append(spans, span)
	}
	return sb.String(), spans
}

// RenderBoard draws the board: columns of 24 characters, the column window follows the cursor.
func RenderBoard(b Board) string {
	first, last := columnWindow(len(b.Cols), b.Col, b.W, b.FirstCol)
	shown := last - first
	width := shown*colWidth + max(shown-1, 0)*colGap
	if b.W > 0 {
		width = min(width, b.W)
	}

	bodyHeight := boardBodyHeight(b.H)
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
	lines = append(lines, ruleLine(width), statusLine(b.Status, "", b.IsErr, b.W), dimStyle.Render(truncate(boardHint, b.W)))
	if b.H > 0 && len(lines) > b.H {
		lines = lines[:b.H]
	}
	return strings.Join(lines, "\n")
}

// boardBodyHeight - the column body height for frame height h; h <= 0: not limited (0).
func boardBodyHeight(h int) int {
	if h <= 0 {
		return 0
	}
	return max(h-boardFrame, 0)
}

// columnWindow picks the shown columns [first, last). The window is sticky: hint - the previous first
// column, it stays while the cursor is in the window; if the cursor left it - the page the cursor is on.
func columnWindow(count, cursor, w, hint int) (first, last int) {
	if count == 0 {
		return 0, 0
	}
	visible := count
	if w > 0 {
		visible = min(max((w+colGap)/(colWidth+colGap), 1), count)
	}
	cursor = min(max(cursor, 0), count-1)
	first = min(cursor/visible*visible, count-visible)
	if hint >= 0 && hint <= count-visible && cursor >= hint && cursor < hint+visible {
		first = hint
	}
	return first, first + visible
}

// FitWindow returns the board windows fitted to the cursor: the first shown column and the top
// card of the selected column. The model stores them in Board.FirstCol and Board.RowTop after each
// cursor move: drawing and mouse hit testing share one state, a click does not move windows.
func FitWindow(b Board) (firstCol, rowTop int) {
	if len(b.Cols) == 0 {
		return 0, 0
	}
	firstCol, _ = columnWindow(len(b.Cols), b.Col, b.W, b.FirstCol)
	rowTop, _ = rowWindow(b, min(max(b.Col, 0), len(b.Cols)-1), boardBodyHeight(b.H))
	return firstCol, rowTop
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
	left := accentStyle.Render("tix") + dimStyle.Render("  project: ") +
		accentStyle.Render(truncate(clean(project), colWidth)) + dimStyle.Render(fmt.Sprintf("  %d %s", b.Total, noun))
	if b.ByDue {
		left += dimStyle.Render("  order: due")
	}
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
		name := fmt.Sprintf("%s %d", clean(b.Cols[i].Name), len(b.Cols[i].Rows))
		if b.Cols[i].Name == UnknownColumn {
			name = fit(name, colWidth)
		} else {
			name = fit(name, colWidth-2) + " " + plusMark
		}
		if i == b.Col {
			name = accentStyle.Render(name)
		}
		names = append(names, name)
	}
	return strings.Join(names, strings.Repeat(" ", colGap))
}

// rowWindow - the cards of column index that fall into the window by height: indexes [offset, end).
// The selected column window is sticky: it starts at RowTop and shifts only when the cursor leaves
// it; the others stay at the top. bodyHeight <= 0: all.
func rowWindow(b Board, index, bodyHeight int) (offset, end int) {
	count := len(b.Cols[index].Rows)
	if bodyHeight <= 0 {
		return 0, count
	}
	capacity := max((bodyHeight+1)/cardLines, 1)
	if index == b.Col {
		offset = min(max(b.RowTop, 0), max(count-capacity, 0))
		if b.Row < offset {
			offset = b.Row
		}
		if b.Row >= offset+capacity {
			offset = b.Row - capacity + 1
		}
		offset = max(offset, 0)
	}
	return offset, min(offset+capacity, count)
}

// columnCells draws the lines of one column in the rowWindow window.
func columnCells(b Board, index, bodyHeight int) []string {
	rows := b.Cols[index].Rows
	selected := index == b.Col
	offset, end := rowWindow(b, index, bodyHeight)
	var out []string
	for r := offset; r < end; r++ {
		out = append(out, rowCells(rows[r], selected && r == b.Row)...)
	}
	return out
}

// askStyles returns the star and pulse style of a row, plain and on the selected line.
func askStyles(kind string, ended bool) (plain, selected lipgloss.Style) {
	color := askKindColors[kind]
	if ended {
		color = askEndedColor
	}
	fg := lipgloss.Color(color)
	return lipgloss.NewStyle().Foreground(fg), selAskStyle.Foreground(fg)
}

func rowCells(row Row, selected bool) []string {
	kind, text, _ := task.ParseAsk(strings.TrimSpace(row.Ask))
	askFg, selAsk := askStyles(kind, row.AskEnded && strings.TrimSpace(row.Ask) != "")
	star := row.Star || strings.TrimSpace(row.Ask) != ""
	marker := " "
	switch {
	case row.Grabbed:
		marker = "="
	case selected:
		marker = ">"
	}
	head := marker + clean(row.ID+" "+row.Project)
	title := " " + clean(row.Title)
	summary, third := pulseLine(row, text, askFg)
	meta := " " + clean(row.Meta)
	blank := strings.Repeat(" ", colWidth)

	if selected || row.Grabbed {
		sel := selStyle
		if row.Grabbed {
			sel, selAsk = grabStyle, grabStyle
		}
		first := sel.Render(fit(head, colWidth))
		if star {
			first = sel.Render(fit(head, colWidth-2)+" ") + selAsk.Render("*")
		}
		pulse := sel
		if strings.TrimSpace(row.Ask) != "" {
			pulse = selAsk
		}
		return []string{
			first,
			sel.Render(fit(title, colWidth)),
			pulse.Render(fit(summary, colWidth)),
			sel.Render(fit(meta, colWidth)),
			blank,
		}
	}
	first := fit(head, colWidth)
	if star {
		first = fit(head, colWidth-2) + " " + askFg.Render("*")
	}
	return []string{first, fit(title, colWidth), third.Render(fit(summary, colWidth)), dimStyle.Render(fit(meta, colWidth)), blank}
}

// pulseLine picks the third card line and its style: ask (askText, askFg), note, summary.
func pulseLine(row Row, askText string, askFg lipgloss.Style) (string, lipgloss.Style) {
	switch {
	case strings.TrimSpace(row.Ask) != "":
		return " " + clean(firstLine("? "+askText)), askFg
	case strings.TrimSpace(row.Note) != "":
		return " " + clean(firstLine(strings.TrimSpace(row.Note))), dimStyle
	}
	return " " + clean(firstLine(row.Summary)), dimStyle
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
