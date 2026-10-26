package view

import "strings"

// actionBtn - a pair of the card actions line. Key is empty for a hint without a button: a pair of two
// keys (`m M`, `j k`) has no single action under a click.
type actionBtn struct{ text, key string }

var cardActions = []actionBtn{
	{"e edit", "e"}, {"c comment", "c"}, {"a answer", "a"}, {"f field", "f"}, {"g session", "g"},
	{"del delete", "del"}, {"j k history", ""}, {"esc back", "esc"},
}

// actionSpan - the place of a button in the actions line: cells [from, to).
type actionSpan struct {
	from, to int
	key      string
}

// actionsText joins the actions line and computes the places of its buttons.
func actionsText() (string, []actionSpan) {
	var b strings.Builder
	var spans []actionSpan
	for i, a := range cardActions {
		if i > 0 {
			b.WriteString("  ")
		}
		from := b.Len()
		b.WriteString(a.text)
		if a.key != "" {
			spans = append(spans, actionSpan{from: from, to: b.Len(), key: a.key})
		}
	}
	return b.String(), spans
}

// CardHitKind - what is under a cell of the card frame.
type CardHitKind int

// Card hit kinds.
const (
	CardNone      CardHitKind = iota // empty space, body headings
	CardHistory                      // a history entry line; the zero-based entry number is in CardHit.Index
	CardAsk                          // the agent ask line `* needs you:`
	CardFields                       // the fields line
	CardAction                       // an actions-line button; the key is in CardHit.Key
	CardCriterion                    // a criterion line (the text); the zero-based number is in CardHit.Index
	CardBox                          // the `[ ]`/`[x]` box of a criterion; the number is in CardHit.Index
)

// CardHit - the HitCard result. Key - the button key (`e`, `c`, `esc`), empty outside CardAction.
type CardHit struct {
	Kind CardHitKind
	Key  string
	// Index - the zero-based criterion or history-entry number.
	Index int
}

// HitCard finds what lies under cell (x, y) of the RenderCard(c) frame: the same lines and scroll
// as in drawing. A button cut by the width is not clickable.
func HitCard(c Card, x, y int) CardHit {
	lay := layoutCard(c)
	if x < 0 || x >= lay.w || y < 0 || y >= len(lay.lines) {
		return CardHit{}
	}
	if y == lay.actionsRow {
		for _, a := range lay.actions {
			if x >= a.from && x < a.to && a.to <= lay.w {
				return CardHit{Kind: CardAction, Key: a.key}
			}
		}
		return CardHit{}
	}
	if hit, ok := lay.histHit(y); ok {
		return hit
	}
	if hit, ok := lay.critHit(y, x); ok {
		return hit
	}
	switch {
	case y == lay.fieldsRow:
		return CardHit{Kind: CardFields}
	case y >= lay.askFrom && y < lay.askTo:
		return CardHit{Kind: CardAsk}
	}
	return CardHit{}
}

// histHit - what lies on a history line of the frame.
func (l cardLay) histHit(y int) (CardHit, bool) {
	for _, r := range l.hists {
		if y >= r.from && y < r.to {
			return CardHit{Kind: CardHistory, Index: r.index}, true
		}
	}
	return CardHit{}, false
}

// critHit - what lies on a criterion line of the frame: the box toggles, the rest of the line selects.
func (l cardLay) critHit(y, x int) (CardHit, bool) {
	for _, r := range l.crits {
		if y < r.from || y >= r.to {
			continue
		}
		if y == r.boxLine && x >= r.boxFrom && x < r.boxTo {
			return CardHit{Kind: CardBox, Index: r.index}, true
		}
		return CardHit{Kind: CardCriterion, Index: r.index}, true
	}
	return CardHit{}, false
}
