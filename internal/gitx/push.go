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

// userinfoRe - everything from "scheme://" to the last "@" of the token: userinfo with "/" and "@" in the password leaves no tail.
var userinfoRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)\S*@`)

func scrub(s string) string { return userinfoRe.ReplaceAllString(s, "${1}") }

// Remotes returns the remote names.
func (r Repo) Remotes() ([]string, error) {
	out, err := r.must(nil, true, "remote")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// Push sends HEAD to a remote: no --force, no password prompts, with a timeout.
// Called without the core lock: the network does not hold up a write.
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

// sshEnv adds BatchMode if ssh is configured neither by the environment nor by git config:
// without it ssh asks the terminal for a password or a key passphrase.
func (r Repo) sshEnv() []string {
	if os.Getenv("GIT_SSH_COMMAND") != "" || os.Getenv("GIT_SSH") != "" {
		return nil
	}
	if out, err := r.must(nil, true, "config", "--get", "core.sshCommand"); err == nil && len(bytes.TrimSpace(out)) > 0 {
		return nil
	}
	return []string{"GIT_SSH_COMMAND=ssh -o BatchMode=yes"}
}

// gcScript goes to sh -c: $1 - git, $2 - the log file. A failed step is a line in the log, the run goes on.
const gcScript = `note() { printf 'time=%s level=WARN msg="background git failed" step=%s code=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2" >>"$LOG"; }
LOG=$2
"$1" gc --quiet 2>>"$LOG" || note gc $?
"$1" commit-graph write --reachable --changed-paths --split 2>>"$LOG" || note commit-graph $?`

// SpawnGC starts `git gc --quiet` and `commit-graph write` in the background; it does not wait for completion.
// A start failure is in the error, a failure of the commands themselves is a line in .tix/log.
func (r Repo) SpawnGC() error {
	bin := r.Bin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(context.Background(), "sh", "-c", gcScript, "sh", bin, filepath.Join(r.Dir, ".tix", "log")) //nolint:gosec // bin comes from Resolve or TIX_GIT
	cmd.Dir = r.Dir
	cmd.Env = r.env()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survives tix exit
	if err := cmd.Start(); err != nil {
		return &Error{Op: "gc", Stderr: err.Error()}
	}
	go func() { _ = cmd.Wait() }() // collects the exit code: a long process has no zombies
	return nil
}
