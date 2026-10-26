package view

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/history"
)

const (
	cardWidth   = 100 // width if W is not set
	cardFrame   = 3   // the line, the state and the actions line at the bottom
	dateWidth   = 11  // "02.01 15:04"
	actorWidth  = 16
	historyHead = "history"
)

// RenderCard draws the card: header, body with history, state, actions line. The body
// scrolls, the header and the bottom stay.
func RenderCard(c Card) string {
	return strings.Join(layoutCard(c, false).lines, "\n")
}

// CardScroll - the body offset of the frame for c. With fit the window first shifts to the Cursor
// line (only when it left the window); without, the Scroll is only clamped to the body. The model
// stores the result in Card.Scroll, so the stored offset always equals the frame on the screen.
func CardScroll(c Card, fit bool) int {
	return layoutCard(c, fit).bodyOff
}

// cardLay - the card frame and the lines the mouse can hit. Drawing and HitCard take it
// from one place, so body scroll does not diverge from a click.
type cardLay struct {
	lines          []string
	w              int
	askFrom, askTo int // agent ask lines [askFrom, askTo); -1 if there is no ask
	fieldsRow      int // the fields line; -1 if there is none
	actionsRow     int
	actions        []actionSpan
	bodyOff        int         // the body offset the frame is drawn at: the fit or the clamped scroll
	crits          []frameSpan // visible criterion lines
	hists          []frameSpan // visible history entry lines
}

// frameSpan - frame lines [from, to) of a criterion (index - the criterion number) or of a history
// entry (index - the entry number; boxLine -1). For a criterion boxLine - the line of its box with
// frame columns [boxFrom, boxTo); -1 - there is no box on the visible lines.
type frameSpan struct {
	from, to, index         int
	boxLine, boxFrom, boxTo int
}

// critDraw - the drawn criterion: its lines [from, to) in the returned body lines and the place of
// the box: the wrapped piece with `[x]` and frame columns [boxFrom, boxTo); piece -1 - no box.
type critDraw struct {
	from, to       int
	piece          int
	boxFrom, boxTo int
}

func layoutCard(c Card, fit bool) cardLay {
	w := c.W
	if w <= 0 {
		w = cardWidth
	}
	head, lay := cardHead(c, w)
	body, curStart, curEnd, crits, hists := cardBody(c, w)
	offset := 0
	if c.H > 0 {
		room := max(c.H-len(head)-cardFrame, 0)
		first := -1
		if len(crits) > 0 {
			first = crits[0].from
		} else if len(hists) > 0 {
			first = hists[0][0]
		}
		body, offset = scrollBody(body, c.Scroll, [3]int{curStart, curEnd, first}, room, fit)
	}
	lay.bodyOff = offset
	for i, cr := range crits {
		sh0, sh1 := cr.from-offset, cr.to-offset
		from, to := max(sh0, 0)+len(head), min(sh1, len(body))+len(head)
		if from >= to {
			continue
		}
		sp := frameSpan{from: from, to: to, index: i, boxLine: -1}
		if cr.piece >= 0 {
			if bl := sh0 + cr.piece + len(head); sh0+cr.piece >= 0 && bl < to {
				sp.boxLine, sp.boxFrom, sp.boxTo = bl, cr.boxFrom, cr.boxTo
			}
		}
		lay.crits = append(lay.crits, sp)
	}
	for i, h := range hists {
		from, to := max(h[0]-offset, 0)+len(head), min(h[1]-offset, len(body))+len(head)
		if from < to {
			lay.hists = append(lay.hists, frameSpan{from: from, to: to, index: i})
		}
	}
	lines := append(head, body...)
	text, spans := actionsText()
	lines = append(lines, ruleLine(w), statusLine(c.Status, "", c.IsErr, w), dimStyle.Render(truncate(text, w)))
	lay.lines, lay.w = lines, w
	lay.actionsRow, lay.actions = len(lines)-1, spans
	return lay
}

// cardHead builds the header and marks its lines in cardLay: the fields and the agent ask.
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

// cardBody builds the body lines, the bounds of the selected line (a criterion or a history entry; -1
// if there is none), the lines of each criterion in the body and the lines of each history entry.
func cardBody(c Card, w int) (lines []string, curStart, curEnd int, crits []critDraw, hists [][2]int) {
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
		for k, cr := range rel {
			cr.from, cr.to = cr.from+base, cr.to+base
			crits = append(crits, cr)
			if k == c.Cursor {
				curStart, curEnd = cr.from, cr.to
			}
		}
	}
	section(historyHead, "")
	base := len(lines)
	hl, from, to, rel := historyLines(c, w)
	lines = append(lines, hl...)
	if from >= 0 {
		curStart, curEnd = base+from, base+to
	}
	for i := range rel {
		rel[i][0], rel[i][1] = rel[i][0]+base, rel[i][1]+base
	}
	hists = rel
	return lines, curStart, curEnd, crits, hists
}

// historyLines draws the history entries; rel - the lines of each entry inside the result, from, to -
// the lines of the selected entry (-1 if there is none).
func historyLines(c Card, w int) (lines []string, from, to int, rel [][2]int) {
	from, to = -1, -1
	if len(c.History) == 0 {
		lines = append(lines, dimStyle.Render("  no history"))
	}
	for i, e := range c.History {
		start := len(lines)
		selected := i == c.Cursor-len(c.CritAt)
		lines = append(lines, entryLines(e, selected, w)...)
		rel = append(rel, [2]int{start, len(lines)})
		if selected {
			from, to = start, len(lines)
		}
	}
	return lines, from, to, rel
}

// indentLines prints the section text with an indent; dimRest dims all text lines after the first.
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

// detailLines draws the Details section; the selected criterion (Cursor) is highlighted. rel - the
// lines of each criterion inside the returned lines, with the place of its box.
func detailLines(c Card, w int) (lines []string, rel []critDraw) {
	critOf := map[int]int{}
	for k, at := range c.CritAt {
		critOf[at] = k
	}
	for i, src := range strings.Split(c.Details, "\n") {
		k, isCrit := critOf[i]
		start := len(lines)
		pieces := wrap(src, w-2)
		for _, l := range pieces {
			l = "  " + l
			if isCrit && k == c.Cursor {
				l = selStyle.Render(fit(l, w))
			}
			lines = append(lines, l)
		}
		if isCrit {
			cd := critDraw{from: start, to: len(lines), piece: -1}
			if p, from, to := boxPlace(pieces); p >= 0 {
				cd.piece, cd.boxFrom, cd.boxTo = p, boxIndent+from, boxIndent+to
			}
			rel = append(rel, cd)
		}
	}
	return lines, rel
}

// boxIndent - the frame columns before a wrapped Details piece: two spaces of the indent.
const boxIndent = 2

// boxPlace - the columns [from, to) of the `[ ]`/`[x]` box in the wrapped pieces (of the piece with
// the box); not found - piece -1.
func boxPlace(pieces []string) (piece, from, to int) {
	for j, p := range pieces {
		for i := 0; i+2 < len(p); i++ {
			if p[i] != '[' {
				continue
			}
			if (p[i+1] == ' ' || p[i+1] == 'x' || p[i+1] == 'X') && p[i+2] == ']' {
				return j, lipgloss.Width(p[:i]), lipgloss.Width(p[:i+3])
			}
		}
	}
	return -1, 0, 0
}

// entryLines draws a history entry; a comment - a heading and the text under it, others - one line with wrapping.
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

// scrollBody keeps room body lines from offset scroll. With fit the offset shifts only when the
// selected line (cur = start, end, start of the first selectable line) left the window: below the
// bottom edge or above the top one. On the first selectable line the preamble (abstract, summary)
// shows from the top while it fits.
func scrollBody(body []string, scroll int, cur [3]int, room int, fit bool) ([]string, int) {
	curStart, curEnd, first := cur[0], cur[1], cur[2]
	if room <= 0 {
		return nil, 0
	}
	if fit && curStart >= 0 {
		if curEnd > scroll+room {
			scroll = curEnd - room
		}
		if curStart < scroll {
			scroll = curStart
		}
		if curStart == first && curEnd <= room {
			scroll = 0
		}
	}
	scroll = min(max(scroll, 0), max(len(body)-room, 0))
	return body[scroll:min(scroll+room, len(body))], scroll
}
