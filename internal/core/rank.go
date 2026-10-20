package core

import (
	"fmt"
	"os"
	"sort"

	"tracker/internal/task"
)

// minRankGap - зазор между соседними рангами, меньше которого колонка перенумеруется.
const minRankGap = 1e-6

// placement - итог place: ранг задачи и, при тесном зазоре, новые ранги соседей.
type placement struct {
	rank       float64
	keep       bool // задача уже на месте: ранг не меняется
	neighbours []pendingWrite
}

// columnTasks возвращает задачи колонки без skipID по возрастанию ранга. Битые файлы
// на порядок не влияют.
func (s *Store) columnTasks(column, skipID string) []*task.Task {
	var col []*task.Task
	for _, e := range s.rankIndex() {
		if e.status == column && e.id != skipID {
			col = append(col, &task.Task{ID: e.id, Status: e.status, Rank: e.rank, Path: e.path})
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

// place считает ранг задачи t в колонке column: без Place - в конец (в своей колонке
// задача остаётся на месте), First - в начало, After - после соседа.
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

// renumber выдаёт колонке ранги 1..n с задачей t после col[at]; соседи с новым рангом
// уходят в pending (один коммит операции).
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
		data, err := os.ReadFile(c.Path) //nolint:gosec // путь строит ядро
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
