// Package view draws the board and the card as a string; no IO and no state.
package view

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/daniil4545/tix/internal/history"
)

// Row - a board column line. The third card line (the pulse) is the first of: the Ask question (yellow),
// the Note note (a ready line "<age> <text>", empty outside the active column), Summary.
type Row struct {
	ID, Project, Title, Summary, Meta string
	Ask, Note                         string
	Star                              bool // a star in the first line; a non-zero Ask turns it on itself
	Grabbed                           bool // the card is grabbed: highlight and the "=" marker instead of ">"
}

// UnknownColumn - the column name for tasks with a status that is not in config.toml.
const UnknownColumn = "?"

// Col - a board column.
type Col struct {
	Name string
	Rows []Row
}

// Board - everything needed for a board frame. W, H <= 0: the size is not limited.
type Board struct {
	Project  string
	Total    int
	Cols     []Col
	Col, Row int
	W, H     int
	Status   string
	IsErr    bool
	ByDue    bool // columns are sorted by due, not by rank
	// FirstCol and RowTop - the previous window position (the first shown column and the top card
	// of the selected column). Zeros give cursor windows; the window shifts only when the cursor left.
	FirstCol, RowTop int
}

// Card - everything needed for a card frame. Scroll - the body offset in lines; the frame
// shifts it itself so that the Cursor line (an index in History) is visible. W, H <= 0: the size is not limited.
type Card struct {
	ID, Title, Column, Fields, Ask, Abstract, Summary, Details, Checks string
	History                                                            []history.Entry
	Cursor, Scroll, W, H                                               int
	CritAt                                                             []int // criterion rows in Details (indexes by "\n"); Cursor counts them before the history
	Status                                                             string
	IsErr                                                              bool
}

// Layout colors: main - the terminal color, dim, accent, yellow (action needed), orange (error).
var (
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#69756F"))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7FD1C3"))
	askStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E9CF6A"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8A06A"))
	selStyle    = lipgloss.NewStyle().Background(lipgloss.Color("#7FD1C3")).Foreground(lipgloss.Color("#111416"))
	selAskStyle = lipgloss.NewStyle().Background(lipgloss.Color("#7FD1C3")).Foreground(lipgloss.Color("#E9CF6A"))
	grabStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#E9CF6A")).Foreground(lipgloss.Color("#111416"))
)

// RenderInput draws the input line: the prompt, the text and the cursor. A long text is shown by its tail.
func RenderInput(prompt, text string, w int) string {
	prompt = clean(prompt)
	text = clean(text)
	if w > 0 {
		room := max(w-lipgloss.Width(prompt)-2, 0)
		text = tail(text, room)
	}
	return accentStyle.Render(prompt) + " " + text + selStyle.Render(" ")
}

// statusLine draws the status line; an empty one is replaced by the key hint.
func statusLine(status, hint string, isErr bool, w int) string {
	if status == "" {
		return dimStyle.Render(truncate(hint, w))
	}
	status = truncate(clean(status), w)
	if isErr {
		return errStyle.Render(status)
	}
	return status
}

// clean replaces control characters with spaces: task text comes from files and must not move the terminal cursor.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// truncate cuts a string to n columns; n <= 0 - no cut.
func truncate(s string, n int) string {
	if n <= 0 || lipgloss.Width(s) <= n {
		return s
	}
	head, _ := cut(s, n)
	return head
}

// tail keeps the last n columns of a string.
func tail(s string, n int) string {
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > n {
		r = r[1:]
	}
	return string(r)
}

// cut splits a string into a head up to n columns wide and the rest; at least one character goes to the head.
func cut(s string, n int) (head, rest string) {
	used := 0
	for i, r := range s {
		width := lipgloss.Width(string(r))
		if used+width > n && i > 0 {
			return s[:i], s[i:]
		}
		used += width
	}
	return s, ""
}

// fit cuts a string to n columns and pads it with spaces.
func fit(s string, n int) string {
	s = truncate(s, n)
	return s + strings.Repeat(" ", max(n-lipgloss.Width(s), 0))
}

// wrap wraps text by words into lines up to w wide; w <= 0 - no wrap.
func wrap(text string, w int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = clean(line)
		if w <= 0 {
			out = append(out, line)
			continue
		}
		out = append(out, wrapLine(line, w)...)
	}
	return out
}

func wrapLine(line string, w int) []string {
	var out []string
	cur := ""
	for _, word := range strings.Fields(line) {
		for lipgloss.Width(word) > w {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			var head string
			head, word = cut(word, w)
			out = append(out, head)
		}
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	return append(out, cur)
}

// ruleLine draws a dim line of hyphens.
func ruleLine(n int) string {
	return dimStyle.Render(strings.Repeat("-", max(n, 0)))
}

// joinEnds puts left on the left and right on the right in a line w wide; if it does not fit - only left.
func joinEnds(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if w <= 0 || right == "" {
		return left
	}
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}
