package core_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/core"
)

// putTask writes a task file by hand, like an old 0.8 store.
func (e *env) putTask(id, status, extra, body string) {
	e.t.Helper()
	text := fmt.Sprintf("---\nid: %s\ntitle: Task %s\nproject: proj\nstatus: %s\nrank: %s\ncreated: 2026-01-01T10:00:00\n%s---\n%s",
		id, id, status, strings.TrimPrefix(id, "T-"), extra, body)
	if err := os.MkdirAll(e.root, 0o750); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, id+"-task.md"), []byte(text), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// writeProbe opens the store and makes the first write, which runs the migration.
func (e *env) writeProbe() {
	e.t.Helper()
	if _, err := e.open(2*time.Second).Create("Probe", "proj", "idea", "tester"); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) migrateSubjects() int {
	return strings.Count(e.git("log", "--format=%s"), "migrate: epic lines to parent")
}

const epicTags = "tags:\n  - epic\n  - keep\n"

// oldStore is the S22 fixture: an epic in an epic, T-5 in two epics, a missing T-9, a closed epic,
// [x] on an open child, a child with a live claim, an epic with no lines.
func (e *env) oldStore() {
	e.writeConfig("")
	e.putTask("T-1", "open", epicTags, "## Details\n- [ ] T-5 five\n- [x] T-6 six\n- [ ] T-9 nine\n- [ ] plain\n")
	e.putTask("T-2", "closed", epicTags, "## Details\n- [x] T-5 five\n- [ ] T-3 three\n")
	e.putTask("T-3", "open", epicTags, "## Details\n- [ ] T-7 seven\n")
	e.putTask("T-4", "open", epicTags, "## Details\n- [ ] plain\n- [ ] T-4 itself\n")
	e.putTask("T-5", "open", "", "text\n")
	e.putTask("T-6", "open", "", "text\n")
	e.putTask("T-7", "in_progress", "claim: claude:a until 2099-01-01T00:00\n", "text\n")
}

func TestMigrateParent_S22_FixtureCounts(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	e.oldStore()
	e.writeProbe()
	for id, parent := range map[string]string{"T-5": "T-2", "T-6": "T-1", "T-7": "T-3"} {
		ensure(t, strings.Contains(e.text(id), "parent: "+parent), "%s parent want %s:\n%s", id, parent, e.text(id))
	}
	ensure(t, !strings.Contains(e.text("T-3"), "parent:"), "T-3 stays a root:\n%s", e.text("T-3"))
	t1, t2, t3 := e.text("T-1"), e.text("T-2"), e.text("T-3")
	ensure(t, !strings.Contains(t1, "T-5 ") && !strings.Contains(t1, "T-6 "), "linked lines stay in T-1:\n%s", t1)
	ensure(t, strings.Contains(t1, "- [ ] T-9 nine") && strings.Contains(t1, "- [ ] plain"), "kept lines lost:\n%s", t1)
	ensure(t, strings.Contains(t2, "- [ ] T-3 three") && !strings.Contains(t2, "T-5"), "T-2 lines wrong:\n%s", t2)
	ensure(t, !strings.Contains(t3, "T-7"), "T-7 line stays:\n%s", t3)
	for _, id := range []string{"T-1", "T-2", "T-3", "T-4"} {
		x := e.text(id)
		ensure(t, !strings.Contains(x, "epic") && strings.Contains(x, "keep"), "%s tags wrong:\n%s", id, x)
	}
	ensure(t, strings.Contains(e.text("T-4"), "- [ ] T-4 itself"), "a line naming its own epic must stay:\n%s", e.text("T-4"))
	ensure(t, strings.Contains(e.text("T-2"), "status: closed") && strings.Contains(e.text("T-5"), "status: open") &&
		strings.Contains(e.text("T-7"), "status: in_progress") &&
		strings.Contains(e.text("T-7"), "claim: claude:a until 2099-01-01T00:00"), "status or claim changed")
	cfg, _ := os.ReadFile(filepath.Join(e.root, ".tix", "config.toml"))
	ensure(t, strings.Contains(string(cfg), "format = 2"), "config: %s", cfg)
	ensure(t, e.migrateSubjects() == 1, "want one migrate commit:\n%s", e.git("log", "--format=%s"))
	want := "migrate: epic lines to parent: 4 epics, 3 children; in several epics: T-5; kept as criteria: T-3 T-9"
	ensure(t, strings.Contains(e.log.String(), want), "report line missing:\n%s", e.log.String())
	ensure(t, e.git("status", "--porcelain") == "", "dirty tree:\n%s", e.git("status", "--porcelain"))
}

// A tagged task with no task lines listed in another epic is both a child and an epic: one write.
func TestMigrateParent_TaggedChild(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	e.writeConfig("")
	e.putTask("T-1", "open", epicTags, "## Details\n- [ ] T-2 two\n")
	e.putTask("T-2", "open", epicTags, "## Details\n- [ ] plain\n")
	e.writeProbe()
	t2 := e.text("T-2")
	ensure(t, strings.Contains(t2, "parent: T-1") && !strings.Contains(t2, "epic"), "T-2 wrong:\n%s", t2)
	ensure(t, e.migrateSubjects() == 1, "want one migrate commit:\n%s", e.git("log", "--format=%s"))
	ensure(t, e.git("status", "--porcelain") == "", "dirty tree:\n%s", e.git("status", "--porcelain"))
}

func TestMigrateParent_RerunChangesNothing(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	e.oldStore()
	e.writeProbe()
	n := e.count()
	snap := e.git("show", "HEAD:T-1-task.md")
	s2 := e.open(2 * time.Second)
	if _, err := s2.Create("New2", "proj", "idea", "tester"); err != nil {
		t.Fatal(err)
	}
	ensure(t, e.migrateSubjects() == 1, "second migration:\n%s", e.git("log", "--format=%s"))
	ensure(t, e.count() == n+1, "commits: %d want %d", e.count(), n+1)
	ensure(t, e.git("show", "HEAD:T-1-task.md") == snap, "T-1 changed")
}

func TestMigrateParent_ReadDoesNotMigrate(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	e.oldStore()
	if err := os.WriteFile(filepath.Join(e.root, ".tix", "config.toml"), []byte("[projects]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := e.open(2 * time.Second)
	before := e.text("T-1")
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("T-1"); err != nil {
		t.Fatal(err)
	}
	ensure(t, e.text("T-1") == before && !strings.Contains(e.text("T-5"), "parent:"), "a read migrated the store")
	_, err := os.Stat(filepath.Join(e.root, ".git"))
	ensure(t, err != nil, "a read created git")
}

// S23: the state an interrupted run leaves on disk; the next write must reach the clean result.
func TestMigrateParent_S23_Interrupted(t *testing.T) {
	defer guard(t)
	clean := func(e *env) map[string]string {
		e.writeProbe()
		out := map[string]string{}
		for _, id := range []string{"T-1", "T-2", "T-3", "T-4", "T-5", "T-6", "T-7"} {
			out[id] = e.text(id)
		}
		return out
	}
	e0 := newEnv(t)
	e0.oldStore()
	want := clean(e0)

	cases := map[string]func(e *env){
		"children written": func(e *env) {
			e.putTask("T-5", "open", "parent: T-2\ntags: []\n", "text\n")
			e.putTask("T-6", "open", "parent: T-1\ntags: []\n", "text\n")
			e.putTask("T-7", "in_progress", "claim: claude:a until 2099-01-01T00:00\nparent: T-3\ntags: []\n", "text\n")
		},
		"nested epic written": func(e *env) { // T-3 done (lines, tag), its child written; T-2 untouched
			e.putTask("T-7", "in_progress", "claim: claude:a until 2099-01-01T00:00\nparent: T-3\ntags: []\n", "text\n")
			e.putTask("T-3", "open", "tags:\n  - keep\n", "## Details\n")
		},
		"two epics, smaller one written": func(e *env) { // T-5 child of T-2; T-1 lost its T-5 line and tag
			e.putTask("T-5", "open", "parent: T-2\ntags: []\n", "text\n")
			e.putTask("T-6", "open", "parent: T-1\ntags: []\n", "text\n")
			e.putTask("T-1", "open", "tags:\n  - keep\n", "## Details\n- [ ] T-9 nine\n- [ ] plain\n")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.oldStore()
			mutate(e)
			got := clean(e)
			for id, w := range want {
				if id == "T-1" && name == "two epics, smaller one written" {
					continue
				}
				if id == "T-3" && name == "nested epic written" {
					continue
				}
				ensure(t, got[id] == w, "%s differs from a clean run:\n%s\nwant:\n%s", id, got[id], w)
			}
			ensure(t, !strings.Contains(got["T-1"], "T-5 "), "kept line of a linked task:\n%s", got["T-1"])
			ensure(t, !strings.Contains(got["T-2"], "T-5 "), "kept line of a linked task:\n%s", got["T-2"])
		})
	}
}

// S24: a store with format = 2 is not scanned: an unreadable task file does not matter. (No read
// counter seam exists in core; a scan would fail the write on the unreadable T-3.)
func TestMigrateParent_S24_Format2NotScanned(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	e.writeConfig("format = 2\n")
	e.putTask("T-1", "open", epicTags, "## Details\n- [ ] T-2 two\n")
	e.putTask("T-2", "open", "", "text\n")
	e.putTask("T-3", "open", "", "text\n")
	if err := os.Chmod(filepath.Join(e.root, "T-3-task.md"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(e.root, "T-3-task.md"), 0o600) })
	s := e.open(2 * time.Second)
	if _, err := s.Create("New", "proj", "idea", "tester"); err != nil {
		t.Fatal(err)
	}
	ensure(t, strings.Contains(e.text("T-1"), "- [ ] T-2 two") && !strings.Contains(e.text("T-2"), "parent:"), "scanned")
	ensure(t, e.migrateSubjects() == 0, "migrated a format 2 store")
}

// A file that does not parse is skipped, left byte for byte, and named in the report.
func TestMigrateParent_UnparsedSkipped(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	e.oldStore()
	bad := filepath.Join(e.root, "T-8-bad.md")
	if err := os.WriteFile(bad, []byte("no frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.writeProbe()
	got, _ := os.ReadFile(bad)
	ensure(t, string(got) == "no frontmatter\n", "bad file rewritten")
	ensure(t, strings.Contains(e.log.String(), "skipped files: T-8"), "report: %s", e.log.String())
	ensure(t, strings.Contains(e.text("T-5"), "parent: T-2"), "the rest not migrated")
}

// The first write of an old store can be any operation: the migration commit comes first, the
// operation then works on the migrated files.
func TestMigrateParent_FirstWriteNote(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	e.oldStore()
	if _, err := e.open(2*time.Second).Note("T-1", "hello", "tester"); err != nil {
		t.Fatal(err)
	}
	log := strings.Split(strings.TrimSpace(e.git("log", "--format=%s", "-3")), "\n")
	ensure(t, strings.Contains(log[0], "note hello") && log[1] == "migrate: epic lines to parent", "commits: %v", log)
	ensure(t, !strings.Contains(e.text("T-1"), "T-5 ") && strings.Contains(e.text("T-5"), "parent: T-2"), "not migrated")
	cfg, _ := os.ReadFile(filepath.Join(e.root, ".tix", "config.toml"))
	ensure(t, strings.Contains(string(cfg), "format = 2"), "config: %s", cfg)
}

func TestMigrateParent_FirstWriteMove(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	e.oldStore()
	if _, err := e.open(2*time.Second).Move("T-6", "closed", core.MoveOpts{Force: true, Message: "x"}, "tester"); err != nil {
		t.Fatal(err)
	}
	log := strings.Split(strings.TrimSpace(e.git("log", "--format=%s", "-3")), "\n")
	ensure(t, log[1] == "migrate: epic lines to parent", "commits: %v", log)
	got := e.text("T-6")
	ensure(t, strings.Contains(got, "parent: T-1") && strings.Contains(got, "status: closed"), "move lost the parent:\n%s", got)
	ensure(t, !strings.Contains(e.text("T-1"), "T-6 "), "line of T-6 came back:\n%s", e.text("T-1"))
}
