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
	"tracker/internal/tui"
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
	env := cli.Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
		Cwd:    cwd,
	}
	if len(os.Args) == 1 && isTerminal(os.Stdout) {
		return runTUI(ctx, env)
	}
	return cli.Run(ctx, os.Args[1:], env)
}

// isTerminal: stdout - терминал, а не пайп или файл.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// runTUI открывает доску от имени человека (spec 2.4). Deps.Root заполняет зона M:
// OpenStore по плану корень не возвращает.
func runTUI(ctx context.Context, env cli.Env) int {
	store, log, proj, err := cli.OpenStore(env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "err", err)
		return 1
	}
	if err := tui.Run(ctx, tui.Deps{Store: store, Actor: "human", Project: proj, Log: log}); err != nil {
		fmt.Fprintln(os.Stderr, "err", err)
		return 1
	}
	return 0
}
