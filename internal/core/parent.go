package core

import (
	"sort"
	"strings"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/task"
)

// checkParent applies the refusals of setting task t under parent; "" clears and is always allowed.
// t.ID is empty for a task that is not created yet.
func (s *Store) checkParent(t *task.Task, parent string) error {
	if parent == "" {
		return nil
	}
	if !idRe.MatchString(parent) {
		return rule("parent must look like T-N, got %q", parent)
	}
	if parent == t.ID {
		return rule("a task cannot be its own parent")
	}
	tasks, _ := s.List() // broken files do not affect the check
	var p *task.Task
	for _, o := range tasks {
		switch {
		case o.ID == parent:
			p = o
		case t.ID != "" && o.Parent == t.ID:
			return rule("%s has children (%s): clear or move them first, the depth is one", t.ID, o.ID)
		}
	}
	switch {
	case p == nil:
		return rule("parent %s not found", parent)
	case p.Parent != "":
		return rule("%s already has a parent (%s): the depth is one", parent, p.Parent)
	case s.kindOf(p.Status) == config.Closed:
		return rule("parent %s is closed: reopen it first", parent)
	}
	return nil
}

// Children returns the tasks whose parent is id, by number, and how many of them are of the kind closed.
// One pass over the task list.
func (s *Store) Children(id string) ([]*task.Task, int) {
	tasks, _ := s.List()
	var kids []*task.Task
	closed := 0
	for _, o := range tasks {
		if o.Parent == id && o.ID != id {
			kids = append(kids, o)
			if s.kindOf(o.Status) == config.Closed {
				closed++
			}
		}
	}
	sort.SliceStable(kids, func(i, j int) bool { return IDNum(kids[i].ID) < IDNum(kids[j].ID) })
	return kids, closed
}

// HasTask reports whether a task file with this id exists.
func (s *Store) HasTask(id string) bool {
	_, err := s.findPath(strings.TrimSpace(id))
	return err == nil
}
