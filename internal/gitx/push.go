package gitx

import (
	"context"
	"errors"
)

// Remotes возвращает имена remote.
func (r Repo) Remotes() ([]string, error) {
	return nil, errors.New("not implemented")
}

// Push отправляет HEAD в remote: без --force, без запросов пароля, с таймаутом.
func (r Repo) Push(ctx context.Context, remote string) error {
	return errors.New("not implemented")
}

// SpawnGC запускает в фоне `git gc --quiet` и `commit-graph write`; не ждёт завершения.
// Сбой запуска - в ошибке, сбой самих команд - в логе фонового процесса.
func (r Repo) SpawnGC() error {
	return errors.New("not implemented")
}
