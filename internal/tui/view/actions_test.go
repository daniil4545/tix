package view_test

// The view of the human main path: board hint buttons,
// "+" in the column names line, the card actions line. Only the public view of RenderBoard and RenderCard;
// hit testing and grab state are checked by the tui harness. Written from the spec, without reading the code of zones F and G.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/tui/view"
)

const (
	boardHintFull = "arrows select  space grab  enter open  n new  a answer  e edit  c comment  o order  p project  ? help  q quit"
	cardRowFull   = "e edit  c comment  a answer  f field  g session  del delete  j k history  esc back"
)

func lastNonEmpty(lines []string) (int, string) {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i, strings.TrimSpace(lines[i])
		}
	}
	return -1, ""
}

// The last board line is the new hint, the old keys m M are gone from it.
func TestRenderBoard_Hint_IsActionHint(t *testing.T) {
	b := view.Board{Total: 1, W: 140, H: 24, Cols: []view.Col{oneCol(view.Row{ID: "T-1", Title: "Alpha", Meta: "m"})}}
	ls := frameLines(b)
	if len(ls) != 24 {
		t.Fatalf("lines in the frame %d, expected 24", len(ls))
	}
	if got := strings.TrimSpace(ls[len(ls)-1]); got != boardHintFull {
		t.Errorf("hint %q, expected %q", got, boardHintFull)
	}
	if strings.Contains(strings.Join(ls, "\n"), "m M move") {
		t.Error("the hint m M move stayed in the frame")
	}
}

// A narrow board cuts the hint by the width, it does not wrap.
func TestRenderBoard_Hint_NarrowWidthCutNotWrapped(t *testing.T) {
	for _, w := range []int{100, 60, 30} {
		b := view.Board{Total: 1, W: w, H: 12, Status: "status", Cols: []view.Col{oneCol(view.Row{ID: "T-1", Title: "Alpha"})}}
		out := view.RenderBoard(b)
		fits(t, fmt.Sprintf("board %dx12", w), out, w, 12)
		ls := frameLines(b)
		if last := ls[len(ls)-1]; strings.TrimSpace(last) == "" || !strings.HasPrefix(boardHintFull, strings.TrimSpace(last)) {
			t.Errorf("width %d: the last line %q is not the start of the hint", w, last)
		}
	}
}

// New task: "+" is the last (24th) character of the cell of every shown column in the names line.
func TestRenderBoard_PlusAtLastCellOfEachColumnName(t *testing.T) {
	cols := []view.Col{oneCol(), oneCol(), oneCol()}
	cols[0].Name, cols[1].Name, cols[2].Name = "idea", "todo", "doing"
	b := view.Board{Total: 0, W: 120, H: 24, Cols: cols}
	names := []rune(frameLines(b)[2])
	for i := range cols {
		x := i*(24+2) + 23
		if len(names) <= x || names[x] != '+' {
			t.Errorf("column %d: no '+' at x=%d in the names line %q", i, x, string(names))
		}
	}
	if strings.Count(string(names), "+") != len(cols) {
		t.Errorf("pluses %d, expected %d: %q", strings.Count(string(names), "+"), len(cols), string(names))
	}
}

// The actions line is the last card line with an empty and a non-empty status line,
// the status line is above it; the frame is not taller than the window.
func TestRenderCard_ActionRowLastStatusAbove(t *testing.T) {
	for _, status := range []string{"", "T-1: no question"} {
		for _, h := range []int{24, 10} {
			t.Run(fmt.Sprintf("status=%q/h=%d", status, h), func(t *testing.T) {
				c := view.Card{ID: "T-1", Title: "Alpha", Column: "doing", W: 120, H: h, Status: status,
					Summary: "s1\ns2\ns3\ns4\ns5\ns6\ns7\ns8\ns9\ns10\ns11\ns12"}
				out := view.RenderCard(c)
				fits(t, "card", out, 120, h)
				ls := strings.Split(plain(out), "\n")
				last, got := lastNonEmpty(ls)
				if got != cardRowFull {
					t.Fatalf("last line %q, expected %q", got, cardRowFull)
				}
				if prev := strings.TrimSpace(ls[last-1]); prev != status {
					t.Errorf("status line above the actions %q, expected %q", prev, status)
				}
			})
		}
	}
}

// The actions line is cut by the width, not wrapped; the keys m M are gone from it.
func TestRenderCard_ActionRowNarrowCutNotWrapped(t *testing.T) {
	for _, w := range []int{60, 30} {
		c := view.Card{ID: "T-1", Title: "Alpha", Column: "doing", W: w, H: 12, Status: "status"}
		out := view.RenderCard(c)
		fits(t, fmt.Sprintf("card %dx12", w), out, w, 12)
		ls := strings.Split(plain(out), "\n")
		if _, last := lastNonEmpty(ls); last == "" || !strings.HasPrefix(cardRowFull, last) {
			t.Errorf("width %d: the last line %q is not the start of the actions line", w, last)
		}
		if strings.Contains(plain(out), "m M move") {
			t.Errorf("width %d: the hint m M move stayed", w)
		}
	}
}

// The card ask line is still "* needs you: <ask>" and stays above the body.
func TestRenderCard_AskLineStaysAboveBody(t *testing.T) {
	c := view.Card{ID: "T-1", Title: "Alpha", Column: "doing", W: 120, H: 24, Ask: "pick A or B", Summary: "sum"}
	ls := strings.Split(plain(view.RenderCard(c)), "\n")
	ask, body := -1, -1
	for i, ln := range ls {
		switch {
		case strings.Contains(ln, "* needs you: pick A or B"):
			ask = i
		case strings.Contains(ln, "summary"):
			body = i
		}
	}
	if ask < 0 || body < 0 || ask > body {
		t.Errorf("ask line at %d, body at %d:\n%s", ask, body, strings.Join(ls, "\n"))
	}
}
