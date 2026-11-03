package view

import (
	"fmt"
	"strings"
)

// PickItem - a row of the project list. Value is the filter it sets ("" - all); Count shows Tasks.
type PickItem struct {
	Name, Value string
	Tasks       int
	Count       bool
}

// Picker - the project list screen: the query line, the rows, the selected one.
type Picker struct {
	Query string
	Items []PickItem
	Sel   int
	W, H  int
}

// RenderPicker draws the list; a long list scrolls so that the selected row stays in view.
func RenderPicker(p Picker) string {
	lines := []string{accentStyle.Render("project") + dimStyle.Render("  type to narrow  enter set  esc back"), ruleLine(max(min(p.W, 60), 1))}
	room := len(p.Items)
	if p.H > 0 {
		room = max(p.H-3, 1) // heading, rule, query line
	}
	top := max(p.Sel-room+1, 0)
	for i := top; i < len(p.Items) && i < top+room; i++ {
		it := p.Items[i]
		name := clean(it.Name)
		count := ""
		if it.Count {
			count = fmt.Sprintf(" %d", it.Tasks)
		}
		w := p.W
		if w <= 0 {
			w = 40
		}
		name = truncate(name, max(w-len(count)-2, 1))
		row := fit(" "+name, max(w-len(count), 1)) + count
		if i == p.Sel {
			row = selStyle.Render(row)
		}
		lines = append(lines, row)
	}
	if len(p.Items) == 0 {
		lines = append(lines, dimStyle.Render(" no match"))
	}
	lines = append(lines, RenderInput("find:", p.Query, p.W))
	if p.H > 0 && len(lines) > p.H {
		lines = append(lines[:p.H-1], lines[len(lines)-1])
	}
	return strings.Join(lines, "\n")
}
