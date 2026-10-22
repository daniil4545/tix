package view_test

import (
	"hash/fnv"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/tui/view"
)

// boardWith puts the row under test first (selected) or second; id is T-7.
func boardWith(selected bool, row view.Row) (view.Board, string) {
	row.ID, row.Title = "T-7", "Task"
	other := view.Row{ID: "T-8", Title: "Other"}
	rows := []view.Row{other, row}
	if selected {
		rows = []view.Row{row, other}
	}
	return view.Board{Total: 2, W: 120, H: 40, Cols: []view.Col{oneCol(rows...)}}, "T-7"
}

// cardLinesOf returns the raw lines of the card T-7 (the five lines from its first line).
func cardLinesOf(t *testing.T, row view.Row, selected bool) []string {
	t.Helper()
	b, id := boardWith(selected, row)
	lines := strings.Split(view.RenderBoard(b), "\n")
	for i, ln := range lines {
		if strings.Contains(plain(ln), id) {
			return lines[i : i+4]
		}
	}
	t.Fatalf("no card %s in:\n%s", id, plain(view.RenderBoard(b)))
	return nil
}

// spec: T-85 states
func TestStateOf_ClaimAndAsk(t *testing.T) {
	cases := []struct {
		name string
		row  view.Row
		want view.CardState
	}{
		{"no claim, no ask", view.Row{}, view.StateIdle},
		{"no claim, ask", view.Row{Ask: "which db?"}, view.StateIdle},
		{"live claim, no ask", view.Row{Live: true}, view.StateWorking},
		{"live claim, blank ask", view.Row{Live: true, Ask: "  "}, view.StateWorking},
		{"live claim and ask", view.Row{Live: true, Ask: "review: x"}, view.StateWaiting},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := view.StateOf(c.row); got != c.want {
				t.Errorf("state %v, want %v", got, c.want)
			}
		})
	}
}

// spec: T-85 scheme shape: a sign only when you are needed; both live states have the same title color,
// the idle card has another one.
func TestScheme_SignOnlyWhenWaiting_IdleTitleDiffers(t *testing.T) {
	if view.SignOf(view.StateWorking) != "" || view.SignOf(view.StateIdle) != "" || view.SignOf(view.StateWaiting) != "*" {
		t.Errorf("signs: working %q, idle %q, waiting %q", view.SignOf(view.StateWorking), view.SignOf(view.StateIdle), view.SignOf(view.StateWaiting))
	}
	if !view.KindStarOf(view.StateWaiting) || view.KindStarOf(view.StateIdle) {
		t.Errorf("only a waiting card takes the ask-kind color")
	}
	if view.TitleSGR(view.StateWorking) != view.TitleSGR(view.StateWaiting) {
		t.Errorf("live states differ in title color: %q, %q", view.TitleSGR(view.StateWorking), view.TitleSGR(view.StateWaiting))
	}
	if view.TitleSGR(view.StateWorking) == view.TitleSGR(view.StateIdle) {
		t.Errorf("live and idle titles share the color %q", view.TitleSGR(view.StateIdle))
	}
}

// spec: T-85 scenario "green title with a live claim"
func TestRenderBoard_Title_ColorByState(t *testing.T) {
	rows := map[view.CardState]view.Row{
		view.StateWorking: {Live: true},
		view.StateWaiting: {Live: true, Ask: "which db?"},
		view.StateIdle:    {},
	}
	for state, row := range rows {
		t.Run(view.TitleSGR(state), func(t *testing.T) {
			title := cardLinesOf(t, row, false)[1]
			if !strings.Contains(title, view.TitleSGR(state)) {
				t.Errorf("title line lacks the state color %q: %q", view.TitleSGR(state), title)
			}
			for other := range rows {
				if view.TitleSGR(other) != view.TitleSGR(state) && strings.Contains(title, view.TitleSGR(other)) {
					t.Errorf("title line has the color of another state %q: %q", view.TitleSGR(other), title)
				}
			}
		})
	}
}

// spec: T-85 scenario "star by kind only with a live claim"
func TestRenderBoard_Star_OnlyWithLiveClaimAndAsk(t *testing.T) {
	cases := []struct {
		name string
		row  view.Row
		star bool
		kind string
	}{
		{"working", view.Row{Live: true}, false, ""},
		{"idle", view.Row{}, false, ""},
		{"idle with ask", view.Row{Ask: "review: x"}, false, ""},
		{"idle with ask, expired claim", view.Row{Ask: "access: x", Live: false}, false, ""},
		{"waiting decide", view.Row{Live: true, Ask: "which db?"}, true, "decide"},
		{"waiting decide prefix", view.Row{Live: true, Ask: "decide: which db?"}, true, "decide"},
		{"waiting review", view.Row{Live: true, Ask: "review: x"}, true, "review"},
		{"waiting access", view.Row{Live: true, Ask: "access: x"}, true, "access"},
		{"waiting unknown prefix is decide", view.Row{Live: true, Ask: "foo: x"}, true, "decide"},
	}
	for _, selected := range []bool{false, true} {
		for _, c := range cases {
			name := c.name
			if selected {
				name += " selected"
			}
			t.Run(name, func(t *testing.T) {
				checkStar(t, cardLinesOf(t, c.row, selected)[0], c.star, c.kind, selected)
			})
		}
	}
}

func checkStar(t *testing.T, first string, star bool, kind string, selected bool) {
	t.Helper()
	if has := strings.Contains(first, "*"); has != star {
		t.Fatalf("star present=%v, want %v: %q", has, star, plain(first))
	}
	if !star {
		return
	}
	want := strings.TrimSuffix(strings.TrimPrefix(view.KindSGR(kind), "\x1b["), "m")
	if got := starParams(t, first); !hasParam(got, want) {
		t.Errorf("star color %q, want %q", got, want)
	}
	if selected && !strings.Contains(first, selBg) {
		t.Errorf("selected star lost the highlight background %s: %q", selBg, first)
	}
}

// spec: T-85 scenario "grey card without a claim and with an expired one": title, meta, pulse incl. ask text
func TestRenderBoard_IdleCard_AllLinesGrey(t *testing.T) {
	row := view.Row{Ask: "review: look at it", Meta: "1/3 28.10"}
	lines := cardLinesOf(t, row, false)
	grey := view.TitleSGR(view.StateIdle)
	for i, want := range []string{"T-7", "Task", "? look at it", "1/3 28.10"} {
		ln := lines[i]
		if !strings.Contains(plain(ln), want) {
			t.Fatalf("line %d lacks %q: %q", i, want, plain(ln))
		}
		if !strings.Contains(ln, grey) {
			t.Errorf("line %d is not grey (%q): %q", i, grey, ln)
		}
	}
	for _, kind := range []string{"decide", "review", "access"} {
		if sgr := view.KindSGR(kind); strings.Contains(strings.Join(lines, "\n"), sgr) {
			t.Errorf("idle card has the %s color %q", kind, sgr)
		}
	}
	if green := view.TitleSGR(view.StateWorking); strings.Contains(strings.Join(lines, "\n"), green) {
		t.Errorf("idle card has the live title color %q", green)
	}
}

// spec: T-85 live card keeps the kind color on the ask text and does not grey the ID line
func TestRenderBoard_WaitingCard_PulseHasKindColor(t *testing.T) {
	pulse := cardLinesOf(t, view.Row{Live: true, Ask: "access: need a key"}, false)[2]
	if !strings.Contains(pulse, view.KindSGR("access")) || !strings.Contains(plain(pulse), "? need a key") {
		t.Errorf("pulse line %q lacks the access color %q", pulse, view.KindSGR("access"))
	}
}

// spec: T-85 scenario "red overdue date": the due part only, on any card
func TestRenderBoard_Overdue_DuePartRed(t *testing.T) {
	cases := []struct {
		name string
		row  view.Row
		red  bool
	}{
		{"idle overdue", view.Row{Meta: "1/3 due 28.10", Overdue: "due 28.10"}, true},
		{"working overdue", view.Row{Live: true, Meta: "due 28.10 @claude", Overdue: "due 28.10"}, true},
		{"waiting overdue", view.Row{Live: true, Ask: "x", Meta: "due 28.10 @claude", Overdue: "due 28.10"}, true},
		{"not overdue", view.Row{Live: true, Meta: "1/3 due 28.10"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			meta := cardLinesOf(t, c.row, false)[3]
			if got := strings.Contains(meta, view.OverdueSGR()+"due 28.10"); got != c.red {
				t.Errorf("due part red=%v, want %v: %q", got, c.red, meta)
			}
			if c.red && strings.Contains(meta, view.OverdueSGR()+"1/3") {
				t.Errorf("more than the due part is red: %q", meta)
			}
			if !strings.Contains(plain(meta), c.row.Meta) {
				t.Errorf("meta text changed: %q, want %q", plain(meta), c.row.Meta)
			}
		})
	}
}

// spec: T-85 project color: FNV-1a of the lower-cased name picks the palette entry; also on grey cards
func TestRenderBoard_ProjectColor_StableAndCaseFree(t *testing.T) {
	if view.ProjectPaletteSize() != 6 {
		t.Fatalf("palette size %d, want 6", view.ProjectPaletteSize())
	}
	seen := map[string]bool{}
	for i := 0; i < view.ProjectPaletteSize(); i++ {
		seen[view.ProjectPaletteSGR(i)] = true
	}
	if len(seen) != 6 {
		t.Errorf("palette has repeated colors: %d distinct of 6", len(seen))
	}
	for _, name := range []string{"tarn", "galera", "tracker", "a", "beta-2"} {
		h := fnv.New32a()
		_, _ = h.Write([]byte(name))
		want := view.ProjectPaletteSGR(int(h.Sum32() % 6))
		for _, spelled := range []string{name, strings.ToUpper(name[:1]) + name[1:]} {
			if got := view.ProjectSGR(spelled); got != want {
				t.Errorf("project %q color %q, want %q (FNV-1a of %q)", spelled, got, want, name)
			}
		}
	}
	for _, row := range []view.Row{{Project: "Tarn"}, {Project: "tarn", Live: true}} {
		first := cardLinesOf(t, row, false)[0]
		if !strings.Contains(first, view.ProjectSGR("tarn")+row.Project) {
			t.Errorf("project label without its color: %q", first)
		}
		if !strings.Contains(plain(first), "T-7 "+row.Project) {
			t.Errorf("project label text changed: %q", plain(first))
		}
	}
}

// spec: T-85 selected row unchanged: the whole line keeps the selection background, no project or title color
func TestRenderBoard_SelectedRow_KeepsSelectionStyle(t *testing.T) {
	for _, row := range []view.Row{{}, {Live: true}, {Live: true, Ask: "x"}} {
		row.Project, row.Meta = "tarn", "m"
		lines := cardLinesOf(t, row, true)
		for i, ln := range lines {
			if !strings.Contains(ln, selBg) {
				t.Errorf("selected line %d lost the background: %q", i, ln)
			}
		}
		if green := view.TitleSGR(view.StateWorking); strings.Contains(lines[1], green) {
			t.Errorf("selected title recolored %q: %q", green, lines[1])
		}
	}
}
