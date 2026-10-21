package view

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/history"
)

const (
	cardWidth   = 100 // ширина, если W не задан
	cardFrame   = 3   // линия, состояние и строка действий внизу
	dateWidth   = 11  // "02.01 15:04"
	actorWidth  = 16
	historyHead = "history"
)

// RenderCard рисует карточку: шапка, тело с историей, состояние, строка действий. Тело
// прокручивается, шапка и низ стоят.
func RenderCard(c Card) string {
	return strings.Join(layoutCard(c).lines, "\n")
}

// cardLay - кадр карточки и строки, на которые попадает мышь. Рисование и HitCard берут его
// из одного места, поэтому прокрутка тела не расходится с кликом.
type cardLay struct {
	lines          []string
	w              int
	askFrom, askTo int // строки вопроса агента [askFrom, askTo); -1, если вопроса нет
	fieldsRow      int // строка полей; -1, если её нет
	actionsRow     int
	actions        []actionSpan
	crits          []critSpan // видимые строки критериев
}

// critSpan - строки кадра [from, to), занятые критерием с номером index (с нуля).
type critSpan struct{ from, to, index int }

func layoutCard(c Card) cardLay {
	w := c.W
	if w <= 0 {
		w = cardWidth
	}
	head, lay := cardHead(c, w)
	body, curStart, curEnd, crits := cardBody(c, w)
	if c.H > 0 {
		room := max(c.H-len(head)-cardFrame, 0)
		var offset int
		body, offset = scrollBody(body, c.Scroll, curStart, curEnd, room)
		for i := range crits {
			crits[i][0], crits[i][1] = crits[i][0]-offset, crits[i][1]-offset
		}
	}
	for i, r := range crits {
		from, to := max(r[0], 0)+len(head), min(r[1], len(body))+len(head)
		if from < to {
			lay.crits = append(lay.crits, critSpan{from: from, to: to, index: i})
		}
	}
	lines := append(head, body...)
	text, spans := actionsText()
	lines = append(lines, ruleLine(w), statusLine(c.Status, "", c.IsErr, w), dimStyle.Render(truncate(text, w)))
	lay.lines, lay.w = lines, w
	lay.actionsRow, lay.actions = len(lines)-1, spans
	return lay
}

// cardHead собирает шапку и отмечает в cardLay её строки: поля и вопрос агента.
func cardHead(c Card, w int) ([]string, cardLay) {
	lay := cardLay{fieldsRow: -1, askFrom: -1, askTo: -1}
	column := " [" + clean(c.Column) + "]"
	title := truncate(clean(c.ID+"  "+c.Title), max(w-lipgloss.Width(column), 1))
	lines := []string{joinEnds(accentStyle.Render(title), dimStyle.Render(column), w)}
	if c.Fields != "" {
		lay.fieldsRow = len(lines)
		lines = append(lines, dimStyle.Render(truncate(clean(c.Fields), w)))
	}
	if c.Ask != "" {
		lay.askFrom = len(lines)
		for _, l := range wrap("* needs you: "+c.Ask, w) {
			lines = append(lines, askStyle.Render(l))
		}
		lay.askTo = len(lines)
	}
	return append(lines, ruleLine(w)), lay
}

// cardBody собирает строки тела, границы выбранной строки (критерий или запись истории; -1,
// если её нет) и строки каждого критерия в теле.
func cardBody(c Card, w int) (lines []string, curStart, curEnd int, crits [][2]int) {
	curStart, curEnd = -1, -1
	section := func(title, right string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, joinEnds(title, dimStyle.Render(clean(right)), w))
	}
	if strings.TrimSpace(c.Abstract) != "" {
		section("abstract", "")
		lines = append(lines, indentLines(c.Abstract, false, w)...)
	}
	if strings.TrimSpace(c.Summary) != "" {
		section("summary", "")
		lines = append(lines, indentLines(c.Summary, true, w)...)
	}
	if strings.TrimSpace(c.Details) != "" || c.Checks != "" {
		section("details", c.Checks)
		base := len(lines)
		dl, rel := detailLines(c, w)
		lines = append(lines, dl...)
		for k, r := range rel {
			crits = append(crits, [2]int{base + r[0], base + r[1]})
			if k == c.Cursor {
				curStart, curEnd = base+r[0], base+r[1]
			}
		}
	}
	section(historyHead, "")
	base := len(lines)
	hl, from, to := historyLines(c, w)
	lines = append(lines, hl...)
	if from >= 0 {
		curStart, curEnd = base+from, base+to
	}
	return lines, curStart, curEnd, crits
}

// historyLines рисует записи истории; from, to - строки выбранной записи внутри результата (-1, если её нет).
func historyLines(c Card, w int) (lines []string, from, to int) {
	from, to = -1, -1
	if len(c.History) == 0 {
		lines = append(lines, dimStyle.Render("  no history"))
	}
	for i, e := range c.History {
		start := len(lines)
		selected := i == c.Cursor-len(c.CritAt)
		lines = append(lines, entryLines(e, selected, w)...)
		if selected {
			from, to = start, len(lines)
		}
	}
	return lines, from, to
}

// indentLines выводит текст раздела с отступом; dimRest приглушает все строки текста после первой.
func indentLines(text string, dimRest bool, w int) []string {
	var out []string
	for i, src := range strings.Split(text, "\n") {
		for _, l := range wrap(src, w-2) {
			if dimRest && i > 0 {
				l = dimStyle.Render(l)
			}
			out = append(out, "  "+l)
		}
	}
	return out
}

// detailLines рисует раздел Details; выбранный критерий (Cursor) подсвечен. rel - строки каждого
// критерия внутри возвращённых строк.
func detailLines(c Card, w int) (lines []string, rel [][2]int) {
	critOf := map[int]int{}
	for k, at := range c.CritAt {
		critOf[at] = k
	}
	for i, src := range strings.Split(c.Details, "\n") {
		k, isCrit := critOf[i]
		start := len(lines)
		for _, l := range wrap(src, w-2) {
			l = "  " + l
			if isCrit && k == c.Cursor {
				l = selStyle.Render(fit(l, w))
			}
			lines = append(lines, l)
		}
		if isCrit {
			rel = append(rel, [2]int{start, len(lines)})
		}
	}
	return lines, rel
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
func scrollBody(body []string, scroll, curStart, curEnd, room int) ([]string, int) {
	if room <= 0 {
		return nil, 0
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
	return body[scroll:min(scroll+room, len(body))], scroll
}
