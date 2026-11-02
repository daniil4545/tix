package cli_test

import (
	"strings"
	"testing"
)

// epicBox: an epic with the lines of two tasks and one plain criterion, and the two tasks.
func epicBox(t *testing.T) (b *box, epic, c1, c2 string) {
	b = newBox(t)
	epic = b.newTask("Epic", "open")
	c1 = b.newTask("One", "open")
	c2 = b.newTask("Two", "open")
	b.ok("", "set", epic, "tags=epic")
	b.edit(epic, "## Details\n- [ ] "+c1+" one\n- [ ] "+c2+"\n- [ ] acceptance\n")
	b.edit(c1, "## Details\n- [x] shipped\n")
	b.edit(c2, "## Details\n- [x] shipped\n")
	return b, epic, c1, c2
}

func TestCLI_Epic_LineFollowsStatus(t *testing.T) {
	defer guard(t)
	b, epic, c1, c2 := epicBox(t)
	n := b.commitCount()
	b.ok("", "done", c1)
	ensure(t, b.commitCount() == n+1, "the tick must be in the commit of done")
	body := b.ok("", "show", epic).out
	ensure(t, strings.Contains(body, "- [x] "+c1+" one") && strings.Contains(body, "- [ ] "+c2), "tick: %q", body)
	// a forced close ticks too; a move out of closed clears the mark
	b.ok("", "move", c1, "open")
	ensure(t, strings.Contains(b.ok("", "show", epic).out, "- [ ] "+c1+" one"), "reopened task keeps its mark")
	b.ok("", "done", c1)
	b.ok("", "move", c2, "in_progress")
	b.ok("", "done", c2)
	ensure(t, strings.Contains(b.ok("", "show", epic).out, "- [x] "+c2+"\n"), "bare line must tick")
}

func TestCLI_Epic_CheckAndDone(t *testing.T) {
	defer guard(t)
	b, epic, c1, _ := epicBox(t)
	ensure(t, strings.Contains(b.rule("check", epic, "1"), "follows the status of "+c1), "check refusal")
	b.ok("", "check", epic, "3")
	// an unchecked task line holds the epic even with --force
	b.rule("done", epic, "--force", "-m", "x")
	ensure(t, strings.Contains(b.ok("", "show", epic).out, "status: open"), "epic must stay open")
}

func TestCLI_Epic_ShowListCtx(t *testing.T) {
	defer guard(t)
	b, epic, c1, c2 := epicBox(t)
	out := b.ok("", "show", c1).out
	ensure(t, strings.HasPrefix(strings.SplitN(out, "\n", 2)[1], "Epic: "+epic+"\n"), "show: %q", out)
	ensure(t, !strings.Contains(b.ok("", "show", epic).out, "Epic: T"), "an epic has no Epic line")
	ensure(t, strings.Contains(b.ok("", "list").out, epic+" [epic] Epic"), "list")
	ensure(t, strings.Contains(b.ok("", "ctx", "--project", "personal").out, epic+" [epic] Epic"), "ctx")
	ensure(t, strings.Contains(b.ok("", "list").out, c2+" Two"), "plain task has no mark")
	// an Epic: line in a task body is ignored
	b.edit(c2, "Epic: "+epic+"\n## Details\n- [x] shipped\n")
	b.ok("", "done", c2)
	ensure(t, strings.Contains(b.ok("", "show", epic).out, "- [x] "+c2+"\n"), "line ticks by the epic list")
}

func TestCLI_Epic_ForceBoundaryAndReady(t *testing.T) {
	defer guard(t)
	b, epic, c1, c2 := epicBox(t)
	b.edit(epic, "## Details\n- [ ] "+c1+" one\n- [ ] "+c2+"\n- [ ] T-20 other\n- [ ] "+c1+"suffix\n")
	b.edit(c1, "## Details\n- [ ] open\n")
	b.ok("", "done", c1, "--force", "-m", "x")
	body := b.ok("", "show", epic).out
	ensure(t, strings.Contains(body, "- [x] "+c1+" one") && strings.Contains(body, "- [ ] T-20 other") &&
		strings.Contains(body, "- [ ] "+c1+"suffix"), "forced close or id boundary: %q", body)
	b.ok("", "move", c1, "open")
	b.edit(epic, "## Details\n- [ ] "+c1+" one\n- [ ] "+c2+"\n- [ ] acceptance\n")
	b.ok("", "check", c1, "1")
	b.ok("", "done", c1)
	r := b.ok("", "done", c2)
	ensure(t, strings.Contains(r.out, "epic "+epic+": all tasks closed, ready for acceptance\n"), "ready signal: %q", r.out)
}

func TestCLI_Epic_DoneTakesTruthFromTasks(t *testing.T) {
	defer guard(t)
	b, epic, c1, c2 := epicBox(t)
	b.ok("", "done", c1)
	b.edit(epic, "## Details\n- [ ] "+c1+"\n- [ ] "+c2+"\n- [ ] T-99\n")
	msg := b.rule("done", epic, "--force", "-m", "x")
	ensure(t, strings.Contains(msg, c2+" (not closed)") && strings.Contains(msg, "T-99 (not found)"), "refusal text: %q", msg)
	b.ok("", "done", c2)
	b.edit(epic, "## Details\n- [ ] "+c1+"\n- [ ] "+c2+"\n")
	b.ok("", "done", epic, "--force", "-m", "x")
	body := b.ok("", "show", epic).out
	ensure(t, strings.Contains(body, "- [x] "+c1+"\n") && strings.Contains(body, "- [x] "+c2+"\n"), "lines of closed tasks: %q", body)
}
