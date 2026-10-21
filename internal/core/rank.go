package core

import (
	"fmt"
	"os"
	"sort"

	"github.com/daniil4545/tix/internal/task"
)

// minRankGap - the gap between neighbor ranks below which the column is renumbered.
const minRankGap = 1e-6

// placement - the result of place: the task rank and, on a tight gap, the new ranks of neighbors.
type placement struct {
	rank       float64
	keep       bool // the task is already in place: the rank does not change
	neighbours []pendingWrite
}

// columnTasks returns the column tasks without skipID in ascending rank order. Broken files
// do not affect the order.
func (s *Store) columnTasks(column, skipID string) []*task.Task {
	all, _ := s.List()
	var col []*task.Task
	for _, t := range all {
		if t.Status == column && t.ID != skipID {
			col = append(col, t)
		}
	}
	sort.SliceStable(col, func(i, j int) bool {
		if col[i].Rank != col[j].Rank {
			return col[i].Rank < col[j].Rank
		}
		return IDNum(col[i].ID) < IDNum(col[j].ID)
	})
	return col
}

// place computes the rank of task t in column column: without Place - to the end (in its own column
// the task stays in place), First - to the start, After - after a neighbor.
func (s *Store) place(t *task.Task, column string, p Place) (placement, error) {
	col := s.columnTasks(column, t.ID)
	same := t.Status == column
	switch {
	case p.First:
		if len(col) == 0 {
			return placement{rank: 1, keep: same}, nil
		}
		if same && t.Rank < col[0].Rank {
			return placement{keep: true}, nil
		}
		return placement{rank: col[0].Rank - 1}, nil
	case p.After != "":
		return s.placeAfter(t, col, column, same, p.After)
	}
	if same {
		return placement{keep: true}, nil
	}
	if len(col) == 0 {
		return placement{rank: 1}, nil
	}
	return placement{rank: col[len(col)-1].Rank + 1}, nil
}

func (s *Store) placeAfter(t *task.Task, col []*task.Task, column string, same bool, after string) (placement, error) {
	i := -1
	for k, c := range col {
		if c.ID == after {
			i = k
			break
		}
	}
	if i < 0 {
		return placement{}, rule("%s is not in column %s", after, column)
	}
	lo := col[i].Rank
	if i+1 == len(col) {
		return placement{rank: lo + 1, keep: same && t.Rank > lo}, nil
	}
	hi := col[i+1].Rank
	if same && t.Rank > lo && t.Rank < hi {
		return placement{keep: true}, nil
	}
	if hi-lo >= minRankGap {
		return placement{rank: lo + (hi-lo)/2}, nil
	}
	return s.renumber(t, col, i)
}

// renumber gives the column ranks 1..n with task t after col[at]; neighbors with a new rank
// go to pending (one operation commit).
func (s *Store) renumber(t *task.Task, col []*task.Task, at int) (placement, error) {
	out := placement{}
	n := 0
	assign := func(c *task.Task) error {
		n++
		if c == t {
			out.rank = float64(n)
			return nil
		}
		if c.Rank == float64(n) {
			return nil
		}
		data, err := os.ReadFile(c.Path) //nolint:gosec // the path is built by the core
		if err != nil {
			return fmt.Errorf("read %s: %w", c.Path, err)
		}
		nt, err := task.Parse(c.Path, data)
		if err != nil {
			return err
		}
		nt.Rank = float64(n)
		out.neighbours = append(out.neighbours, pendingWrite{path: c.Path, old: data, new: nt.Render()})
		return nil
	}
	for k, c := range col {
		if err := assign(c); err != nil {
			return placement{}, err
		}
		if k == at {
			if err := assign(t); err != nil {
				return placement{}, err
			}
		}
	}
	return out, nil
}
