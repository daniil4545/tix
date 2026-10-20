// Команда tk - CLI трекера.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"tracker/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Процесс короткий, куча на 1000 задач единицы МБ: реже GC - list 18 -> 14 мс (замер).
	debug.SetGCPercent(400)
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "err working directory unavailable:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Run(ctx, os.Args[1:], cli.Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
		Cwd:    cwd,
	})
}
