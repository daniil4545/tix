package cli_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Tasks 30 days old: a note resets the idle time, an external edit, set and a migration do not; in_progress is never stale.
func TestCLI_Stale_CtxAndList(t *testing.T) {
	defer guard(t)
	old := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", old)
	t.Setenv("GIT_COMMITTER_DATE", old)
	b := newBox(t)
	idea, open, noted, edited := b.newTask("Idea", "idea"), b.newTask("Open", "open"),
		b.newTask("Noted", "idea"), b.newTask("Edited", "idea")
	work := b.newTask("Work", "in_progress")
	for _, k := range []string{"GIT_AUTHOR_DATE", "GIT_COMMITTER_DATE"} {
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	created := regexp.MustCompile(`(?m)^created: .*$`)
	for _, id := range []string{idea, open, noted, edited, work} {
		b.rewrite(id, func(s string) string { return created.ReplaceAllString(s, "created: "+old[:19]) })
	}
	b.rewrite(edited, func(s string) string { return s + "\nedited by hand\n" })
	b.ok("", "set", idea, "tags=x")
	// a mass commit of tix, like the status migration, touches every file and is not a move
	b.rewrite(open, func(s string) string { return s + "\nmigrated\n" })
	b.git("commit", "-qam", "tix: migrate statuses to idea open in_progress blocked closed")
	b.ok("", "note", noted, "still alive")

	ctx := b.ok("", "ctx", "--project", "personal").out
	m := regexp.MustCompile(`(?m)^stale 14d\+: (\d+) \((.*)\)$`).FindStringSubmatch(ctx)
	ensure(t, m != nil && m[1] == "3", "stale line: %q", ctx)
	for _, id := range []string{idea, open, edited} {
		ensure(t, strings.Contains(m[2], id), "%s must be stale: %q", id, m[2])
	}
	ensure(t, !strings.Contains(m[2], noted) && !strings.Contains(m[2], work), "noted and in_progress are not stale: %q", m[2])

	out := b.ok("", "list", "--stale").out
	ensure(t, strings.Contains(out, "idea "+idea+" Idea | idle 30d") && strings.Contains(out, "open "+open+" Open | idle 30d"),
		"list --stale: %q", out)
	ensure(t, !strings.Contains(out, noted) && !strings.Contains(out, work), "list --stale extra: %q", out)
	ensure(t, strings.Contains(b.ok("", "list", "--stale", "--json").out, `"idle_days":30`), "json idle_days")
}

// A fresh store has no stale line.
func TestCLI_Stale_NoneNoLine(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.newTask("Fresh", "idea")
	ensure(t, !strings.Contains(b.ok("", "ctx", "--project", "personal").out, "stale"), "no stale line")
}
