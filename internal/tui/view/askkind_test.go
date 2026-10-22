package view_test

import (
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/tui/view"
)

const (
	redFg  = "31"
	blueFg = "34"
	greyFg = "38;2;105;117;111" // #69756F, the muted color
	yellow = "38;2;233;207;106"
)

// starParams returns the color parameters of the escape sequence right before the first "*".
func starParams(t *testing.T, line string) string {
	t.Helper()
	i := strings.Index(line, "*")
	if i < 0 {
		t.Fatalf("no star in %q", plain(line))
	}
	j := strings.LastIndex(line[:i], "\x1b[")
	if j < 0 {
		t.Fatalf("star without color in %q", line)
	}
	seq := line[j+len("\x1b[") : i]
	return strings.TrimSuffix(seq, "m")
}

func hasParam(params, want string) bool {
	return params == want || strings.HasPrefix(params, want+";") || strings.HasSuffix(params, ";"+want) ||
		strings.Contains(params, ";"+want+";")
}

// spec: T-44 scenario 3
func TestRenderBoard_AskKind_StarColor(t *testing.T) {
	cases := []struct {
		name string
		row  view.Row
		want string
	}{
		{"decide by default", view.Row{Ask: "which db?"}, yellow},
		{"decide prefix", view.Row{Ask: "decide: which db?"}, yellow},
		{"review", view.Row{Ask: "review: look at it"}, blueFg},
		{"access", view.Row{Ask: "access: need a key"}, redFg},
		{"decide ended", view.Row{Ask: "decide: x", AskEnded: true}, greyFg},
		{"review ended", view.Row{Ask: "review: x", AskEnded: true}, greyFg},
		{"access ended", view.Row{Ask: "access: x", AskEnded: true}, greyFg},
		{"unknown prefix is decide", view.Row{Ask: "foo: x"}, yellow},
	}
	for _, selected := range []bool{false, true} {
		for _, c := range cases {
			name := c.name
			if selected {
				name += " selected"
			}
			t.Run(name, func(t *testing.T) {
				c.row.ID, c.row.Title = "T-7", "Task"
				other := view.Row{ID: "T-8", Title: "Other"}
				rows := []view.Row{c.row, other}
				if !selected {
					rows = []view.Row{other, c.row}
				}
				b := view.Board{Total: 2, W: 120, H: 40, Col: 0, Row: 0, Cols: []view.Col{oneCol(rows...)}}
				line := lineWith(t, view.RenderBoard(b), "T-7")
				got := starParams(t, line)
				if !hasParam(got, c.want) {
					t.Errorf("star color %q, want %q", got, c.want)
				}
				if selected && !strings.Contains(got, selBg) {
					t.Errorf("selected star lost the highlight background %s: %q", selBg, got)
				}
			})
		}
	}
}

// spec: T-44 scenario 3 (pulse line has no prefix, muted when ended)
func TestRenderBoard_AskKind_PulseLineWithoutPrefix(t *testing.T) {
	for _, ask := range []string{"review: look at it", "access: look at it", "decide: look at it", "look at it"} {
		t.Run(ask, func(t *testing.T) {
			b := view.Board{Total: 1, W: 120, H: 40, Cols: []view.Col{oneCol(view.Row{ID: "T-7", Title: "Task", Ask: ask})}}
			raw := plain(view.RenderBoard(b))
			if !strings.Contains(raw, "? look at it") {
				t.Errorf("no pulse line %q in:\n%s", "? look at it", raw)
			}
			for _, kind := range []string{"review:", "access:", "decide:"} {
				if strings.Contains(raw, kind) {
					t.Errorf("pulse line keeps the prefix %q:\n%s", kind, raw)
				}
			}
		})
	}
}

func TestRenderBoard_AskEnded_PulseLineMuted(t *testing.T) {
	b := view.Board{Total: 2, W: 120, H: 40, Row: 1, Cols: []view.Col{oneCol(
		view.Row{ID: "T-7", Title: "Task", Ask: "review: look at it", AskEnded: true},
		view.Row{ID: "T-8", Title: "Other"},
	)}}
	line := lineWith(t, view.RenderBoard(b), "? look at it")
	if !strings.Contains(line, greyFg) || strings.Contains(line, yellow) {
		t.Errorf("ended ask pulse line is not muted: %q", line)
	}
}
