package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const newBody = "## Summary\n\nOne call\n\n## Details\n\n- [ ] Works\n"

func showBody(t *testing.T, b *box, id string) (body, status string) {
	t.Helper()
	var tk struct{ Body, Status string }
	if err := json.Unmarshal([]byte(b.ok("", "show", id, "--json").out), &tk); err != nil {
		t.Fatal(err)
	}
	return tk.Body, tk.Status
}

func TestCLI_New_BodyFileStdin(t *testing.T) {
	defer guard(t)
	for _, to := range []string{"idea", "todo", "ready"} {
		t.Run(to, func(t *testing.T) {
			b := newBox(t)
			b.ok(newBody, "new", "Fresh", "--to", to, "--body-file", "-")
			body, status := showBody(t, b, "T-1")
			want := to
			if to == "ready" {
				want = "todo"
			}
			ensure(t, body == newBody && status == want, "created body %q, status %q", body, status)
		})
	}
}

func TestCLI_New_BodyFilePath(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	if err := os.WriteFile(filepath.Join(b.cwd, "body.md"), []byte(newBody), 0o600); err != nil {
		t.Fatal(err)
	}
	b.ok("", "new", "Fresh", "--to", "ready", "--body-file", "body.md")
	body, status := showBody(t, b, "T-1")
	ensure(t, body == newBody && status == "todo", "created body %q, status %q", body, status)
	r := b.tix("", "new", "Missing", "--body-file", "nope.md")
	ensure(t, r.code == 6 && strings.Contains(r.err, "body file"), "missing file: %+v", r)
}

// Stdin without --body-file stays unread: a shell loop keeps its lines, an open pipe does not hang new.
func TestCLI_New_StdinIgnoredWithoutFlag(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("two\nthree\n", "new", "Loop one")
	body, _ := showBody(t, b, "T-1")
	ensure(t, body == "", "body %q, want empty", body)
}

func TestCLI_New_ReadyWithoutCriteriaLeavesStoreUnchanged(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Seed")
	before := b.snapshot()
	for _, body := range []string{"", "## Details\nNo checks\n", "## Details\n```\n- [ ] Example\n```\n"} {
		r := b.tix(body, "new", "Fresh", "--to", "ready", "--body-file", "-")
		ensure(t, r.code == 5, "expected rule refusal: %+v", r)
		ensure(t, b.snapshot() == before, "refusal changed the store")
	}
}

// spec: T-82 scenario 5
func TestCLI_New_ToCheckedColumn_RefusedNothingWritten(t *testing.T) {
	defer guard(t)
	path := map[string]string{"todo": "--body-file", "ready": "--body-file", "blocked": "tix move <id>",
		"waiting": "tix move <id>", "done": "tix done <id>", "closed": "tix done <id>"}
	for to, want := range path {
		t.Run(to, func(t *testing.T) {
			b := newBox(t)
			b.ok("", "new", "Seed")
			before := b.snapshot()
			msg := b.rule("new", "Fresh", "--to", to)
			ensure(t, strings.Contains(msg, "a new task"), "refusal %q does not say why", msg)
			ensure(t, strings.Contains(msg, want), "refusal %q does not name the path %q", msg, want)
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
