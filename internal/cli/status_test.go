package cli_test

import (
	"strings"
	"testing"
)

func TestStatusCommand_AddRenameRm_List(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "A")
	if out := strings.Fields(b.ok("", "status").out); strings.Join(out, " ") != "idea idea 1 open open 0 in_progress in_progress 0 blocked blocked 0 closed closed 0" {
		t.Errorf("status: %q", out)
	}
	if out := b.ok("", "status", "add", "review", "--kind", "in_progress").out; out != "ok status add review in_progress\n" {
		t.Errorf("add: %q", out)
	}
	b.ok("", "move", "T-1", "review")
	if out := b.ok("", "status").out; !strings.Contains(strings.Join(strings.Fields(out), " "), "review in_progress 1 blocked") {
		t.Errorf("status after add:\n%s", out)
	}
	r := b.tix("", "status", "rm", "review")
	if r.code != 5 || !strings.Contains(r.err, "has 1 tasks") {
		t.Errorf("rm with a task: code %d, %q", r.code, r.err)
	}
	if out := b.ok("", "status", "rename", "review", "qa").out; out != "ok status rename review qa\n" {
		t.Errorf("rename: %q", out)
	}
	if out := b.ok("", "list", "--status", "qa").out; !strings.Contains(out, "T-1") {
		t.Errorf("list --status qa: %q", out)
	}
	b.ok("", "move", "T-1", "idea")
	if out := b.ok("", "status", "rm", "qa").out; out != "ok status rm qa\n" {
		t.Errorf("rm: %q", out)
	}
	for _, args := range [][]string{{"status", "add", "x"}, {"status", "rename", "a"}, {"status", "bogus"}, {"status", "rm", "a", "--kind", "open"}} {
		if r := b.tix("", args...); r.code != 6 {
			t.Errorf("%v: code %d, %q", args, r.code, r.err)
		}
	}
	if r := b.tix("", "status", "add", "closed", "--kind", "open"); r.code != 5 {
		t.Errorf("kind word: code %d, %q", r.code, r.err)
	}
}
