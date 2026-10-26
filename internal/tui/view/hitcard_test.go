package view_test

import (
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/tui/view"
)

// cardRow looks for a frame line with a substring: the line number and the substring column.
func cardRow(t *testing.T, c view.Card, sub string) (x, y int) {
	t.Helper()
	for y, ln := range strings.Split(plain(view.RenderCard(c)), "\n") {
		if i := strings.Index(ln, sub); i >= 0 {
			return i, y
		}
	}
	t.Fatalf("%q is not in the frame", sub)
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
			t.Errorf("HitCard at %q = %+v, expected %+v", tc.sub, got, tc.want)
		}
	}
}

// The columns of the box are exactly the bracket pair `[ ]`/[x]`: the box toggles,
// the marker and the text only select.
func TestHitCard_CriterionBoxSeparateFromText(t *testing.T) {
	c := critCard()
	fx, fy := cardRow(t, c, "- [ ] first")  // the line and the marker
	bx, by := cardRow(t, c, "[ ] first")    // the box columns
	tx, _ := cardRow(t, c, "first")         // the text
	chx, chy := cardRow(t, c, "[x] second") // the box of the second criterion
	sx, sy := cardRow(t, c, "second")
	if by != fy || chy != sy {
		t.Fatalf("the box and the text are not on one frame line: %d %d and %d %d", fy, by, chy, sy)
	}
	cases := []struct {
		x, y int
		want view.CardHit
	}{
		{fx, fy, view.CardHit{Kind: view.CardCriterion, Index: 0}},
		{bx, by, view.CardHit{Kind: view.CardBox, Index: 0}},
		{bx + 2, by, view.CardHit{Kind: view.CardBox, Index: 0}},
		{tx, fy, view.CardHit{Kind: view.CardCriterion, Index: 0}},
		{chx, chy, view.CardHit{Kind: view.CardBox, Index: 1}},
		{sx, sy, view.CardHit{Kind: view.CardCriterion, Index: 1}},
	}
	for _, tc := range cases {
		if got := view.HitCard(c, tc.x, tc.y); got != tc.want {
			t.Errorf("HitCard at (%d,%d) = %+v, expected %+v", tc.x, tc.y, got, tc.want)
		}
	}
}

// A history entry is clickable on every line of it; the index is the entry number.
func TestHitCard_HistoryRowsReported(t *testing.T) {
	c := card()
	_, y := cardRow(t, c, "history")
	hist, hy := cardRow(t, c, "10:00") // the first entry line
	if hy < 0 || hy <= y {
		t.Fatalf("the first entry %d is not after the history heading %d", hy, y)
	}
	for i := 0; i < 4; i++ {
		if got := view.HitCard(c, hist, hy+i); got.Kind != view.CardHistory {
			t.Errorf("HitCard on history line %d = %+v, expected a history line", i, got)
		}
	}
	if got := view.HitCard(c, hist, y); got.Kind == view.CardHistory {
		t.Errorf("the section heading is clickable: %+v", got)
	}
}

// A criterion scrolled out is not clickable; a cut button too.
func TestHitCard_ScrolledOutAndTruncated_NotClickable(t *testing.T) {
	c := critCard()
	c.H = 12
	c.Cursor = 5 // the history entry at the end: the body is scrolled
	x, y := cardRow(t, c, "c comment")
	if got := view.HitCard(c, x, y); got.Kind != view.CardAction {
		t.Fatalf("button c comment: %+v", got)
	}
	c.W = 20 // the actions line is cut: g session does not fit whole
	last := strings.Count(view.RenderCard(c), "\n")
	clickable := 0
	for x := 0; x < c.W; x++ {
		if got := view.HitCard(c, x, last); got.Kind == view.CardAction {
			clickable++
		}
		if got := view.HitCard(c, x, last); got.Kind == view.CardAction && got.Key == "g" {
			t.Errorf("the cut button g is clickable at column %d", x)
		}
	}
	if clickable == 0 {
		t.Error("no button is clickable in the cut line: the check is empty")
	}
}

// A wrapped criterion whose first line is scrolled out has no box on the screen: a click on the
// columns where the box would be selects the criterion, never toggles it.
func TestHitCard_WrappedCriterionTopCut_ContinuationIsNotBox(t *testing.T) {
	c := card()
	c.W, c.H = 30, 12
	c.Details = "- [ ] first words of a long criterion that wraps over lines"
	c.CritAt = []int{0}
	c.Scroll = 8 // body line 7 is the criterion head, line 8 its continuation
	const firstBodyRow = 3
	got := view.HitCard(c, 4, firstBodyRow)
	if got.Kind != view.CardCriterion || got.Index != 0 {
		t.Errorf("click on the continuation line at the box columns = %+v, expected CardCriterion 0", got)
	}
}
