package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/core"
)

// Status operations (spec 0.9.0 2.3), slice 6.

func statusNames(s *core.Store) string {
	var n []string
	for _, st := range s.Statuses() {
		n = append(n, st.Name)
	}
	return strings.Join(n, " ")
}

func TestS10_AddStatus_AfterLastOfKind_DefaultKept(t *testing.T) {
	e := newEnv(t)
	s := e.open(5e9)
	mustCreate(t, s, "a")
	n := e.count()
	if _, err := s.AddStatus("review", "in_progress", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddStatus("qa", "in_progress", "tester"); err != nil {
		t.Fatal(err)
	}
	ensure(t, statusNames(s) == "idea open in_progress qa review blocked closed" || statusNames(s) == "idea open in_progress review qa blocked closed",
		"order %s", statusNames(s))
	ensure(t, statusNames(s) == "idea open in_progress review qa blocked closed", "qa goes after review: %s", statusNames(s))
	ensure(t, e.count() == n+2, "commits %d, want %d", e.count(), n+2)
	// a fresh process sees the file, and the default of the kind is the first status
	s2 := e.open(5e9)
	mustMove(t, s2, "T-1", "in_progress", core.MoveOpts{})
	ensure(t, statusNames(s2) == statusNames(s), "reopen: %s", statusNames(s2))
	if st := e.git("status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Errorf("dirty tree:\n%s", st)
	}
	ensure(t, s2.Statuses()[2].Tasks == 1, "counts %v", s2.Statuses())
}

func TestS11_RenameStatus_AllTasksOneCommitClaimKept(t *testing.T) {
	e, s := kindStore(t)
	a := mkCrit(t, s, "a", "open")
	b := mkCrit(t, s, "b", "open")
	other := mkCrit(t, s, "c", "open")
	mustMove(t, s, a, "review", core.MoveOpts{})
	mustMove(t, s, b, "review", core.MoveOpts{})
	if _, err := s.Take(a, core.Take, "agent:a"); err != nil {
		t.Fatal(err)
	}
	_, claimA := get(t, s, a).ClaimInfo()
	ensure(t, claimA, "no claim before rename")
	n := e.count()
	if _, err := s.RenameStatus("review", "qa", "tester"); err != nil {
		t.Fatal(err)
	}
	ensure(t, e.count() == n+1, "commits %d, want %d", e.count(), n+1)
	for _, id := range []string{a, b} {
		ensure(t, get(t, s, id).Status == "qa", "%s status %q", id, get(t, s, id).Status)
	}
	ensure(t, get(t, s, other).Status == "open", "other task touched")
	c, has := get(t, s, a).ClaimInfo()
	ensure(t, has && c.Actor == "agent:a", "claim lost: %v %v", c, has)
	ensure(t, statusNames(s) == "idea open in_progress qa blocked closed wontfix", "names %s", statusNames(s))
	if st := e.git("status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Errorf("dirty tree:\n%s", st)
	}
	_, err := s.RenameStatus("review", "x", "tester")
	wantRule(t, "rename of the old name", err, "unknown status")
}

func TestS12_RenameInterrupted_SameCommandFinishes(t *testing.T) {
	e, s := kindStore(t)
	a := mkCrit(t, s, "a", "open")
	b := mkCrit(t, s, "b", "open")
	mustMove(t, s, a, "review", core.MoveOpts{})
	mustMove(t, s, b, "review", core.MoveOpts{})
	// the run died after the first task file: a carries qa, the config still says review
	data, _ := os.ReadFile(e.file(a))
	if err := os.WriteFile(e.file(a), []byte(strings.Replace(string(data), "status: review", "status: qa", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	ensure(t, s.Statuses()[3].Tasks == 1, "a is in the column ?, counts %v", s.Statuses())
	if _, err := s.RenameStatus("review", "qa", "tester"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a, b} {
		ensure(t, get(t, s, id).Status == "qa", "%s status %q", id, get(t, s, id).Status)
	}
	ensure(t, s.Statuses()[3].Name == "qa" && s.Statuses()[3].Tasks == 2, "counts %v", s.Statuses())
}

func TestS13_RemoveStatus_WithTasksAndLastOfKind_Refused(t *testing.T) {
	_, s := kindStore(t)
	id := mkCrit(t, s, "a", "open")
	mustMove(t, s, id, "review", core.MoveOpts{})
	_, err := s.RemoveStatus("review", "tester")
	wantRule(t, "rm with a task", err, "has 1 tasks")
	ensure(t, err != nil && strings.Contains(err.Error(), "tix status rename") && strings.Contains(err.Error(), "tix move"), "hints: %v", err)
	_, err = s.RemoveStatus("idea", "tester")
	wantRule(t, "rm last of kind", err, "last of the kind idea")
	mustMove(t, s, id, "open", core.MoveOpts{})
	if _, err := s.RemoveStatus("review", "tester"); err != nil {
		t.Fatal(err)
	}
	ensure(t, statusNames(s) == "idea open in_progress blocked closed wontfix", "names %s", statusNames(s))
	_, err = s.RemoveStatus("nope", "tester")
	wantRule(t, "rm unknown", err, "unknown status")
}

func TestS14_RenameAndRemove_UnparsedFile_Refused(t *testing.T) {
	e, s := kindStore(t)
	mkCrit(t, s, "a", "open")
	if err := os.WriteFile(filepath.Join(e.root, "T-99 broken.md"), []byte("not a task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.RenameStatus("review", "qa", "tester")
	wantRule(t, "rename", err, "T-99 broken.md")
	_, err = s.RemoveStatus("wontfix", "tester")
	wantRule(t, "rm", err, "T-99 broken.md")
	ensure(t, statusNames(s) == "idea open in_progress review blocked closed wontfix", "config changed: %s", statusNames(s))
}

func TestS29_AddStatus_Refusals(t *testing.T) {
	_, s := kindStore(t)
	for _, c := range []struct{ name, kind, want string }{
		{"review", "open", "already exists"},
		{"Bad Name", "open", "use a-z"},
		{"closed", "open", "kind word"},
		{"fresh", "nope", "unknown kind"},
	} {
		_, err := s.AddStatus(c.name, c.kind, "tester")
		wantRule(t, c.name+" "+c.kind, err, c.want)
	}
}

func TestStatusOps_NoSection_WritesFiveDefaultsFirst(t *testing.T) {
	e := newEnv(t)
	s := e.open(5e9)
	if _, err := s.RenameStatus("open", "todo", "tester"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(e.root, ".tix", "config.toml"))
	for _, k := range []string{`"idea"`, `"todo"`, `"in_progress"`, `"blocked"`, `"closed"`} {
		ensure(t, strings.Contains(string(cfg), k), "config lacks %s:\n%s", k, cfg)
	}
}
