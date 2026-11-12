package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Kinds in the output (spec 0.9.0 2.2), slice 3.

const kindConfig = `format = 2

[[status]]
name = "idea"
kind = "idea"

[[status]]
name = "open"
kind = "open"

[[status]]
name = "in_progress"
kind = "in_progress"

[[status]]
name = "review"
kind = "in_progress"

[[status]]
name = "blocked"
kind = "blocked"

[[status]]
name = "closed"
kind = "closed"

[[status]]
name = "wontfix"
kind = "closed"
`

// kindBox - a box with the review status and one task in review, one in open.
func kindBox(t *testing.T) (b *box, inReview, inOpen string) {
	t.Helper()
	b = newBox(t)
	if err := os.MkdirAll(filepath.Join(b.home, ".tix"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.home, ".tix", "config.toml"), []byte(kindConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	inOpen = b.newTask("Plain", "open")
	inReview = b.newTask("Looked at", "open")
	b.ok("", "move", inReview, "review")
	return b, inReview, inOpen
}

func TestS15_TextOutputPrintsNameAndKind(t *testing.T) {
	defer guard(t)
	b, rev, open := kindBox(t)
	list := b.ok("", "list").out
	if !strings.Contains(list, "review [in_progress] "+rev+" Looked at") {
		t.Errorf("list: %q, want review [in_progress]", list)
	}
	if !strings.Contains(list, "open "+open+" Plain") || strings.Contains(list, "open [") {
		t.Errorf("list: %q, want a bare open", list)
	}
	if out := b.ok("", "show", rev).out; !strings.Contains(out, "Status: review [in_progress]") {
		t.Errorf("show: %q, want the Status line", out)
	}
	if out := b.ok("", "show", open).out; strings.Contains(out, "Status:") {
		t.Errorf("show of a kind-named status: %q, want no Status line", out)
	}
	proj := b.ok("", "list", "--json").out
	var items []struct{ ID, Status, Kind string }
	if err := json.Unmarshal([]byte(proj), &items); err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		want := map[string]string{rev: "in_progress", open: "open"}[it.ID]
		if it.Kind != want {
			t.Errorf("list --json %s: kind %q, want %q", it.ID, it.Kind, want)
		}
	}
	ctx := b.ok("", "ctx", "--project", "personal").out
	if !strings.Contains(ctx, "review [in_progress]") {
		t.Errorf("ctx: %q, want review [in_progress]", ctx)
	}
	var br struct {
		Sections []struct{ Column, Kind string }
	}
	if err := json.Unmarshal([]byte(b.ok("", "ctx", "--project", "personal", "--json").out), &br); err != nil {
		t.Fatal(err)
	}
	if len(br.Sections) != 2 || br.Sections[0].Column != "review" || br.Sections[0].Kind != "in_progress" {
		t.Errorf("ctx --json sections: %+v", br.Sections)
	}
}

func TestS29_ListFiltersByNameAndByKind(t *testing.T) {
	defer guard(t)
	b, rev, open := kindBox(t)
	if out := b.ok("", "list", "--status", "review").out; !strings.Contains(out, rev) || strings.Contains(out, open) {
		t.Errorf("--status review: %q", out)
	}
	b.ok("", "take", open, "--as", "claude:a")
	if out := b.ok("", "list", "--kind", "in_progress").out; !strings.Contains(out, rev) || !strings.Contains(out, open) {
		t.Errorf("--kind in_progress: %q, want both tasks", out)
	}
	if out := b.ok("", "list", "--status", "in_progress").out; strings.Contains(out, rev) {
		t.Errorf("--status in_progress: %q, must not hold the review task", out)
	}
	r := b.tix("", "list", "--kind", "review")
	if r.code == 0 || !strings.Contains(r.err, "kinds: idea open in_progress blocked closed") {
		t.Errorf("--kind review: code %d, %q, want a refusal with the kinds", r.code, r.err)
	}
}

func TestS6_DoneUsesTheClosedKind(t *testing.T) {
	defer guard(t)
	b, rev, _ := kindBox(t)
	b.ok("", "done", rev, "--force", "-m", "ok")
	if out := b.ok("", "list", "--kind", "closed").out; !strings.Contains(out, "closed "+rev) {
		t.Errorf("done: %q, want the task in closed", out)
	}
	b.ok("", "move", rev, "wontfix")
	n := b.commitCount()
	b.ok("", "done", rev, "--force", "-m", "ok")
	if b.commitCount() != n {
		t.Errorf("done on a wontfix task made a commit")
	}
	if r := b.tix("", "move", rev, "nope"); r.code == 0 || !strings.Contains(r.err, "wontfix") {
		t.Errorf("move to nope: %q, want the configured names", r.err)
	}
}

const namedConfig = `format = 2

[[status]]
name = "backlog"
kind = "idea"

[[status]]
name = "todo"
kind = "open"

[[status]]
name = "ready"
kind = "open"

[[status]]
name = "doing"
kind = "in_progress"

[[status]]
name = "blocked"
kind = "blocked"

[[status]]
name = "shipped"
kind = "closed"

[[status]]
name = "wontfix"
kind = "closed"
`

func namedBox(t *testing.T) *box {
	t.Helper()
	b := newBox(t)
	if err := os.MkdirAll(filepath.Join(b.home, ".tix"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.home, ".tix", "config.toml"), []byte(namedConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

// namedTask creates a task in backlog with criteria and moves it to the status to.
func namedTask(b *box, title, to string) string {
	b.t.Helper()
	id := strings.Fields(b.ok("", "new", title).out)[1]
	b.edit(id, critBody)
	b.ok("", "move", id, to)
	return id
}

func TestS15_NewGoesToFirstIdeaStatus_ListFollowsConfigOrder(t *testing.T) {
	defer guard(t)
	b := namedBox(t)
	a := b.ok("", "new", "Alpha thing").out
	if !strings.Contains(a, "backlog") {
		t.Errorf("new: %q, want backlog", a)
	}
	id := namedTask(b, "Beta thing", "ready")
	first := namedTask(b, "Gamma thing", "todo")
	lines := strings.Split(strings.TrimSpace(b.ok("", "list").out), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "backlog [idea]") ||
		!strings.Contains(lines[1], "todo [open] "+first) || !strings.Contains(lines[2], "ready [open] "+id) {
		t.Errorf("list order: %q", lines)
	}
}

func TestS15_CtxNextFromSecondOpenStatus(t *testing.T) {
	defer guard(t)
	b := namedBox(t)
	dep := strings.Fields(b.ok("", "new", "Dependency").out)[1]
	todo := namedTask(b, "Waiting one", "todo")
	b.edit(todo, critBody+"After: "+dep+"\n")
	ready := namedTask(b, "Free one", "ready")
	if out := b.ok("", "ctx", "--project", "personal").out; !strings.Contains(out, "next "+ready) {
		t.Errorf("ctx: %q, want next %s", out, ready)
	}
}

func TestS29_LikeWithKindClosedFindsWontfix(t *testing.T) {
	defer guard(t)
	b := namedBox(t)
	id := namedTask(b, "Duplicate cards report", "todo")
	b.ok("", "move", id, "wontfix", "-m", "no", "--force")
	if out := b.ok("", "list", "--like", "duplicate cards report", "--kind", "closed").out; !strings.Contains(out, "wontfix") {
		t.Errorf("--like with --kind closed: %q, want the wontfix task", out)
	}
	if out := b.ok("", "list", "--like", "duplicate cards report").out; strings.Contains(out, id) {
		t.Errorf("--like alone: %q, a wontfix task counts as closed", out)
	}
}

func TestS25_UncheckEndToEnd(t *testing.T) {
	defer guard(t)
	b, rev, _ := kindBox(t)
	b.ok("", "check", rev, "1", "2")
	if out := b.ok("", "uncheck", rev, "2").out; !strings.Contains(out, "ok "+rev+" 1/") {
		t.Errorf("uncheck: %q, want `ok %s 1/N`", out, rev)
	}
	if r := b.tix("", "uncheck", rev, "99"); r.code == 0 {
		t.Errorf("uncheck 99 accepted")
	}
	b.ok("", "done", rev, "--force", "-m", "ok")
	if r := b.tix("", "uncheck", rev, "1"); r.code != 5 || !strings.Contains(r.err, "reopen") {
		t.Errorf("uncheck in closed: code %d %q, want 5 and the hint", r.code, r.err)
	}
}
