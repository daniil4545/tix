package tui

import (
	"os"
	"slices"
	"testing"
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
	e.acStartIn("", 160, 30)
	cfg := e.root + "/.tix/config.toml"
	if err := os.WriteFile(cfg, []byte(sixStatuses+"\n[[status]]\nname = \"wontfix\"\nkind = \"closed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := e.model.loadCmd()()
	e.model.Update(msg)
	if got := colNames(e.model); got[len(got)-1] != "wontfix" {
		t.Fatalf("columns %v, want wontfix last", got)
	}
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
