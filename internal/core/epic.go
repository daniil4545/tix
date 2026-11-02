package core

import (
	"bytes"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/task"
)

// epicFile - an epic that lists a task: the bytes it was read from and the parsed card.
type epicFile struct {
	t    *task.Task
	data []byte
}

// epicsOf finds the epics (tag epic) that have a line for task id. Unreadable files are skipped.
// The files are read in parallel chunks like List does; a byte prefilter skips the parse.
func (s *Store) epicsOf(id string) []epicFile {
	refs, err := s.taskFiles()
	if err != nil {
		return nil
	}
	found := make([]*epicFile, len(refs))
	workers := min(listWorkers, runtime.NumCPU(), max(len(refs), 1))
	chunk := (len(refs) + workers - 1) / workers
	var wg sync.WaitGroup
	for from := 0; from < len(refs); from += chunk {
		wg.Add(1)
		go func(from, to int) {
			defer wg.Done()
			for i := from; i < to; i++ {
				found[i] = s.epicFileOf(refs[i], id)
			}
		}(from, min(from+chunk, len(refs)))
	}
	wg.Wait()
	var out []epicFile
	for _, e := range found {
		if e != nil {
			out = append(out, *e)
		}
	}
	return out
}

// epicFileOf returns the epic in file r if it has a line for task id, otherwise nil.
func (s *Store) epicFileOf(r taskRef, id string) *epicFile {
	if r.id == id {
		return nil
	}
	data, err := os.ReadFile(r.path) //nolint:gosec // the path is built by the core
	if err != nil || !bytes.Contains(data, []byte(id)) {
		return nil
	}
	t, err := task.Parse(r.path, data)
	if err != nil {
		s.log.Warn("epic not read", "op", "epic", "id", r.id, "err", err)
		return nil
	}
	if !t.IsEpic() || !slices.ContainsFunc(t.Criteria(), func(c task.Criterion) bool { return task.LineTask(c.Text) == id }) {
		return nil
	}
	return &epicFile{t: t, data: data}
}

// EpicsOf returns the ids of the epics whose todo list has a line for task id.
func (s *Store) EpicsOf(id string) []string {
	var ids []string
	for _, e := range s.epicsOf(id) {
		ids = append(ids, e.t.ID)
	}
	return ids
}

// syncEpics ticks the lines of task id in its epics on close, unticks them otherwise, in the move commit.
// It returns the epics whose task lines are all ticked now.
func (s *Store) syncEpics(tx *txEnv, id string, closed bool) (ready []string) {
	for _, e := range s.epicsOf(id) {
		if !e.t.SetLine(id, closed) {
			continue
		}
		if slices.ContainsFunc(tx.pending, func(p pendingWrite) bool { return p.path == e.t.Path }) {
			continue
		}
		tx.pending = append(tx.pending, pendingWrite{path: e.t.Path, old: e.data, new: e.t.Render(),
			warn: "epic " + e.t.ID + ": line of " + id + " not updated"})
		if closed && !slices.ContainsFunc(e.t.Criteria(), func(c task.Criterion) bool {
			return !c.Done && task.LineTask(c.Text) != ""
		}) {
			ready = append(ready, e.t.ID)
		}
	}
	return ready
}

// settleLines is the check before closing an epic: the status of the tasks is the truth. A line of a closed task
// is ticked in the epic's own write; an open or missing task refuses the close, --force included.
func (s *Store) settleLines(t *task.Task) error {
	var bad []string
	for _, c := range t.Criteria() {
		id := task.LineTask(c.Text)
		if c.Done || id == "" {
			continue
		}
		path, err := s.findPath(id)
		if err != nil {
			bad = append(bad, id+" (not found)")
			continue
		}
		line, err := readTask(path)
		if err != nil || line.Status != config.Closed {
			bad = append(bad, id+" (not closed)")
			continue
		}
		t.SetLine(id, true)
	}
	if len(bad) > 0 {
		return rule("epic lines of tasks that are not closed: %s", strings.Join(bad, ", "))
	}
	return nil
}
