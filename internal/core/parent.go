package core

import (
	"bytes"
	"os"
	"slices"
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
// One pass over the task files; a byte prefilter skips the parse of files without "parent:" and the id.
func (s *Store) Children(id string) ([]*task.Task, int) {
	refs, err := s.taskFiles()
	if err != nil {
		s.log.Warn("children not read", "op", "children", "id", id, "err", err)
		return nil, 0
	}
	found := make([]*task.Task, len(refs))
	forEachChunked(len(refs), func(i int) { found[i] = s.childOf(refs[i], id) })
	var kids []*task.Task
	closed := 0
	for _, o := range found {
		if o != nil {
			kids = append(kids, o)
			if s.kindOf(o.Status) == config.Closed {
				closed++
			}
		}
	}
	sort.SliceStable(kids, func(i, j int) bool { return IDNum(kids[i].ID) < IDNum(kids[j].ID) })
	return kids, closed
}

// childOf returns the task in file r if its parent is id, otherwise nil.
func (s *Store) childOf(r taskRef, id string) *task.Task {
	if r.id == id {
		return nil
	}
	data, err := os.ReadFile(r.path) //nolint:gosec // the path is built by the core
	if err != nil {
		s.log.Warn("child not read", "op", "children", "id", r.id, "err", err)
		return nil
	}
	if !bytes.Contains(data, []byte("parent:")) || !bytes.Contains(data, []byte(id)) {
		return nil
	}
	t, err := task.Parse(r.path, data)
	if err != nil {
		s.log.Warn("child not read", "op", "children", "id", r.id, "err", err)
		return nil
	}
	if t.Parent != id {
		return nil
	}
	return t
}

// HasTask reports whether a task file with this id exists.
func (s *Store) HasTask(id string) bool {
	_, err := s.findPath(strings.TrimSpace(id))
	return err == nil
}

// openChildrenOf returns the ids of the children of id that are not of the kind closed.
func (s *Store) openChildrenOf(id string) []string {
	kids, _ := s.Children(id)
	var open []string
	for _, k := range kids {
		if s.kindOf(k.Status) != config.Closed {
			open = append(open, k.ID)
		}
	}
	return open
}

// checkCloseEpic is the check before closing t: a child that is not closed refuses it, --force included.
func (s *Store) checkCloseEpic(t *task.Task) error {
	if open := s.openChildrenOf(t.ID); len(open) > 0 {
		return rule("%s has open children: %s", t.ID, strings.Join(open, ", "))
	}
	return nil
}

// childMoved adds to ready the parent of child t that has no open child left after t is closed, and returns
// the warning for a child reopened under a closed parent.
func (s *Store) childMoved(t *task.Task, nowClosed bool, ready []string) (_, notices []string) {
	if t.Parent == "" {
		return ready, nil
	}
	path, err := s.findPath(t.Parent)
	if err != nil {
		return ready, nil
	}
	p, err := readTask(path)
	if err != nil {
		return ready, nil
	}
	closedParent := s.kindOf(p.Status) == config.Closed
	switch {
	case !nowClosed && closedParent:
		notices = append(notices, "warning: parent "+p.ID+" is closed")
	case nowClosed && !closedParent && !slices.Contains(ready, p.ID) && !slices.ContainsFunc(s.openChildrenOf(p.ID), func(c string) bool { return c != t.ID }):
		ready = append(ready, p.ID)
	}
	return ready, notices
}
