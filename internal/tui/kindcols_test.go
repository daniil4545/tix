package tui

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/core"
)

const sixStatuses = `format = 2

[projects]

[[status]]
name = "idea"
kind = "idea"

[[status]]
name = "open"
kind = "open"

[[status]]
name = "in_progress"
kind = "in_progress"

[[status]]
name = "review"
kind = "in_progress"

[[status]]
name = "blocked"
kind = "blocked"

[[status]]
name = "closed"
kind = "closed"
`

func colNames(m *Model) []string {
	var out []string
	for _, c := range m.board.Cols {
		out = append(out, c.Name)
	}
	return out
}

// A config.toml rewritten by another process shows up as a new column after the next board reload.
func TestBoard_ConfigEditedOutside_NewColumn(t *testing.T) {
	e := mvNewEnv(t, sixStatuses)
	e.create("Plain", "idea")
	e.acStartIn("", 240, 30)
	cfg := e.root + "/.tix/config.toml"
	if err := os.WriteFile(cfg, []byte(sixStatuses+"\n[[status]]\nname = \"wontfix\"\nkind = \"closed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.tm.Send(e.model.loadCmd()()) // through the program: the model belongs to its goroutine
	e.screenHas("wontfix")
}

// S27: six statuses of the config in config order, and `?` for a status that is not in it.
func TestS27_Board_SixStatusesAndUnknown(t *testing.T) {
	e := mvNewEnv(t, sixStatuses)
	id := e.create("Odd", "idea")
	e.create("Plain", "idea")
	setRawStatus(t, e, id, "idea", "later")
	e.acStartIn("", 160, 30)
	want := []string{"idea", "open", "in_progress", "review", "blocked", "closed", "?"}
	if got := colNames(e.model); !slices.Equal(got, want) {
		t.Fatalf("columns %v, want %v", got, want)
	}
	if !e.model.configured("review") || e.model.configured("later") {
		t.Error("configured: review must be known, later not")
	}
	if e.model.kind("review") != "in_progress" {
		t.Errorf("kind(review) = %q", e.model.kind("review"))
	}
}

const sevenKinds = sixStatuses + "\n[[status]]\nname = \"wontfix\"\nkind = \"closed\"\n\n[[status]]\nname = \"stuck\"\nkind = \"blocked\"\n"

// A move between two statuses of one kind asks nothing; from the column `?` it goes by the kind of the target.
func TestMoveTo_SameKindNoAsk_FromUnknownAllowed(t *testing.T) {
	e := mvNewEnv(t, sevenKinds)
	a := e.create("Closed one", "idea")
	b := e.create("Odd", "idea")
	e.acStartIn("", 160, 30)
	e.model.Update(e.model.loadCmd()())
	if _, err := e.store.Move(a, "closed", core.MoveOpts{Force: true, Message: "x"}, "t"); err != nil {
		t.Fatal(err)
	}
	e.model.Update(e.model.loadCmd()())
	if cmd := e.model.moveTo(e.model.byID(a), "wontfix", core.MoveOpts{}); cmd == nil || e.model.input != nil {
		t.Errorf("closed to wontfix: want a move without a question, input %v", e.model.input)
	}
	setRawStatus(t, e, b, "idea", "later")
	e.model.Update(e.model.loadCmd()())
	e.model.moveTo(e.model.byID(b), "stuck", core.MoveOpts{})
	if e.model.input == nil || e.model.isErr {
		t.Errorf("from ? to blocked kind: want the waiting question, status %q", e.model.status)
	}
}

// A broken config.toml shows on the status line and the board keeps the old columns.
func TestBoard_BrokenConfig_StatusLineErrorOldColumns(t *testing.T) {
	e := mvNewEnv(t, sixStatuses)
	e.create("Plain", "idea")
	e.acStartIn("", 160, 30)
	before := colNames(e.model)
	if err := os.WriteFile(e.root+"/.tix/config.toml", []byte("[[status]\nbroken"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.model.Update(e.model.loadCmd()())
	if !e.model.isErr || !strings.Contains(e.model.status, "config not reloaded") {
		t.Errorf("status %q err %v", e.model.status, e.model.isErr)
	}
	if got := colNames(e.model); !slices.Equal(got, before) {
		t.Errorf("columns %v, want %v", got, before)
	}
}
