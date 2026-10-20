// Package tui - интерактивная доска: модель Bubble Tea, очередь записи, наблюдатель файлов.
package tui

import (
	"context"
	"errors"
	"log/slog"

	_ "charm.land/bubbletea/v2"      // модель зоны M; импорт держит зависимость в go.mod
	_ "github.com/fsnotify/fsnotify" // наблюдатель зоны M

	"tracker/internal/core"
)

// Deps - что TUI получает снаружи. Project - начальный фильтр (папка проекта), пусто - все.
type Deps struct {
	Store                *core.Store
	Root, Actor, Project string
	Log                  *slog.Logger
}

// Run открывает TUI на альтернативном экране; паника уходит ошибкой в Log.
// Заглушка зоны 0, логика - зона M.
func Run(ctx context.Context, d Deps) error {
	return errors.New("not implemented")
}
