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
	CardNone      CardHitKind = iota // empty space, history, body
	CardAsk                          // the agent ask line `* needs you:`
	CardFields                       // the fields line
	CardAction                       // an actions-line button; the key is in CardHit.Key
	CardCriterion                    // a criterion line; the zero-based number is in CardHit.Index
)

// CardHit - the HitCard result. Key - the button key (`e`, `c`, `esc`), empty outside CardAction.
type CardHit struct {
	Kind CardHitKind
	Key  string
	// Index - the zero-based criterion number for CardCriterion.
	Index int
}

// HitCard finds what lies under cell (x, y) of the RenderCard(c) frame: the same lines and scroll
// as in drawing. A button cut by the width is not clickable.
func HitCard(c Card, x, y int) CardHit {
	lay := layoutCard(c)
	if x < 0 || x >= lay.w || y < 0 || y >= len(lay.lines) {
		return CardHit{}
	}
	switch {
	case y == lay.actionsRow:
		for _, a := range lay.actions {
			if x >= a.from && x < a.to && a.to <= lay.w {
				return CardHit{Kind: CardAction, Key: a.key}
			}
		}
	case lay.critAt(y) >= 0:
		return CardHit{Kind: CardCriterion, Index: lay.critAt(y)}
	case y == lay.fieldsRow:
		return CardHit{Kind: CardFields}
	case y >= lay.askFrom && y < lay.askTo:
		return CardHit{Kind: CardAsk}
	}
	return CardHit{}
}

// critAt - the criterion number on frame line y or -1.
func (l cardLay) critAt(y int) int {
	for _, r := range l.crits {
		if y >= r.from && y < r.to {
			return r.index
		}
	}
	return -1
}
