package cli_test

import (
	"strings"
	"testing"
)

func TestS17_Show_ParentAndChildren(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	p := b.newTask("Epic", "idea")
	c1 := b.newTaskIn(b.cwd, "One", "idea", "--parent", p)
	c2 := b.newTaskIn(b.cwd, "Two", "idea", "--parent", p)
	b.ok("", "move", c2, "closed", "-m", "r", "--force")
	out := b.ok("", "show", p).out
	for _, want := range []string{"\nChildren: 1/2\n", "  idea " + c1 + " One\n", "  closed " + c2 + " Two\n"} {
		ensure(t, strings.Contains(out, want), "show parent lacks %q:\n%s", want, out)
	}
	ensure(t, strings.Index(out, c1+" One") < strings.Index(out, c2+" Two"), "children out of order:\n%s", out)
	ensure(t, strings.Contains(b.ok("", "show", c1).out, "\nParent: "+p+"\n"), "show child")
	b.ok("", "set", c1, "parent=")
	ensure(t, !strings.Contains(b.ok("", "show", c1).out, "\nParent: "), "cleared parent still printed")
	ensure(t, strings.Contains(b.rule("new", "X", "--parent", "T-999"), "not found"), "new under a missing parent")
	ensure(t, strings.Contains(b.rule("set", p, "parent="+c1), "has children"), "set parent on a task with children")
}

func TestS28_Show_ManualParent(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	a, c := b.newTask("A", "idea"), b.newTask("C", "idea")
	b.rewrite(a, func(s string) string { return strings.Replace(s, "\nstatus:", "\nparent: T-999\nstatus:", 1) })
	b.rewrite(c, func(s string) string { return strings.Replace(s, "\nstatus:", "\nparent: "+c+"\nstatus:", 1) })
	ensure(t, strings.Contains(b.ok("", "show", a).out, "\nParent: T-999 (not found)\n"), "missing parent")
	ensure(t, strings.Contains(b.ok("", "show", c).out, "\nParent: "+c+"\n"), "self parent")
	// a parent that has a parent: shown as written, set refuses it
	f, d, e := b.newTask("F", "idea"), b.newTask("D", "idea"), b.newTask("E", "idea")
	b.ok("", "set", d, "parent="+f)
	b.rewrite(e, func(s string) string { return strings.Replace(s, "\nstatus:", "\nparent: "+d+"\nstatus:", 1) })
	ensure(t, strings.Contains(b.ok("", "show", e).out, "\nParent: "+d+"\n"), "parent that has a parent")
	b.ok("", "note", e, "x") // commits the hand edit, so the refusal below adds nothing
	ensure(t, strings.Contains(b.rule("set", e, "parent="+d), "already has a parent"), "set applies the refusal")
}

func TestS18_S19_S20_S21_Cli(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	p := b.newTask("Epic", "idea")
	c1 := b.newTaskIn(b.cwd, "One", "idea", "--parent", p)
	c2 := b.newTaskIn(b.cwd, "Two", "idea", "--parent", p)
	ensure(t, strings.Contains(b.rule("done", p, "--force"), c1+", "+c2), "S18 children not named")
	ensure(t, strings.Contains(b.rule("rm", p), "has children"), "S21")
	b.ok("", "done", c1, "--force", "-m", "r")
	ensure(t, strings.Contains(b.ok("", "done", c2, "--force", "-m", "r").out,
		"epic "+p+": all tasks closed, ready for acceptance"), "S19")
	b.ok("", "done", p, "--force", "-m", "r")
	r := b.ok("", "move", c1, "idea")
	ensure(t, strings.Contains(r.err, "warning: parent "+p+" is closed"), "S20: %q", r.err)
}

func TestList_ParentMark(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	p := b.newTask("Epic", "idea")
	c1 := b.newTaskIn(b.cwd, "One", "idea", "--parent", p)
	b.newTaskIn(b.cwd, "Two", "idea", "--parent", p)
	b.ok("", "done", c1, "--force", "-m", "r")
	out := b.ok("", "list").out
	ensure(t, strings.Contains(out, p+" [epic 1/2] Epic"), "list mark:\n%s", out)
	js := b.ok("", "list", "--json", "--all").out
	for _, want := range []string{`"children":2`, `"children_closed":1`, `"parent":"` + p + `"`} {
		ensure(t, strings.Contains(js, want), "list --json lacks %s:\n%s", want, js)
	}
	b.ok("", "move", p, "in_progress")
	ctx := b.ok("", "ctx", "--project", "personal").out
	ensure(t, strings.Contains(ctx, p+" [epic 1/2] Epic"), "ctx mark:\n%s", ctx)
	cj := b.ok("", "ctx", "--project", "personal", "--json").out
	ensure(t, strings.Contains(cj, `"epic":true`) && strings.Contains(cj, `"children_closed":1`), "ctx json:\n%s", cj)
}

// A parent whose only child is closed stays out of `list --like` (spec 3.3).
func TestList_LikeSkipsParentOfClosedChild(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	p := b.newTask("Card duplicates cleanup", "idea")
	c := b.newTaskIn(b.cwd, "Other thing", "idea", "--parent", p)
	b.ok("", "done", c, "--force", "-m", "r")
	out := b.ok("", "list", "--like", "card duplicates cleanup").out
	ensure(t, !strings.Contains(out, p+" "), "parent listed:\n%s", out)
}
