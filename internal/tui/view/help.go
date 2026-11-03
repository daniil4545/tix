package view

import "strings"

// helpEntry - a help line: a key or gesture and the action; an empty key is a section heading.
type helpEntry struct{ keys, what string }

var helpEntries = []helpEntry{
	{"", "Board"},
	{"arrows, h j k l", "select a column and a card"},
	{"space", "grab the card: arrows move it, space or enter drops, esc cancels"},
	{"enter", "open the card"},
	{"n", "new task in the selected column"},
	{"e", "edit the text in $EDITOR"},
	{"c", "comment"},
	{"J K", "move the card down or up in its column"},
	{"p", "project list: type to narrow, up and down choose, enter sets, esc keeps"},
	{"delete", "delete the task, asks y/n"},
	{"?", "this help"},
	{"q", "quit"},
	{"", "Card"},
	{"j k", "step through the history"},
	{"enter", "check a criterion; on a history entry: unfold edits, or show all history and back"},
	{"h l, arrows", "previous or next task of the column"},
	{"e c delete", "as on the board"},
	{"esc", "back to the board"},
	{"", "Mouse"},
	{"click", "select a card; double click opens it; a hint button presses its key"},
	{"drag", "drag a card to another column to move it"},
	{"+ in a column name", "new task in that column"},
	{"wheel", "scroll the column or the history"},
}

const helpKeyWidth = 20

// RenderHelp draws the help screen: all keys and mouse actions. W, H <= 0: the size is not limited.
func RenderHelp(w, h int) string {
	lines := []string{accentStyle.Render("tix help") + dimStyle.Render("  esc or ? back")}
	for _, e := range helpEntries {
		if e.keys == "" {
			lines = append(lines, accentStyle.Render(e.what))
			continue
		}
		lines = append(lines, truncate("  "+fit(e.keys, helpKeyWidth)+e.what, w))
	}
	if h > 0 && len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}
