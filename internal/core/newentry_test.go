package core_test

import (
	"testing"
	"time"
)

// spec: T-82 scenario 5 (the core refuses before any write)
func TestStore_Create_CheckedColumn_RuleErrorNothingWritten(t *testing.T) {
	defer guard(t)
	for _, column := range []string{"todo", "blocked", "done"} {
		t.Run(column, func(t *testing.T) {
			e := newEnv(t)
			s := e.open(2 * time.Second)
			if _, err := s.Create("Seed", "proj", "idea", "tester"); err != nil {
				t.Fatal(err)
			}
			commits := e.count()
			_, err := s.Create("Fresh", "proj", column, "tester")
			wantRule(t, "Create into "+column, err, "a new task has no")
			ensure(t, e.count() == commits, "the refused Create added a commit")
			tasks, _ := s.List()
			ensure(t, len(tasks) == 1, "the refused Create left %d tasks, want 1", len(tasks))
		})
	}
}

func TestStore_Create_UncheckedColumns_Allowed(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	for _, column := range []string{"idea", "doing"} {
		if _, err := s.Create("Task "+column, "proj", column, "tester"); err != nil {
			t.Errorf("Create into %s: %v", column, err)
		}
	}
}
