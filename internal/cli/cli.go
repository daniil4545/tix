// Package cli разбирает команды tk и печатает ответ.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"tracker/internal/core"
	"tracker/internal/project"
)

// Env - окружение запуска: потоки, переменные, рабочая папка.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Cwd            string
}

const (
	defaultLockTimeout = 5 * time.Second
	personalProject    = "personal"
	usage              = "tk new|list|show|edit|comment|note|rm; flags: -v, --as, --json; text starting with a dash goes after --"
)

var (
	idPattern = regexp.MustCompile(`^T-[0-9]+$`)
	// laterCommands - команды раздела 2.2 спеки, которые делает З2.
	laterCommands = map[string]bool{
		"ctx": true, "take": true, "move": true, "set": true,
		"check": true, "done": true, "backup": true,
	}
)

// idError привязывает ошибку к задаче: stderr `err T-12: причина`.
type idError struct {
	ID  string
	Err error
}

func (e *idError) Error() string { return e.Err.Error() }
func (e *idError) Unwrap() error { return e.Err }

// app - один запуск tk.
type app struct {
	env   Env
	cmd   string
	log   *slog.Logger
	store *core.Store
	root  string
}

// Run выполняет команду и возвращает код выхода. Ловит панику.
func Run(ctx context.Context, args []string, env Env) (code int) {
	a := &app{env: env, log: slog.New(slog.DiscardHandler)}
	defer func() {
		if r := recover(); r != nil {
			a.fail(fmt.Errorf("internal error: %v", r))
			code = 1
		}
	}()
	if err := a.dispatch(ctx, args); err != nil {
		a.fail(err)
		return 1
	}
	return 0
}

func (a *app) dispatch(ctx context.Context, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("command required: " + usage)
	}
	a.cmd, args = args[0], args[1:]
	switch a.cmd {
	case "-h", "--help", "help":
		a.out("%s", usage)
		return nil
	case "new":
		return a.cmdNew(args)
	case "list":
		return a.cmdList(args)
	case "show":
		return a.cmdShow(args)
	case "edit":
		return a.cmdEdit(args)
	case "comment", "note":
		return a.cmdText(args)
	case "rm":
		return a.cmdRemove(args)
	}
	if laterCommands[a.cmd] {
		return fmt.Errorf("command %s is not implemented yet", a.cmd)
	}
	return fmt.Errorf("unknown command %s; %s", a.cmd, usage)
}

// out печатает строку ответа в stdout.
func (a *app) out(format string, v ...any) {
	_, _ = fmt.Fprintf(a.env.Stdout, format+"\n", v...)
}

// warn печатает предупреждение в stderr.
func (a *app) warn(format string, v ...any) {
	_, _ = fmt.Fprintf(a.env.Stderr, "warn "+format+"\n", v...)
}

// fail печатает `err [id: ]причина` без вывода git и пишет в лог.
func (a *app) fail(err error) {
	reason := firstLine(err.Error())
	var ie *idError
	if errors.As(err, &ie) {
		a.log.Error("command failed", "cmd", a.cmd, "id", ie.ID, "err", err)
		_, _ = fmt.Fprintf(a.env.Stderr, "err %s: %s\n", ie.ID, reason)
		return
	}
	a.log.Error("command failed", "cmd", a.cmd, "err", err)
	_, _ = fmt.Fprintf(a.env.Stderr, "err %s\n", reason)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// rootDir: TRACKER_HOME, иначе ~/tasks.
func (a *app) rootDir() (string, error) {
	if dir := a.env.Getenv("TRACKER_HOME"); dir != "" {
		return dir, nil
	}
	home := a.env.Getenv("HOME")
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", fmt.Errorf("home directory unknown, set TRACKER_HOME: %w", err)
		}
	}
	return filepath.Join(home, "tasks"), nil
}

// open открывает базу; лог пишется в .tracker/log, если папка уже есть.
func (a *app) open() error {
	root, err := a.rootDir()
	if err != nil {
		return err
	}
	timeout := defaultLockTimeout
	if raw := a.env.Getenv("TRACKER_LOCK_TIMEOUT"); raw != "" {
		if timeout, err = time.ParseDuration(raw); err != nil || timeout <= 0 {
			return fmt.Errorf("TRACKER_LOCK_TIMEOUT %q: duration like 5s required", raw)
		}
	}
	a.root = root
	a.log = slog.New(slog.NewTextHandler(&lazyLog{dir: filepath.Join(root, ".tracker")}, nil))
	a.store, err = core.Open(root, core.Options{
		LockTimeout: timeout,
		GitBin:      a.env.Getenv("TRACKER_GIT"),
		Log:         a.log,
	})
	return err
}

// detectProject возвращает проект рабочей папки, если она внутри проекта.
func (a *app) detectProject() (string, bool) {
	return project.Detect(a.env.Cwd, a.root, a.store.Config())
}

// taskID проверяет номер задачи.
func taskID(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("task id required, for example T-12")
	}
	if !idPattern.MatchString(args[0]) {
		return "", fmt.Errorf("bad task id %q, expected form T-12", args[0])
	}
	return args[0], nil
}

// lazyLog открывает .tracker/log при первой записи и только если папка есть:
// писать лог не должно создавать базу.
type lazyLog struct {
	dir  string
	file *os.File
}

func (l *lazyLog) Write(p []byte) (int, error) {
	if l.file == nil {
		// Путь задаёт владелец через TRACKER_HOME: обхода границы доверия нет.
		info, err := os.Stat(l.dir) //nolint:gosec
		if err != nil || !info.IsDir() {
			return len(p), nil
		}
		f, err := os.OpenFile(filepath.Join(l.dir, "log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec
		if err != nil {
			return len(p), nil //nolint:nilerr // лог не должен ронять команду
		}
		l.file = f
	}
	return l.file.Write(p)
}
