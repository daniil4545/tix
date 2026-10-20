package cli

import (
	"errors"
	"log/slog"

	"tracker/internal/core"
)

// OpenStore открывает базу для TUI: база, лог, проект рабочей папки (пусто - вне проекта).
// Заглушка зоны 0: зона M выносит сюда тело app.open, и app.open зовёт OpenStore.
func OpenStore(env Env) (*core.Store, *slog.Logger, string, error) {
	return nil, nil, "", errors.New("not implemented")
}
