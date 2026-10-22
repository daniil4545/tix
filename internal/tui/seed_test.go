package tui

import (
	"path/filepath"
	"testing"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
)

// SeedTask creates a task in column of the store at root, with no entry check in the way: a column
// with a check (ready, waiting, closed) gets the task through the first inbox or active column,
// with three criteria added and then moved. The store config must have such a column.
func SeedTask(t *testing.T, s *core.Store, root, title, project, column string) string {
	t.Helper()
	cfg, err := config.Load(filepath.Join(root, ".tix", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	role, into := cfg.RoleOf(column), column
	checked := role == "ready" || role == "waiting" || role == "closed"
	if checked {
		into = ""
		for _, c := range cfg.Columns {
			if c.Role == "inbox" || c.Role == "active" {
				into = c.Name
				break
			}
		}
		if into == "" {
			t.Fatalf("SeedTask(%q): config has no inbox or active column to start from", column)
		}
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
	if role != "ready" {
		o.Message, o.Force = "seed", true
	}
	if _, err = s.Move(r.ID, column, o, "seed"); err != nil {
		t.Fatalf("Move %s to %s: %v", r.ID, column, err)
	}
	return r.ID
}
