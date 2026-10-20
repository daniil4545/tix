// Package core - единственный путь записи в ~/tasks: лок, временный файл, rename, коммит.
package core

import (
	"errors"
	"log/slog"
	"time"

	"tracker/internal/config"
	"tracker/internal/task"
)

// Ошибки ядра. Сравнивать через errors.Is.
var (
	ErrConflict = errors.New("конфликт правок")
	ErrLocked   = errors.New("база занята")
	ErrNotFound = errors.New("задача не найдена")
)

// Options - настройки Open.
type Options struct {
	LockTimeout time.Duration
	GitBin      string
	Log         *slog.Logger
}

// Result - ответ операции записи. Warning: коммит упал, правка в файле.
type Result struct{ ID, Rev, Commit, Warning string }

// Store - открытая база задач. Зона C добавляет поля.
type Store struct {
	cfg *config.Config
}

// Open открывает базу в root и читает конфиг.
func Open(root string, o Options) (*Store, error) {
	panic("not implemented")
}

// Config возвращает конфиг базы.
func (s *Store) Config() *config.Config { return s.cfg }

// Create создаёт задачу в колонке project/column от имени actor.
func (s *Store) Create(title, project, column, actor string) (Result, error) {
	panic("not implemented")
}

// Edit заменяет тело; baseRev - версия, которую читал вызывающий.
func (s *Store) Edit(id, baseRev, body, actor string) (Result, error) {
	panic("not implemented")
}

// Comment дописывает комментарий.
func (s *Store) Comment(id, text, actor string) (Result, error) {
	panic("not implemented")
}

// Note пишет пустой коммит T-N: note <текст>.
func (s *Store) Note(id, text, actor string) (Result, error) {
	panic("not implemented")
}

// Remove удаляет задачу.
func (s *Store) Remove(id, actor string) (Result, error) {
	panic("not implemented")
}

// Get читает задачу и её rev, пишет blob в git (hash-object -w). Отказ git - warn.
func (s *Store) Get(id string) (*task.Task, string, error) {
	panic("not implemented")
}

// List читает все задачи без git. Битые файлы - в списке ошибок.
func (s *Store) List() ([]*task.Task, []error) {
	panic("not implemented")
}
