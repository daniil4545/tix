// Package cli разбирает команды tk и печатает ответ.
package cli

import (
	"context"
	"io"
)

// Env - окружение запуска: потоки, переменные, рабочая папка.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Cwd            string
}

// Run выполняет команду и возвращает код выхода. Ловит панику.
func Run(ctx context.Context, args []string, env Env) int {
	panic("not implemented")
}
