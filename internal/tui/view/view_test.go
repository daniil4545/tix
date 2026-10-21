package view_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/daniil4545/tix/internal/history"
	"github.com/daniil4545/tix/internal/tui/view"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata")

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

// plain strips color codes: tests check the text, not the palette.
func plain(s string) string { return ansiRe.ReplaceAllString(s, "") }

// fits checks the screen bounds: no more than h lines, each line no wider than w.
func fits(t *testing.T, name, out string, w, h int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > h {
		t.Errorf("%s: %d lines at height %d", name, len(lines), h)
	}
	for i, ln := range lines {
		if lw := lipgloss.Width(ln); lw > w {
			t.Errorf("%s: line %d of width %d at width %d: %q", name, i, lw, w, plain(ln))
			return
		}
	}
}

var colNames = []string{"idea", "todo", "doing", "blocked", "done"}

func bigBoard(rowsPerCol int) view.Board {
	b := view.Board{Total: rowsPerCol * len(colNames), Col: 1, Row: 0}
	n := 0
	for _, name := range colNames {
		c := view.Col{Name: name}
		for i := 0; i < rowsPerCol; i++ {
			n++
			c.Rows = append(c.Rows, view.Row{
				ID: fmt.Sprintf("T-%d", n), Project: "proj", Title: fmt.Sprintf("Заголовок %d 日本語 long long title", n),
				Summary: "первая строка сводки, довольно длинная", Meta: "1/3 до 28.10 @claude",
			})
		}
		b.Cols = append(b.Cols, c)
	}
	return b
}

func TestRenderBoard_AnySize_FitsScreen(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{200, 60}, {120, 40}, {80, 24}, {60, 12}, {40, 8}} {
		for _, rows := range []int{0, 3, 60} {
			b := bigBoard(rows)
			b.W, b.H = sz.w, sz.h
			b.Status = "status line that is rather long to check truncation of the bottom line as well"
			fits(t, fmt.Sprintf("board %dx%d, %d rows in a column", sz.w, sz.h, rows), view.RenderBoard(b), sz.w, sz.h)
		}
	}
}

func TestRenderBoard_WideScreen_ShowsColumnsAndRowParts(t *testing.T) {
	b := view.Board{Project: "alpha", Total: 2, W: 200, H: 40, Cols: []view.Col{
		{Name: "idea"},
		{Name: "todo", Rows: []view.Row{
			{ID: "T-7", Project: "alpha", Title: "Fix login", Summary: "see notes", Meta: "1/3 28.10"},
		}},
		{Name: "doing"}, {Name: "blocked"}, {Name: "done"},
	}}
	out := plain(view.RenderBoard(b))
	for _, want := range colNames {
		if !strings.Contains(out, want) {
			t.Errorf("board without column %q:\n%s", want, out)
		}
	}
	for _, want := range []string{"T-7", "alpha", "Fix login", "see notes", "1/3 28.10"} {
		if !strings.Contains(out, want) {
			t.Errorf("board without %q:\n%s", want, out)
		}
	}
}

func TestRenderBoard_StarOnlyOnRowThatNeedsYou(t *testing.T) {
	b := view.Board{W: 120, H: 30, Total: 2, Cols: []view.Col{{Name: "todo", Rows: []view.Row{
		{ID: "T-1", Project: "p", Title: "Quiet"},
		{ID: "T-2", Project: "p", Title: "Needs you", Star: true},
	}}}}
	for _, ln := range strings.Split(plain(view.RenderBoard(b)), "\n") {
		hasStar := strings.Contains(ln, "*")
		if strings.Contains(ln, "T-2") && !hasStar {
			t.Errorf("line T-2 without a star: %q", ln)
		}
		if strings.Contains(ln, "T-1") && hasStar {
			t.Errorf("line T-1 with a star: %q", ln)
		}
	}
}

func TestRenderBoard_SelectedColumnStaysVisibleWhenWindowIsNarrow(t *testing.T) {
	// Columns of 24 characters: two fit in 60 characters, the window follows the cursor.
	b := bigBoard(3)
	b.W, b.H = 60, 30
	for col, name := range colNames {
		b.Col, b.Row = col, 2
		out := plain(view.RenderBoard(b))
		if !strings.Contains(out, name) {
			t.Errorf("cursor in column %q: it is not on the screen:\n%s", name, out)
		}
		id := b.Cols[col].Rows[2].ID + " "
		if !strings.Contains(out, id) {
			t.Errorf("cursor on %s: the line is not on the screen:\n%s", id, out)
		}
	}
}

func TestRenderBoard_SelectedRowStaysVisibleInLongColumn(t *testing.T) {
	b := bigBoard(80)
	b.W, b.H = 120, 20
	b.Col, b.Row = 2, 70
	out := plain(view.RenderBoard(b))
	if id := b.Cols[2].Rows[70].ID + " "; !strings.Contains(out, id) {
		t.Errorf("cursor on %s in a long column: the line is not on the screen:\n%s", id, out)
	}
}

func TestRenderBoard_SelectionAndErrorLookDifferent(t *testing.T) {
	base := bigBoard(3)
	base.W, base.H = 120, 30
	a, b := base, base
	b.Row = 1
	if view.RenderBoard(a) == view.RenderBoard(b) {
		t.Error("selection of the first and second line is drawn the same")
	}
	ok, bad := base, base
	ok.Status, bad.Status = "write failed", "write failed"
	bad.IsErr = true
	if view.RenderBoard(ok) == view.RenderBoard(bad) {
		t.Error("an error in the status line looks like a normal status")
	}
	if !strings.Contains(plain(view.RenderBoard(bad)), "write failed") {
		t.Error("status line text not shown")
	}
}

func card() view.Card {
	at := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	return view.Card{
		ID: "T-12", Title: "Report on calls", Column: "doing", Fields: "alpha   due 28.10   @claude",
		Abstract: "abstract text", Summary: "summary text", Details: "details text", Checks: "1/3",
		History: []history.Entry{
			{At: at, Actor: "claude", Op: "new"},
			{At: at.Add(time.Hour), Actor: "claude", Op: "set", Text: "3 edits", Folded: []history.Entry{
				{Op: "set"}, {Op: "set"}, {Op: "set"}}},
			{At: at.Add(2 * time.Hour), Actor: "daniil", Op: "comment", Text: "use a dash for empty values"},
		},
		W: 100, H: 40,
	}
}

func TestRenderCard_ShowsEverySectionAndHistory(t *testing.T) {
	out := plain(view.RenderCard(card()))
	for _, want := range []string{"T-12", "Report on calls", "doing", "alpha", "abstract text", "summary text",
		"details text", "claude", "new", "3 edits", "daniil", "use a dash for empty values"} {
		if !strings.Contains(out, want) {
			t.Errorf("card without %q:\n%s", want, out)
		}
	}
}

func TestRenderCard_AskShownOnlyWhenSet(t *testing.T) {
	c := card()
	if out := plain(view.RenderCard(c)); strings.Contains(out, "needs you") {
		t.Errorf("empty ask shown:\n%s", out)
	}
	c.Ask = "pick A or B"
	if out := plain(view.RenderCard(c)); !strings.Contains(out, "* needs you: pick A or B") {
		t.Errorf("ask not shown as the line `* needs you: <ask>`:\n%s", out)
	}
}

func TestRenderCard_AnySize_FitsScreen(t *testing.T) {
	long := strings.Repeat("длинная строка без переносов 日本語 ", 12)
	for _, sz := range []struct{ w, h int }{{120, 50}, {80, 24}, {60, 16}} {
		for _, n := range []int{0, 5, 200} {
			c := card()
			c.W, c.H = sz.w, sz.h
			c.Title, c.Abstract, c.Summary, c.Details, c.Fields = long, long, long, long+"\n"+long, long
			c.Ask, c.Status = long, long
			c.History = nil
			for i := 0; i < n; i++ {
				c.History = append(c.History, history.Entry{Actor: "claude", Op: "comment", Text: long})
			}
			for _, cur := range []int{0, n / 2, max(n-1, 0)} {
				c.Cursor, c.Scroll = cur, cur/2
				fits(t, fmt.Sprintf("card %dx%d, history %d, cursor %d", sz.w, sz.h, n, cur), view.RenderCard(c), sz.w, sz.h)
			}
		}
	}
}

func TestRenderInput_PromptAndTailOfLongText(t *testing.T) {
	short := plain(view.RenderInput("waiting for:", "vendor", 60))
	if !strings.Contains(short, "waiting for:") || !strings.Contains(short, "vendor") {
		t.Errorf("input line without a prompt or text: %q", short)
	}
	long := strings.Repeat("0123456789", 20) + "END"
	out := view.RenderInput("close reason:", long, 40)
	fits(t, "input line", out, 40, 1)
	if p := plain(out); !strings.Contains(p, "END") || !strings.Contains(p, "close reason:") {
		t.Errorf("long text: the prompt and the input end are not visible: %q", p)
	}
}

// golden compares the frame text with a file in testdata; -update rewrites the file.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	got = plain(got)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil { //nolint:gosec // golden file in the repository
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // path built from the test name
	if err != nil {
		t.Fatalf("no golden file %s: run `go test ./internal/tui/view -update` and check the diff by eye", path)
	}
	if got != string(want) {
		t.Errorf("frame %s differs from the golden file.\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}

func TestGolden_BoardAndCard(t *testing.T) {
	b := view.Board{Project: "alpha", Total: 4, Col: 1, Row: 0, W: 130, H: 14, Status: "ok T-7 todo > doing", Cols: []view.Col{
		{Name: "idea"},
		{Name: "todo", Rows: []view.Row{
			{ID: "T-7", Project: "alpha", Title: "Fix login", Summary: "see notes", Meta: "1/3 28.10"},
			{ID: "T-9", Project: "alpha", Title: "Write docs", Star: true},
		}},
		{Name: "doing", Rows: []view.Row{{ID: "T-12", Project: "beta", Title: "Report on calls", Meta: "@claude"}}},
		{Name: "blocked", Rows: []view.Row{{ID: "T-15", Project: "beta", Title: "GPU access", Summary: "waiting for: invoice"}}},
		{Name: "done"},
	}}
	golden(t, "board", view.RenderBoard(b))
	golden(t, "card", view.RenderCard(card()))
}
