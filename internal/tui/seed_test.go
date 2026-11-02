package tui

import (
	"testing"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
)

// SeedTask creates a task in status column of the store at root, with no entry check in the way: a status
// with a check (open, blocked, closed) gets the task through idea, with three criteria added and then moved.
func SeedTask(t *testing.T, s *core.Store, root, title, project, column string) string {
	t.Helper()
	checked := column == config.Open || column == config.Blocked || column == config.Closed
	into := column
	if checked {
		into = config.Idea
	}
	r, err := s.Create(title, project, into, "seed")
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	if !checked {
		return r.ID
	}
	if _, err = s.Edit(r.ID, r.Rev, "## Details\n- [ ] one\n- [ ] two\n- [ ] three\n", "seed"); err != nil {
		t.Fatalf("Edit %s: %v", r.ID, err)
	}
	o := core.MoveOpts{}
	if column != config.Open {
		o.Message, o.Force = "seed", true
	}
	if _, err = s.Move(r.ID, column, o, "seed"); err != nil {
		t.Fatalf("Move %s to %s: %v", r.ID, column, err)
	}
	return r.ID
}
