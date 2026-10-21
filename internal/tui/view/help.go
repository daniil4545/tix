package view

import "strings"

// helpEntry - строка справки: клавиша или жест и действие; пустая клавиша - заголовок раздела.
type helpEntry struct{ keys, what string }

var helpEntries = []helpEntry{
	{"", "Board"},
	{"arrows, h j k l", "select a column and a card"},
	{"space", "grab the card: arrows move it, space or enter drops, esc cancels"},
	{"enter", "open the card"},
	{"n", "new task in the selected column"},
	{"a", "answer the agent's question"},
	{"e", "edit the text in $EDITOR"},
	{"c", "comment"},
	{"g", "go to the agent's session"},
	{"o", "order by due date or by rank"},
	{"J K", "move the card down or up in its column"},
	{"p", "switch the project filter"},
	{"delete", "delete the task, asks y/n"},
	{"?", "this help"},
	{"q", "quit"},
	{"", "Card"},
	{"j k, enter", "step through the history, unfold an entry"},
	{"h l, arrows", "previous or next task of the column"},
	{"e c a g delete", "as on the board"},
	{"f", "edit a field: title, due, tags, ask, waiting, summary, external"},
	{"esc", "back to the board"},
	{"", "Mouse"},
	{"click", "select a card; double click opens it; a hint button presses its key"},
	{"drag", "drag a card to another column to move it"},
	{"+ in a column name", "new task in that column"},
	{"wheel", "scroll the column or the history"},
}

const helpKeyWidth = 20

// RenderHelp рисует экран справки: все клавиши и действия мыши. W, H <= 0: размер не ограничен.
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
