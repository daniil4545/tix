package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const pushTimeout = 30 * time.Second

// userinfoRe - всё от "scheme://" до последнего "@" токена: userinfo с "/" и "@" в пароле не оставляет хвоста.
var userinfoRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)\S*@`)

func scrub(s string) string { return userinfoRe.ReplaceAllString(s, "${1}") }

// Remotes возвращает имена remote.
func (r Repo) Remotes() ([]string, error) {
	out, err := r.must(nil, true, "remote")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// Push отправляет HEAD в remote: без --force, без запросов пароля, с таймаутом.
// Зовётся без лока ядра: сеть не держит запись.
func (r Repo) Push(ctx context.Context, remote string) error {
	if remote == "" || strings.HasPrefix(remote, "-") {
		return &Error{Op: "push", Stderr: fmt.Sprintf("bad remote name %q", remote)}
	}
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	cmd := r.command(ctx, false, "push", "--quiet", remote, "HEAD")
	cmd.Env = append(cmd.Env, r.sshEnv()...)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err == nil {
		return nil
	}
	e := &Error{Op: "push", Stderr: scrub(errOut.String())}
	var ee *exec.ExitError
	switch {
	case ctx.Err() != nil:
		e.Stderr = fmt.Sprintf("%s: %v", ctx.Err(), strings.TrimSpace(e.Stderr))
	case errors.As(err, &ee):
		e.Code = ee.ExitCode()
	default:
		e.Stderr = scrub(err.Error())
	}
	return e
}

// sshEnv добавляет BatchMode, если ssh не настроен ни окружением, ни git config:
// ssh без него спросит пароль или фразу ключа у терминала.
func (r Repo) sshEnv() []string {
	if os.Getenv("GIT_SSH_COMMAND") != "" || os.Getenv("GIT_SSH") != "" {
		return nil
	}
	if out, err := r.must(nil, true, "config", "--get", "core.sshCommand"); err == nil && len(bytes.TrimSpace(out)) > 0 {
		return nil
	}
	return []string{"GIT_SSH_COMMAND=ssh -o BatchMode=yes"}
}

// gcScript идёт в sh -c: $1 - git, $2 - файл лога. Сбой шага - строка в лог, ход дальше.
const gcScript = `note() { printf 'time=%s level=WARN msg="background git failed" step=%s code=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2" >>"$LOG"; }
LOG=$2
"$1" gc --quiet 2>>"$LOG" || note gc $?
"$1" commit-graph write --reachable --changed-paths --split 2>>"$LOG" || note commit-graph $?`

// SpawnGC запускает в фоне `git gc --quiet` и `commit-graph write`; не ждёт завершения.
// Сбой запуска - в ошибке, сбой самих команд - строка в .tix/log.
func (r Repo) SpawnGC() error {
	bin := r.Bin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(context.Background(), "sh", "-c", gcScript, "sh", bin, filepath.Join(r.Dir, ".tix", "log")) //nolint:gosec // bin задаёт Resolve или TIX_GIT
	cmd.Dir = r.Dir
	cmd.Env = r.env()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // переживает выход tix
	if err := cmd.Start(); err != nil {
		return &Error{Op: "gc", Stderr: err.Error()}
	}
	go func() { _ = cmd.Wait() }() // забирает код выхода: в долгом процессе нет зомби
	return nil
}
