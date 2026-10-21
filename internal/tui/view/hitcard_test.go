package view_test

import (
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/tui/view"
)

// cardRow ищет строку кадра с подстрокой: номер строки и колонка подстроки.
func cardRow(t *testing.T, c view.Card, sub string) (x, y int) {
	t.Helper()
	for y, ln := range strings.Split(plain(view.RenderCard(c)), "\n") {
		if i := strings.Index(ln, sub); i >= 0 {
			return i, y
		}
	}
	t.Fatalf("в кадре нет %q", sub)
	return 0, 0
}

func critCard() view.Card {
	c := card()
	c.Details = "intro\n- [ ] first\n- [x] second"
	c.CritAt = []int{1, 2}
	c.Ask = "which one?"
	return c
}

func TestHitCard_RowsAndButtons(t *testing.T) {
	c := critCard()
	cases := []struct {
		sub  string
		want view.CardHit
	}{
		{"* needs you:", view.CardHit{Kind: view.CardAsk}},
		{"- [ ] first", view.CardHit{Kind: view.CardCriterion, Index: 0}},
		{"- [x] second", view.CardHit{Kind: view.CardCriterion, Index: 1}},
		{"intro", view.CardHit{}},
		{"c comment", view.CardHit{Kind: view.CardAction, Key: "c"}},
		{"del delete", view.CardHit{Kind: view.CardAction, Key: "del"}},
		{"j k history", view.CardHit{}},
	}
	for _, tc := range cases {
		x, y := cardRow(t, c, tc.sub)
		if got := view.HitCard(c, x, y); got != tc.want {
			t.Errorf("HitCard на %q = %+v, ждали %+v", tc.sub, got, tc.want)
		}
	}
}

// Критерий, ушедший за прокрутку, не кликается; обрезанная кнопка тоже.
func TestHitCard_ScrolledOutAndTruncated_NotClickable(t *testing.T) {
	c := critCard()
	c.H = 12
	c.Cursor = 5 // запись истории в конце: тело прокручено
	x, y := cardRow(t, c, "c comment")
	if got := view.HitCard(c, x, y); got.Kind != view.CardAction {
		t.Fatalf("кнопка c comment: %+v", got)
	}
	c.W = 20 // строка действий обрезана: g session не влезает целиком
	last := strings.Count(view.RenderCard(c), "\n")
	clickable := 0
	for x := 0; x < c.W; x++ {
		if got := view.HitCard(c, x, last); got.Kind == view.CardAction {
			clickable++
		}
		if got := view.HitCard(c, x, last); got.Kind == view.CardAction && got.Key == "g" {
			t.Errorf("обрезанная кнопка g кликается в колонке %d", x)
		}
	}
	if clickable == 0 {
		t.Error("в обрезанной строке не кликается ни одна кнопка: проверка пуста")
	}
}
