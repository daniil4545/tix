package view

import "strings"

// actionBtn - пара строки действий карточки. Key пуст у подсказки без кнопки: у пары двух
// клавиш (`m M`, `j k`) нет одного действия под клик.
type actionBtn struct{ text, key string }

var cardActions = []actionBtn{
	{"e edit", "e"}, {"c comment", "c"}, {"a answer", "a"}, {"f field", "f"}, {"g session", "g"},
	{"del delete", "del"}, {"j k history", ""}, {"esc back", "esc"},
}

// actionSpan - место кнопки в строке действий: клетки [from, to).
type actionSpan struct {
	from, to int
	key      string
}

// actionsText склеивает строку действий и считает места её кнопок.
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

// CardHitKind - что под клеткой кадра карточки.
type CardHitKind int

// Виды попадания в карточку.
const (
	CardNone      CardHitKind = iota // пустое место, история, тело
	CardAsk                          // строка вопроса агента `* needs you:`
	CardFields                       // строка полей
	CardAction                       // кнопка строки действий; клавиша в CardHit.Key
	CardCriterion                    // строка критерия; номер с нуля в CardHit.Index
)

// CardHit - результат HitCard. Key - клавиша кнопки (`e`, `c`, `esc`), пусто вне CardAction.
type CardHit struct {
	Kind CardHitKind
	Key  string
	// Index - номер критерия с нуля для CardCriterion.
	Index int
}

// HitCard находит, что лежит под клеткой (x, y) кадра RenderCard(c): те же строки и прокрутка,
// что у рисования. Обрезанная по ширине кнопка не кликается.
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

// critAt - номер критерия на строке y кадра или -1.
func (l cardLay) critAt(y int) int {
	for _, r := range l.crits {
		if y >= r.from && y < r.to {
			return r.index
		}
	}
	return -1
}
