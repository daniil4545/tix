package core

import (
	"bytes"
	"os"
	"slices"

	"github.com/daniil4545/tix/internal/task"
)

// epicFile - an epic that lists a task: the bytes it was read from and the parsed card.
type epicFile struct {
	t    *task.Task
	data []byte
}

// epicsOf finds the epics (tag epic) that have a line for task id. Unreadable files are skipped.
func (s *Store) epicsOf(id string) []epicFile {
	refs, err := s.taskFiles()
	if err != nil {
		return nil
	}
	var out []epicFile
	for _, r := range refs {
		if r.id == id {
			continue
		}
		data, err := os.ReadFile(r.path) //nolint:gosec // the path is built by the core
		if err != nil || !bytes.Contains(data, []byte(id)) {
			continue
		}
		t, err := task.Parse(r.path, data)
		if err != nil || !t.IsEpic() {
			continue
		}
		if slices.ContainsFunc(t.Criteria(), func(c task.Criterion) bool { return task.LineTask(c.Text) == id }) {
			out = append(out, epicFile{t: t, data: data})
		}
	}
	return out
}

// EpicsOf returns the ids of the epics whose todo list has a line for task id.
func (s *Store) EpicsOf(id string) []string {
	var ids []string
	for _, e := range s.epicsOf(id) {
		ids = append(ids, e.t.ID)
	}
	return ids
}

// syncEpics follows the status of task id in its epics: a closed task has its line checked, any other unchecked.
// The edits go into the commit of the move; an epic changed since the read keeps its old line.
func (s *Store) syncEpics(tx *txEnv, id string, closed bool) {
	for _, e := range s.epicsOf(id) {
		if e.t.SetLine(id, closed) {
			tx.pending = append(tx.pending, pendingWrite{path: e.t.Path, old: e.data, new: e.t.Render()})
		}
	}
}
