// Package history собирает ленту карточки задачи: коммиты git и комментарии из файла.
package history

import (
	"errors"
	"time"

	"tracker/internal/gitx"
	"tracker/internal/task"
)

// Entry - строка ленты. Folded - свёрнутая серия правок ("N edits"); Op comment - из файла.
type Entry struct {
	At              time.Time
	Actor, Op, Text string
	Folded          []Entry
}

// Load: Log без коммитов comment, плюс t.Comments, по времени, Fold.
// Заглушка зоны 0, логика - зона H.
func Load(r gitx.Repo, t *task.Task) ([]Entry, error) {
	return nil, errors.New("not implemented")
}

// Fold сворачивает серии мелких правок одного Actor от двух подряд.
// Заглушка зоны 0, логика - зона H.
func Fold(es []Entry) []Entry {
	return nil
}
