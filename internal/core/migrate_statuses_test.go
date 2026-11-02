package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oldStore makes a store whose tasks carry the given old statuses (one task per status) and,
// when config is not empty, the given config.toml; everything is committed. Returns the task ids.
func oldStore(t *testing.T, e *env, config string, statuses ...string) []string {
	t.Helper()
	s := e.openAt(newClock())
	var ids []string
	for i, st := range statuses {
		r, err := s.Create("Task "+st, "proj", "idea", "tester")
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids = append(ids, r.ID)
		data, err := os.ReadFile(e.file(r.ID))
		if err != nil {
			t.Fatal(err)
		}
		out := strings.Replace(string(data), "status: idea", "status: "+st, 1)
		if err := os.WriteFile(e.file(r.ID), []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(e.root, ".tix", "config.toml")
	if config != "" {
		if err := os.WriteFile(cfg, []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.git("add", "-A")
	e.git("-c", "user.name=test", "-c", "user.email=test@localhost", "commit", "-q", "-m", "old store")
	return ids
}

const oldColumns = "[[column]]\nname = \"ideas\"\nrole = \"inbox\"\n\n[[column]]\nname = \"todo\"\nrole = \"ready\"\n\n" +
	"[[column]]\nname = \"review\"\n\n[[column]]\nname = \"wip\"\nrole = \"active\"\n\n" +
	"[[column]]\nname = \"stuck\"\nrole = \"waiting\"\n\n[[column]]\nname = \"fin\"\nrole = \"closed\"\n\n" +
	"[projects]\ntracker = \"tix\"\n"

// The migration goes by the role of the column, not by its name: "todo" here is ready, "wip" is active.
// One commit; the second Open changes nothing.
func TestOpen_MigratesStatusesByRole_OneCommit_Idempotent(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	ids := oldStore(t, e, oldColumns, "ideas", "todo", "wip", "stuck", "fin", "review")
	n := e.count()

	e.open(5 * 1e9)
	if e.count() != n+1 {
		t.Fatalf("commits %d, want %d: the migration is one commit", e.count(), n+1)
	}
	want := []string{"idea", "open", "in_progress", "blocked", "closed", "review"}
	for i, id := range ids {
		if got := "status: " + want[i]; !strings.Contains(e.text(id), got) {
			t.Errorf("%s: want %q in\n%s", id, got, e.text(id))
		}
	}
	cfg, err := os.ReadFile(filepath.Join(e.root, ".tix", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "column") || !strings.Contains(string(cfg), `tracker = "tix"`) {
		t.Errorf("config after migration:\n%s", cfg)
	}
	if st := e.git("status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Errorf("the migration left a dirty tree:\n%s", st)
	}

	e.open(5 * 1e9)
	if e.count() != n+1 {
		t.Errorf("the second Open added commits: %d, want %d", e.count(), n+1)
	}
}

// No [[column]] and no config.toml: the default names idea, todo, doing, blocked, done.
func TestOpen_MigratesDefaultNamesWithoutColumns(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	ids := oldStore(t, e, "", "idea", "todo", "doing", "blocked", "done")
	if err := os.Remove(filepath.Join(e.root, ".tix", "config.toml")); err != nil {
		t.Fatal(err)
	}
	e.git("add", "-A")
	e.git("-c", "user.name=test", "-c", "user.email=test@localhost", "commit", "-q", "-m", "no config")
	n := e.count()

	e.open(5 * 1e9)
	want := []string{"idea", "open", "in_progress", "blocked", "closed"}
	for i, id := range ids {
		if got := "status: " + want[i]; !strings.Contains(e.text(id), got) {
			t.Errorf("%s: want %q", id, got)
		}
	}
	if e.count() != n+1 {
		t.Errorf("commits %d, want %d", e.count(), n+1)
	}
	e.open(5 * 1e9)
	if e.count() != n+1 {
		t.Errorf("the second Open added commits: %d", e.count())
	}
}

// A store already on the five statuses is not touched: no commit, no write.
func TestOpen_NewStore_NoMigration(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	if _, err := s.Create("Fresh", "proj", "idea", "tester"); err != nil {
		t.Fatal(err)
	}
	n := e.count()
	e.open(5 * 1e9)
	if e.count() != n {
		t.Errorf("Open of a migrated store added commits: %d, want %d", e.count(), n)
	}
}

// The migration changes only the status line: blank lines, foreign fields, CRLF and the body stay byte for byte.
func TestOpen_MigratesOnlyTheStatusLine(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	ids := oldStore(t, e, "[[column]]\nname = \"todo\"\nrole = \"ready\"\n", "todo")
	odd := "---\r\nid: T-1\r\n\r\ntitle:   spaced  \r\nstatus: todo\r\nfoo:\r\n    - a\r\n    - b\r\n---\r\nBody  \r\n\r\n"
	if err := os.WriteFile(e.file(ids[0]), []byte(odd), 0o644); err != nil {
		t.Fatal(err)
	}
	e.git("add", "-A")
	e.git("-c", "user.name=test", "-c", "user.email=test@localhost", "commit", "-q", "-m", "odd file")
	e.open(5 * 1e9)
	got, err := os.ReadFile(e.file(ids[0]))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(odd, "status: todo", "status: open", 1); string(got) != want {
		t.Errorf("file after migration:\n%q\nwant:\n%q", got, want)
	}
}
