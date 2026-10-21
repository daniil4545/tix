// Команда tix - CLI трекера.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/daniil4545/tix/internal/cli"
	"github.com/daniil4545/tix/internal/tui"
)

// version задаётся сборкой: -ldflags "-X main.version=<значение>". Без флага - dev.
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return 0
	}
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
	if len(os.Args) == 1 && (isTerminal(os.Stdout) || env.Getenv("TIX_TUI_ONESHOT") == "1") {
		return runTUI(ctx, env)
	}
	return cli.Run(ctx, os.Args[1:], env)
}

// isTerminal: stdout - терминал, а не пайп или файл.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// runTUI открывает доску от имени человека (spec 2.4).
func runTUI(ctx context.Context, env cli.Env) int {
	store, log, root, proj, err := cli.OpenStore(env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "err", err)
		return 1
	}
	d := tui.Deps{
		Store: store, Root: root, Actor: "human", Project: proj,
		GitBin: env.Getenv("TIX_GIT"), Log: log,
	}
	if err := tui.Run(ctx, d); err != nil {
		log.Error("tui failed", "err", err)
		fmt.Fprintln(os.Stderr, "err", err)
		return 1
	}
	return 0
}
