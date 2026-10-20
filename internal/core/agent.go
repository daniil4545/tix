package core

import (
	"context"
	"errors"
)

var errNotImplemented = errors.New("not implemented")

// Ошибки рабочего цикла агента.
var (
	ErrClaimed    = errors.New("claimed by another actor")
	ErrNoIdentity = errors.New("no identity")
	ErrRule       = errors.New("rule violated")
	ErrNoRemote   = errors.New("no remote")
)

// TakeMode - режим Take.
type TakeMode int

// Режимы Take.
const (
	Take TakeMode = iota
	Steal
	Release
)

// Place - позиция в колонке: First - в начало, After - после задачи; пусто - в конец.
type Place struct {
	First bool
	After string
}

// MoveOpts - параметры Move. Message - значение -m, Force - закрыть с неотмеченными.
type MoveOpts struct {
	Place   Place
	Message string
	Force   bool
}

// Field - пара имя-значение для Set.
type Field struct{ Name, Value string }

// Take берёт задачу, отдаёт или перехватывает claim. actor "" или "unknown": ErrNoIdentity.
func (s *Store) Take(id string, mode TakeMode, actor string) (Result, error) {
	return Result{}, errNotImplemented
}

// Move переносит задачу в колонку по правилам ролей.
func (s *Store) Move(id, column string, o MoveOpts, actor string) (Result, error) {
	return Result{}, errNotImplemented
}

// Set меняет поля задачи.
func (s *Store) Set(id string, fields []Field, actor string) (Result, error) {
	return Result{}, errNotImplemented
}

// Check отмечает критерии с номерами nums.
func (s *Store) Check(id string, nums []int, actor string) (Result, error) {
	return Result{}, errNotImplemented
}

// Backup коммитит чужие правки под локом и отправляет HEAD в origin без лока.
func (s *Store) Backup(ctx context.Context) (Result, error) {
	return Result{}, errNotImplemented
}
