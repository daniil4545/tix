package view

import "strings"

// SearchRow - a found task: number, title and the excerpt around the match.
type SearchRow struct{ ID, Title, Excerpt string }

// Search - the search screen: the rows, the selected one, the switches and the query line.
type Search struct {
	Query       string
	Rows        []SearchRow
	Sel         int
	All, Closed bool
	Project     string // the board project, named by the scope switch
	W, H        int
}

// RenderSearch draws the result list with the switches in the heading; a long list scrolls.
func RenderSearch(s Search) string {
	scope := "all projects"
	if !s.All {
		scope = "project " + clean(s.Project)
	}
	closed := "closed hidden"
	if s.Closed {
		closed = "closed shown"
	}
	w := s.W
	if w <= 0 {
		w = 80
	}
	lines := []string{
		accentStyle.Render("find") + dimStyle.Render(truncate("  tab: "+scope+"  ctrl+t: "+closed+"  enter open  esc back", max(w-4, 1))),
		ruleLine(max(min(w, 60), 1)),
	}
	room := len(s.Rows)
	if s.H > 0 {
		room = max(s.H-3, 1) // heading, rule, query line
	}
	top := max(s.Sel-room+1, 0)
	for i := top; i < len(s.Rows) && i < top+room; i++ {
		r := s.Rows[i]
		head := truncate(" "+clean(r.ID)+" "+clean(r.Title), w)
		rest := ""
		if room := w - len([]rune(head)) - 2; room > 3 && r.Excerpt != "" {
			rest = "  " + truncate(clean(r.Excerpt), room)
		}
		row := head + dimStyle.Render(rest)
		if i == s.Sel {
			row = selStyle.Render(fit(head+rest, w))
		}
		lines = append(lines, row)
	}
	if len(s.Rows) == 0 {
		msg := " no match"
		if s.Query == "" {
			msg = " type to search the title, Abstract and criteria"
		}
		lines = append(lines, dimStyle.Render(msg))
	}
	lines = append(lines, RenderInput("find:", s.Query, s.W))
	if s.H > 0 && len(lines) > s.H {
		lines = append(lines[:s.H-1], lines[len(lines)-1])
	}
	return strings.Join(lines, "\n")
}
