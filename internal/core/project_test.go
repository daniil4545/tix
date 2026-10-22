package core_test

import (
	"testing"
	"time"
)

// spec: T-73 scenario 6
func TestStore_Create_ProjectStoredLowerCase(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	r, err := s.Create("Alpha", "  Galera ", "idea", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if got := get(t, s, r.ID).Project; got != "galera" {
		t.Errorf("project %q, want galera", got)
	}
}
