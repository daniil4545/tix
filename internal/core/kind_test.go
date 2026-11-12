package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/core"
)

// Rules by the kind of a status (spec 0.9.0 2.2), slice 2.

const sevenStatuses = `format = 2

[projects]

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

func (e *env) writeConfig(src string) {
	e.t.Helper()
	dir := filepath.Join(e.root, ".tix")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(src), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

func kindStore(t *testing.T) (*env, *core.Store) {
	t.Helper()
	e := newEnv(t)
	e.writeConfig(sevenStatuses)
	return e, e.open(5e9)
}

func mustMove(t *testing.T, s *core.Store, id, to string, o core.MoveOpts) {
	t.Helper()
	if _, err := s.Move(id, to, o, "tester"); err != nil {
		t.Fatalf("Move %s to %s: %v", id, to, err)
	}
}

func TestS3_TakeInReview_StaysInReview(t *testing.T) {
	_, s := kindStore(t)
	id := mkCrit(t, s, "a", "open")
	r, err := s.Move(id, "review", core.MoveOpts{}, "tester")
	ensure(t, err == nil && r.Info == "open > review [in_progress]", "info %q %v", r.Info, err)
	if _, err := s.Take(id, core.Take, "agent:a"); err != nil {
		t.Fatal(err)
	}
	tk := get(t, s, id)
	_, has := tk.ClaimInfo()
	ensure(t, tk.Status == "review" && has, "status %q claim %v, want review with claim", tk.Status, has)
}

func TestS4_TakeFromOpen_FirstStatusOfKind(t *testing.T) {
	_, s := kindStore(t)
	id := mkCrit(t, s, "a", "open")
	if _, err := s.Take(id, core.Take, "agent:a"); err != nil {
		t.Fatal(err)
	}
	ensure(t, get(t, s, id).Status == "in_progress", "status %q, want in_progress", get(t, s, id).Status)
}

func TestS5_MoveSameKind_NoChecksClaimKept(t *testing.T) {
	_, s := kindStore(t)
	id := mkCrit(t, s, "a", "open")
	if _, err := s.Take(id, core.Take, "agent:a"); err != nil {
		t.Fatal(err)
	}
	mustMove(t, s, id, "review", core.MoveOpts{})
	tk := get(t, s, id)
	c, has := tk.ClaimInfo()
	ensure(t, tk.Status == "review" && has && c.Actor == "agent:a", "status %q claim %v %v", tk.Status, has, c)
	mustMove(t, s, id, "in_progress", core.MoveOpts{})
	_, has = get(t, s, id).ClaimInfo()
	ensure(t, has, "claim lost on a move back")
}

func TestS6_Close_FirstClosedStatusAndRepeatIsNoop(t *testing.T) {
	e, s := kindStore(t)
	id := mkCrit(t, s, "a", "open")
	mustMove(t, s, id, "review", core.MoveOpts{})
	if _, err := s.Close(id, core.MoveOpts{Message: "r", Force: true}, "tester"); err != nil {
		t.Fatal(err)
	}
	ensure(t, get(t, s, id).Status == "closed", "status %q, want closed", get(t, s, id).Status)
	mustMove(t, s, id, "wontfix", core.MoveOpts{})
	before := e.git("rev-parse", "HEAD")
	r, err := s.Close(id, core.MoveOpts{Message: "again"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	ensure(t, get(t, s, id).Status == "wontfix" && r.Commit == "" && e.git("rev-parse", "HEAD") == before,
		"status %q commit %q, want wontfix with no commit", get(t, s, id).Status, r.Commit)
}

func TestS7_ReleaseFromReview_DefaultOpen(t *testing.T) {
	_, s := kindStore(t)
	id := mkCrit(t, s, "a", "open")
	mustMove(t, s, id, "review", core.MoveOpts{})
	if _, err := s.Take(id, core.Take, "agent:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Take(id, core.Release, "agent:a"); err != nil {
		t.Fatal(err)
	}
	tk := get(t, s, id)
	_, has := tk.ClaimInfo()
	ensure(t, tk.Status == "open" && !has, "status %q claim %v, want open with no claim", tk.Status, has)
	id2 := mkCrit(t, s, "b", "open")
	mustMove(t, s, id2, "review", core.MoveOpts{})
	if _, err := s.Take(id2, core.Take, "agent:a"); err != nil {
		t.Fatal(err)
	}
	ids, _, err := s.ReleaseAll("agent:a")
	ensure(t, err == nil && len(ids) == 1 && get(t, s, id2).Status == "open", "ReleaseAll %v %v", ids, err)
}

func TestS8_TakeFromIdea_NeedsCriteria(t *testing.T) {
	_, s := kindStore(t)
	with := mkCrit(t, s, "a", "idea")
	without := mkIn(t, s, "b", "idea")
	if _, err := s.Take(with, core.Take, "agent:a"); err != nil {
		t.Fatalf("with criteria: %v", err)
	}
	ensure(t, get(t, s, with).Status == "in_progress", "status %q", get(t, s, with).Status)
	_, err := s.Take(without, core.Take, "agent:a")
	wantRule(t, "take without criteria", err, "no criteria")
}

func TestS9_RenamedByOtherProcess_MoveToOldNameRefused(t *testing.T) {
	e, s := kindStore(t)
	id := mkCrit(t, s, "a", "open")
	e.writeConfig(strings.ReplaceAll(sevenStatuses, `"review"`, `"qa"`))
	_, err := s.Move(id, "review", core.MoveOpts{}, "tester")
	wantRule(t, "move to the old name", err, "qa")
	mustMove(t, s, id, "qa", core.MoveOpts{})
}

func TestS28_UnknownNameListsConfigured_UnknownStatusMovesOutWithoutClaim(t *testing.T) {
	e, s := kindStore(t)
	id := mkCrit(t, s, "a", "open")
	_, err := s.Move(id, "todo", core.MoveOpts{}, "tester")
	wantRule(t, "unknown name", err, "wontfix")
	if _, err := s.Take(id, core.Take, "agent:a"); err != nil {
		t.Fatal(err)
	}
	txt := strings.Replace(e.text(id), "status: in_progress", "status: ghost", 1)
	if err := os.WriteFile(e.file(id), []byte(txt), 0o600); err != nil {
		t.Fatal(err)
	}
	mustMove(t, s, id, "open", core.MoveOpts{})
	tk := get(t, s, id)
	_, has := tk.ClaimInfo()
	ensure(t, tk.Status == "open" && !has, "status %q claim %v, want open with no claim", tk.Status, has)
}

// renamedStatuses: the default status of every kind is not named as the kind word.
const renamedStatuses = `format = 2

[projects]

[[status]]
name = "idea"
kind = "idea"

[[status]]
name = "todo2"
kind = "open"

[[status]]
name = "doing"
kind = "in_progress"

[[status]]
name = "review"
kind = "in_progress"

[[status]]
name = "blocked"
kind = "blocked"

[[status]]
name = "parked"
kind = "blocked"

[[status]]
name = "shipped"
kind = "closed"

[[status]]
name = "wontfix"
kind = "closed"
`

func TestS4_S6_S7_DefaultNamesFromConfig_InfoAndKeptFields(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(renamedStatuses)
	s := e.open(5e9)
	r, err := s.Create("a", "proj", "", "tester")
	if err != nil {
		t.Fatal(err)
	}
	ensure(t, get(t, s, r.ID).Status == "idea", "new task in %q", get(t, s, r.ID).Status)
	_, err = s.Take(r.ID, core.Take, "agent:a")
	wantRule(t, "take without criteria", err, "tix move "+r.ID+" todo2")
	r, err = s.Edit(r.ID, r.Rev, critBody, "tester")
	if err != nil {
		t.Fatal(err)
	}
	mustMove(t, s, r.ID, "todo2", core.MoveOpts{})
	tr, err := s.Take(r.ID, core.Take, "agent:a")
	ensure(t, err == nil && strings.HasPrefix(tr.Info, "todo2 [open] > doing [in_progress]  @"), "take info %q %v", tr.Info, err)
	_, err = s.Take(r.ID, core.Release, "agent:a")
	ensure(t, err == nil && get(t, s, r.ID).Status == "todo2", "release: %v status %q", err, get(t, s, r.ID).Status)
	mustMove(t, s, r.ID, "blocked", core.MoveOpts{Message: "waits for X"})
	mustMove(t, s, r.ID, "parked", core.MoveOpts{})
	ensure(t, get(t, s, r.ID).Waiting == "waits for X", "waiting %q lost on a move inside the kind", get(t, s, r.ID).Waiting)
	mustMove(t, s, r.ID, "todo2", core.MoveOpts{})
	if _, err := s.Close(r.ID, core.MoveOpts{Message: "all done", Force: true}, "tester"); err != nil {
		t.Fatal(err)
	}
	ensure(t, get(t, s, r.ID).Status == "shipped", "close went to %q", get(t, s, r.ID).Status)
	mustMove(t, s, r.ID, "wontfix", core.MoveOpts{})
	ensure(t, strings.Contains(e.text(r.ID), "Result: all done"), "result lost on a move inside the kind")
}
