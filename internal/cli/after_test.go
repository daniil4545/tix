package cli_test

import (
	"strings"
	"testing"
)

// After: marks a waiting task in list and show, ctx skips it as next, take still works.
func TestCLI_After_ListShowCtxTake(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	wait, dep, other := b.newTask("Waits", "open"), b.newTask("Dep", "open"), b.newTask("Other", "open")
	b.rewrite(wait, func(s string) string { return s + "\nAfter: " + dep + ", T-999\n" })

	list := b.ok("", "list").out
	ensure(t, strings.Contains(list, "open "+wait+" Waits | after "+dep+"\n"), "list: %q", list)
	ensure(t, strings.Contains(list, "open "+other+" Other\n"), "task without After is unchanged: %q", list)
	show := b.ok("", "show", wait).out
	ensure(t, strings.Contains(show, "\nafter "+dep+"\n"), "show: %q", show)
	ensure(t, !strings.Contains(b.ok("", "show", other).out, "\nafter "), "show without After")
	ctx := b.ok("", "ctx").out
	ensure(t, strings.Contains(ctx, "next "+dep), "next must skip the waiting task: %q", ctx)

	b.ok("", "take", wait, "--as", "agent:t")
	b.ok("", "move", dep, "in_progress")
	b.ok("", "check", dep, "1", "2")
	b.ok("", "move", dep, "closed")
	list = b.ok("", "list").out
	ensure(t, !strings.Contains(list, "after"), "closed dep must free the task: %q", list)
}
