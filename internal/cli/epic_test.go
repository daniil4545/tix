package cli_test

import (
	"os"
	"strings"
	"testing"
)

func TestCLI_DoneEpic_CounterAndAcceptance(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	epic := b.newTask("Epic", "todo")
	child := b.newTask("Child", "todo")
	b.edit(epic, "## Details\n- [ ] "+child+" child\n- [ ] acceptance\n")
	b.edit(child, "\n\nEpic: "+epic+"\n\n## Details\n- [x] shipped\n")
	n := b.commitCount()
	r := b.ok("", "done", child)
	ensure(t, r.err == "" && !strings.Contains(r.out, "ready for acceptance"), "partial epic: %+v", r)
	ensure(t, b.commitCount() == n+2, "done and epic check need separate commits")
	ensure(t, strings.Contains(b.git("show", "HEAD~1:"+strings.TrimPrefix(b.taskFile(child), b.home+"/")), "status: done"), "child closes first")
	ensure(t, strings.Contains(b.git("show", "HEAD~1:"+strings.TrimPrefix(b.taskFile(epic), b.home+"/")), "- [ ] "+child), "epic checked only after done")
	out := b.ok("", "ctx", "--project", "personal").out
	ensure(t, strings.Contains(out, "Epic | 1/2"), "ctx counter: %q", out)
	b.noCommit("done", child)
	b.edit(epic, "## Details\n- [ ] "+child+"\n")
	r = b.ok("", "done", child)
	ensure(t, strings.Contains(r.out, "epic "+epic+" ready for acceptance\n"), "ready message: %q", r.out)
	ensure(t, strings.Contains(b.ok("", "show", epic).out, "status: todo"), "epic stays open")
}

func TestCLI_DoneEpic_MissingAndInvalidLinks(t *testing.T) {
	defer guard(t)
	for _, tc := range []struct{ name, link, criteria string }{
		{"no link", "", "- [ ] T-2"},
		{"later link", "Introduction\nEpic: T-1", "- [ ] T-2"},
		{"missing epic", "Epic: T-999", "- [ ] T-2"},
		{"invalid epic", "Epic: T-1 extra", "- [ ] T-2"},
		{"missing line", "Epic: T-1", "- [ ] unrelated"},
		{"id boundary", "Epic: T-1", "- [ ] T-20 other\n- [ ] T-2suffix"},
		{"already checked", "Epic: T-1", "- [x] T-2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBox(t)
			epic := b.newTask("Epic", "todo")
			child := b.newTask("Child", "todo")
			b.edit(epic, "## Details\n"+tc.criteria+"\n")
			b.edit(child, tc.link+"\n## Details\n- [x] shipped\n")
			n := b.commitCount()
			r := b.ok("", "done", child)
			ensure(t, r.err == "" && !strings.Contains(r.out, "ready for acceptance"), "unexpected output: %+v", r)
			ensure(t, b.commitCount() == n+1, "unexpected epic write")
		})
	}
}

func TestCLI_DoneEpic_FailedDoneAndUnreadableEpic(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	epic := b.newTask("Epic", "todo")
	child := b.newTask("Child", "todo")
	b.edit(epic, "## Details\n- [ ] "+child+"\n")
	b.edit(child, "Epic: "+epic+"\n"+critBody)
	n := b.commitCount()
	b.rule("done", child)
	ensure(t, b.commitCount() == n, "failed done must leave epic untouched")
	b.ok("", "check", child, "1", "2")
	if err := os.WriteFile(b.taskFile(epic), []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := b.ok("", "done", child)
	ensure(t, strings.Contains(r.err, "warn epic "+epic), "missing warning: %+v", r)
	ensure(t, string(mustRead(t, b.taskFile(epic))) == "broken", "broken epic overwritten")
	ensure(t, strings.Contains(b.ok("", "show", child).out, "status: done"), "child must close despite epic failure")
}
