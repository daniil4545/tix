package cli_test

import (
	"strings"
	"testing"
)

// spec: T-82 scenario 5
func TestCLI_New_ToCheckedColumn_RefusedNothingWritten(t *testing.T) {
	defer guard(t)
	for _, to := range []string{"todo", "ready", "blocked", "waiting", "done", "closed"} {
		t.Run(to, func(t *testing.T) {
			b := newBox(t)
			b.ok("", "new", "Seed")
			before := b.snapshot()
			msg := b.rule("new", "Fresh", "--to", to)
			ensure(t, strings.Contains(msg, "a new task has no"), "refusal %q does not say what the new task lacks", msg)
			ensure(t, strings.Contains(msg, "tix move <id>"), "refusal %q does not name the path", msg)
			ensure(t, b.snapshot() == before, "the refused new changed the store")
		})
	}
}

// spec: T-82 scenario 5
func TestCLI_New_ColumnsWithoutEntryCheck_StillCreate(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	for _, args := range [][]string{{"new", "A"}, {"new", "B", "--to", "idea"}, {"new", "C", "--to", "doing"}, {"new", "D", "--to", "active"}} {
		r := b.tix("", args...)
		ensure(t, r.code == 0, "tix %v: code %d, stderr %q", args, r.code, r.err)
	}
}

// spec: T-82 scenario 5 (the path the message names works)
func TestCLI_New_RefusalPath_CreateEditMove(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	id := b.newTask("Fresh", "idea")
	r := b.ok("", "move", id, "todo")
	ensure(t, strings.Contains(r.out, "todo"), "move to ready after criteria: %q", r.out)
}
