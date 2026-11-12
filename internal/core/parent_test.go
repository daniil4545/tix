package core_test

import (
	"os"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/core"
)

func setParent(s *core.Store, id, parent string) error {
	_, err := s.Set(id, []core.Field{{Name: "parent", Value: parent}}, "x:1")
	return err
}

func TestS16_SetParent_Refusals(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	p, c, other := mkIn(t, s, "Parent", "idea"), mkIn(t, s, "Child", "idea"), mkIn(t, s, "Other", "idea")
	closed := mkIn(t, s, "Closed", "closed")
	if err := setParent(s, c, p); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	if got := get(t, s, c).Parent; got != p {
		t.Fatalf("parent = %q, want %s", got, p)
	}
	n := e.count()
	wantRule(t, "missing", setParent(s, other, "T-999"), "not found")
	wantRule(t, "itself", setParent(s, other, other), "own parent")
	wantRule(t, "parent has a parent", setParent(s, other, c), "already has a parent")
	wantRule(t, "task has children", setParent(s, p, other), "has children")
	wantRule(t, "closed parent", setParent(s, other, closed), "closed")
	wantRule(t, "bad id", setParent(s, other, "x"), "T-N")
	if e.count() != n {
		t.Error("a refusal wrote a commit")
	}
	if _, err := s.CreateUnder("T-999", "New", "proj", "idea", "", "x:1"); err == nil {
		t.Error("new under a missing parent accepted")
	}
	_, err := s.CreateUnder(closed, "New", "proj", "idea", "", "x:1")
	wantRule(t, "new under closed", err, "closed")
	_, err = s.CreateUnder(c, "New", "proj", "idea", "", "x:1")
	wantRule(t, "new under a child", err, "already has a parent")
	if e.count() != n {
		t.Error("a refused new wrote a commit")
	}
	if err := setParent(s, c, ""); err != nil {
		t.Fatalf("clear parent: %v", err)
	}
	if got := get(t, s, c).Parent; got != "" || strings.Contains(e.text(c), "parent") {
		t.Errorf("parent not cleared: %q\n%s", got, e.text(c))
	}
}

func TestS17_Children_CountAndOrder(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	p := mkIn(t, s, "Parent", "idea")
	var kids []string
	for _, title := range []string{"A", "B", "C"} {
		r, err := s.CreateUnder(p, title, "proj", "idea", "", "x:1")
		if err != nil {
			t.Fatal(err)
		}
		kids = append(kids, r.ID)
	}
	if _, err := s.Move(kids[1], "closed", core.MoveOpts{Message: "done", Force: true}, "x:1"); err != nil {
		t.Fatal(err)
	}
	got, closed := s.Children(p)
	if len(got) != 3 || closed != 1 {
		t.Fatalf("Children = %d, closed %d, want 3 and 1", len(got), closed)
	}
	for i, k := range got {
		if k.ID != kids[i] {
			t.Errorf("child %d = %s, want %s", i, k.ID, kids[i])
		}
	}
	if none, _ := s.Children(kids[0]); len(none) != 0 {
		t.Errorf("a child has children: %v", none)
	}
}

// S28: hand-edited parents are read as written and never followed deeper.
func TestS28_ManualParent_NothingFails(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	a, b := mkIn(t, s, "A", "idea"), mkIn(t, s, "B", "idea")
	for id, parent := range map[string]string{a: a, b: "T-999"} {
		d := strings.Replace(e.text(id), "\nstatus:", "\nparent: "+parent+"\nstatus:", 1)
		if err := os.WriteFile(e.file(id), []byte(d), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := get(t, s, a).Parent; got != a {
		t.Errorf("parent of a = %q", got)
	}
	if kids, _ := s.Children(a); len(kids) != 0 {
		t.Errorf("a task is its own child: %v", kids)
	}
	if kids, _ := s.Children("T-999"); len(kids) != 1 || kids[0].ID != b {
		t.Errorf("children of a missing task: %v", kids)
	}
	if s.HasTask("T-999") {
		t.Error("HasTask of a missing task")
	}
	wantRule(t, "set parent on a self-parent", setParent(s, a, a), "own parent")
}

// Children sorts by number, whatever the order of creation under the parent.
func TestS17_Children_OrderByNumber(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.openAt(newClock())
	a, b, c := mkIn(t, s, "A", "idea"), mkIn(t, s, "B", "idea"), mkIn(t, s, "C", "idea")
	p := mkIn(t, s, "Parent", "idea")
	for _, id := range []string{c, a, b} {
		if err := setParent(s, id, p); err != nil {
			t.Fatal(err)
		}
	}
	kids, _ := s.Children(p)
	if len(kids) != 3 || kids[0].ID != a || kids[1].ID != b || kids[2].ID != c {
		t.Errorf("children order %v, want %s %s %s", kids, a, b, c)
	}
}
