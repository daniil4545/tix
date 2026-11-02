package core

import (
	"bytes"
	"os"
	"slices"
	"strings"

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
	forEachChunked(len(refs), func(i int) { found[i] = s.epicFileOf(refs[i], id) })
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
		card, i := s.pendingCard(tx, e)
		if card == nil || !card.SetLine(id, closed) {
			continue
		}
		w := pendingWrite{path: card.Path, old: e.data, new: card.Render(),
			warn: "epic " + card.ID + ": line of " + id + " not updated"}
		if i >= 0 {
			// the old bytes of the renumber read: a foreign edit since then must refuse this write
			w.old = tx.pending[i].old
			tx.pending[i] = w
		} else {
			tx.pending = append(tx.pending, w)
		}
		if closed && !slices.ContainsFunc(card.Criteria(), func(c task.Criterion) bool {
			return !c.Done && task.LineTask(c.Text) != ""
		}) {
			ready = append(ready, card.ID)
		}
	}
	return ready
}

// pendingCard returns the epic card to edit and its index in tx.pending, -1 if absent. An epic already pending
// as a renumbered rank neighbour is edited in that write, otherwise its new rank or the line is lost.
func (s *Store) pendingCard(tx *txEnv, e epicFile) (*task.Task, int) {
	i := slices.IndexFunc(tx.pending, func(p pendingWrite) bool { return p.path == e.t.Path })
	if i < 0 {
		return e.t, -1
	}
	t, err := task.Parse(e.t.Path, tx.pending[i].new)
	if err != nil {
		s.log.Warn("epic not read", "op", "epic", "id", e.t.ID, "err", err)
		return nil, i
	}
	return t, i
}

// settleLines is the check before closing an epic: the status of the tasks is the truth, a ticked line included.
// A line of a closed task is ticked in the epic's own write; an open or missing task refuses the close, --force included.
func (s *Store) settleLines(t *task.Task) error {
	var bad []string
	for _, c := range t.Criteria() {
		id := task.LineTask(c.Text)
		if id == "" {
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
