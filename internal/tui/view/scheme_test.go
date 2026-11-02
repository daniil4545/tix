package view_test

import (
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
func TestStateOf_Claim(t *testing.T) {
	if got := view.StateOf(view.Row{}); got != view.StateIdle {
		t.Errorf("no claim: state %v, want idle", got)
	}
	if got := view.StateOf(view.Row{Live: true}); got != view.StateWorking {
		t.Errorf("live claim: state %v, want working", got)
	}
}

// The live card and the idle card have different title colors.
func TestScheme_IdleTitleDiffers(t *testing.T) {
	if view.TitleSGR(view.StateWorking) == view.TitleSGR(view.StateIdle) {
		t.Errorf("live and idle titles share the color %q", view.TitleSGR(view.StateIdle))
	}
}

// spec: T-85 scenario "green title with a live claim"
func TestRenderBoard_Title_ColorByState(t *testing.T) {
	rows := map[view.CardState]view.Row{
		view.StateWorking: {Live: true},
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

// A grey card without a claim and with an expired one: title, meta, pulse.
func TestRenderBoard_IdleCard_AllLinesGrey(t *testing.T) {
	row := view.Row{Summary: "look at it", Meta: "1/3 28.10"}
	lines := cardLinesOf(t, row, false)
	grey := view.TitleSGR(view.StateIdle)
	for i, want := range []string{"T-7", "Task", "look at it", "1/3 28.10"} {
		ln := lines[i]
		if !strings.Contains(plain(ln), want) {
			t.Fatalf("line %d lacks %q: %q", i, want, plain(ln))
		}
		if !strings.Contains(ln, grey) {
			t.Errorf("line %d is not grey (%q): %q", i, grey, ln)
		}
	}
	if green := view.TitleSGR(view.StateWorking); strings.Contains(strings.Join(lines, "\n"), green) {
		t.Errorf("idle card has the live title color %q", green)
	}
}

// spec: T-85 project color: each project on the board gets its own palette slot (FNV-1a of the
// lower-cased name, the next free slot on a collision); the label is drawn in it, also on grey cards
func TestProjectColors_DistinctCaseFreeStable(t *testing.T) {
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
	// shop and api, tracker and galera share an FNV slot: they must still differ.
	names := []string{"shop", "api", "tracker", "Galera", "tarn", "personal"}
	got := view.ProjectColors(names)
	slots := map[int]string{}
	for _, n := range names {
		slot, ok := got[strings.ToLower(n)]
		if !ok {
			t.Fatalf("no slot for %q in %v", n, got)
		}
		if other, dup := slots[slot]; dup {
			t.Errorf("%q and %q share slot %d", n, other, slot)
		}
		slots[slot] = n
	}
	again := view.ProjectColors([]string{"personal", "TARN", "galera", "Tracker", "API", "shop", "shop"})
	for n, slot := range got {
		if again[n] != slot {
			t.Errorf("%q: slot %d, then %d for the same set in another order and case", n, slot, again[n])
		}
	}
	for _, row := range []view.Row{{Project: "Tarn", ProjectColor: 2}, {Project: "tarn", ProjectColor: 2, Live: true}} {
		first := cardLinesOf(t, row, false)[0]
		if !strings.Contains(first, view.ProjectPaletteSGR(2)+row.Project) {
			t.Errorf("project label without its slot color: %q", first)
		}
		if !strings.Contains(plain(first), "T-7 "+row.Project) {
			t.Errorf("project label text changed: %q", plain(first))
		}
	}
}

// spec: T-85 selected row unchanged: the whole line keeps the selection background, no project or title color
func TestRenderBoard_SelectedRow_KeepsSelectionStyle(t *testing.T) {
	for _, row := range []view.Row{{}, {Live: true}} {
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
