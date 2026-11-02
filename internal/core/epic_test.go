package core_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/core"
)

// The epic is a rank neighbour renumbered in the same move: its line still follows the task status.
func TestMove_EpicLineKeptWhenEpicRenumbered(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	anchor := mkIn(t, s, "Anchor", "open")
	child := mkCrit(t, s, "Child", "in_progress")
	epic := mkIn(t, s, "Epic", "open")
	if _, err := s.Set(epic, []core.Field{{Name: "tags", Value: "epic"}}, "tester"); err != nil {
		t.Fatal(err)
	}
	_, rev, _ := s.Get(epic)
	if _, err := s.Edit(epic, rev, "## Details\n- [ ] "+child+"\n", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(child, "closed", core.MoveOpts{Force: true, Message: "x"}, "tester"); err != nil {
		t.Fatal(err)
	}
	ensure(t, strings.Contains(e.text(epic), "- [x] "+child), "close must tick the line")
	// no room between the anchor and the epic: reopening after the anchor renumbers the epic
	rank := regexp.MustCompile(`(?m)^rank: .*$`)
	for id, r := range map[string]string{anchor: "rank: 1", epic: "rank: 1.0000001"} {
		if err := os.WriteFile(e.file(id), rank.ReplaceAll([]byte(e.text(id)), []byte(r)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Move(child, "open", core.MoveOpts{Place: core.Place{After: anchor}}, "tester"); err != nil {
		t.Fatal(err)
	}
	got := e.text(epic)
	ensure(t, strings.Contains(got, "rank: 3"), "the epic must be renumbered: %q", got)
	ensure(t, strings.Contains(got, "- [ ] "+child), "reopen must clear the line: %q", got)
}
