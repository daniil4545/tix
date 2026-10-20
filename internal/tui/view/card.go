package view

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"tracker/internal/actor"
	"tracker/internal/history"
)

const (
	cardWidth   = 100 // ширина, если W не задан
	cardFrame   = 2   // линия и состояние внизу
	dateWidth   = 11  // "02.01 15:04"
	actorWidth  = 16
	cardHint    = "e editor  c comment  m M move  j k history  enter unfold  esc back"
	historyHead = "history"
)

// RenderCard рисует карточку: шапка, тело с историей, состояние. Тело прокручивается, шапка и низ стоят.
func RenderCard(c Card) string {
	w := c.W
	if w <= 0 {
		w = cardWidth
	}
	head := cardHead(c, w)
	body, curStart, curEnd := cardBody(c, w)

	lines := head
	if c.H > 0 {
		room := max(c.H-len(head)-cardFrame, 0)
		body = scrollBody(body, c.Scroll, curStart, curEnd, room)
	}
	lines = append(lines, body...)
	lines = append(lines, ruleLine(w), statusLine(c.Status, cardHint, c.IsErr, w))
	return strings.Join(lines, "\n")
}

func cardHead(c Card, w int) []string {
	column := " [" + clean(c.Column) + "]"
	title := truncate(clean(c.ID+"  "+c.Title), max(w-lipgloss.Width(column), 1))
	lines := []string{joinEnds(accentStyle.Render(title), dimStyle.Render(column), w)}
	if c.Fields != "" {
		lines = append(lines, dimStyle.Render(truncate(clean(c.Fields), w)))
	}
	if c.Ask != "" {
		for _, l := range wrap("* needs you: "+c.Ask, w) {
			lines = append(lines, askStyle.Render(l))
		}
	}
	return append(lines, ruleLine(w))
}

// cardBody собирает строки тела и границы выбранной записи истории (-1, если её нет).
func cardBody(c Card, w int) (lines []string, curStart, curEnd int) {
	curStart, curEnd = -1, -1
	section := func(title, right string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, joinEnds(title, dimStyle.Render(clean(right)), w))
	}
	indented := func(text string, dimRest bool) {
		for i, src := range strings.Split(text, "\n") {
			for _, l := range wrap(src, w-2) {
				if dimRest && i > 0 {
					l = dimStyle.Render(l)
				}
				lines = append(lines, "  "+l)
			}
		}
	}
	if strings.TrimSpace(c.Abstract) != "" {
		section("abstract", "")
		indented(c.Abstract, false)
	}
	if strings.TrimSpace(c.Summary) != "" {
		section("summary", "")
		indented(c.Summary, true)
	}
	if strings.TrimSpace(c.Details) != "" || c.Checks != "" {
		section("details", c.Checks)
		indented(c.Details, false)
	}
	section(historyHead, "")
	if len(c.History) == 0 {
		lines = append(lines, dimStyle.Render("  no history"))
	}
	for i, e := range c.History {
		start := len(lines)
		lines = append(lines, entryLines(e, i == c.Cursor, w)...)
		if i == c.Cursor {
			curStart, curEnd = start, len(lines)
		}
	}
	return lines, curStart, curEnd
}

// entryLines рисует запись истории; комментарий - заголовок и текст под ним, прочее - в одну строку с переносом.
func entryLines(e history.Entry, selected bool, w int) []string {
	op, text := clean(e.Op), e.Text
	if len(e.Folded) > 0 {
		op, text = fmt.Sprintf("%d edits", len(e.Folded)), ""
	}
	marker := "  "
	if selected {
		marker = "> "
	}
	prefix := marker + e.At.Format("02.01 15:04") + "  " + fit(clean(actor.Short(e.Actor)), actorWidth) + " "
	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	room := max(w-len(indent), 1)

	var out []string
	if e.Op == "comment" {
		out = append(out, prefix+op)
		for _, l := range wrap(text, room) {
			out = append(out, indent+l)
		}
	} else {
		body := wrap(strings.TrimSpace(op+"  "+text), room)
		for i, l := range body {
			if i == 0 {
				l = prefix + l
			} else {
				l = indent + l
			}
			out = append(out, l)
		}
	}
	for i, l := range out {
		switch {
		case selected:
			out[i] = selStyle.Render(fit(l, w))
		case e.Op == "comment" && i == 0:
			out[i] = dimStyle.Render(l)
		}
	}
	return out
}

// scrollBody оставляет room строк тела от смещения scroll; смещение подгоняется под выбранную запись.
func scrollBody(body []string, scroll, curStart, curEnd, room int) []string {
	if room <= 0 {
		return nil
	}
	if curStart >= 0 {
		if curEnd > scroll+room {
			scroll = curEnd - room
		}
		if curStart < scroll {
			scroll = curStart
		}
	}
	scroll = min(max(scroll, 0), max(len(body)-room, 0))
	return body[scroll:min(scroll+room, len(body))]
}
