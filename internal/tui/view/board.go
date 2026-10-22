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

func rowCells(row Row, selected bool) []string {
	state := stateOf(row)
	look := scheme[state]
	kind, text, _ := task.ParseAsk(strings.TrimSpace(row.Ask))
	signStyle, selSign := look.Title, selKindStyle(kind)
	if look.KindColor {
		signStyle = kindStyle(kind)
	}
	marker := " "
	switch {
	case row.Grabbed:
		marker = "="
	case selected:
		marker = ">"
	}
	title := " " + clean(row.Title)
	summary, third := pulseLine(row, text, look, kindStyle(kind))
	blank := strings.Repeat(" ", colWidth)

	if selected || row.Grabbed {
		sel := selStyle
		if row.Grabbed {
			sel, selSign = grabStyle, grabStyle
		}
		head := marker + clean(row.ID+" "+row.Project)
		first := sel.Render(fit(head, colWidth))
		pulse := sel
		if look.Sign != "" {
			first = sel.Render(fit(head, colWidth-2)+" ") + selSign.Render(look.Sign)
			pulse = selSign
		}
		return []string{
			first,
			sel.Render(fit(title, colWidth)),
			pulse.Render(fit(summary, colWidth)),
			sel.Render(fit(" "+clean(row.Meta), colWidth)),
			blank,
		}
	}
	first := headLine(marker, row, look, colWidth)
	if look.Sign != "" {
		first = headLine(marker, row, look, colWidth-2) + " " + signStyle.Render(look.Sign)
	}
	return []string{first, look.Title.Render(fit(title, colWidth)), third.Render(fit(summary, colWidth)), metaLine(row, look.Rest), blank}
}

// headLine draws the ID line: the ID in the state style, the project in its palette color.
func headLine(marker string, row Row, look cardLook, width int) string {
	id := marker + clean(row.ID) + " "
	text := fit(id+clean(row.Project), width)
	rest, ok := strings.CutPrefix(text, id)
	if !ok {
		return look.Head.Render(text)
	}
	label := strings.TrimRight(rest, " ")
	return look.Head.Render(id) + projectStyle(row.Project).Render(label) + strings.Repeat(" ", len(rest)-len(label))
}

// metaLine draws the meta line; the Overdue part of it gets the overdue style.
func metaLine(row Row, rest lipgloss.Style) string {
	text := fit(" "+clean(row.Meta), colWidth)
	if row.Overdue != "" {
		if before, after, ok := strings.Cut(text, clean(row.Overdue)); ok {
			return rest.Render(before) + overdueStyle.Render(clean(row.Overdue)) + rest.Render(after)
		}
	}
	return rest.Render(text)
}

// pulseLine picks the third card line and its style: ask (askText, the kind style on a card that
// takes the kind color), note, summary.
func pulseLine(row Row, askText string, look cardLook, askFg lipgloss.Style) (string, lipgloss.Style) {
	switch {
	case strings.TrimSpace(row.Ask) != "":
		if look.KindColor {
			return " " + clean(firstLine("? "+askText)), askFg
		}
		return " " + clean(firstLine("? "+askText)), look.Rest
	case strings.TrimSpace(row.Note) != "":
		return " " + clean(firstLine(strings.TrimSpace(row.Note))), look.Rest
	}
	return " " + clean(firstLine(row.Summary)), look.Rest
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
