package gitx

import (
	"errors"
	"time"
)

// Commit - запись истории одной задачи: хеш, время автора, тема и значение трейлера Actor.
type Commit struct {
	Hash    string
	At      time.Time
	Subject string
	Actor   string
}

// Log возвращает коммиты задачи id от новых к старым; нет коммитов - пусто.
// Заглушка зоны 0, логика - зона H.
func (r Repo) Log(id string) ([]Commit, error) {
	return nil, errors.New("not implemented")
}
