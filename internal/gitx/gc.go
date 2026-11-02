package gitx

import (
	"context"
	"os/exec"
	"path/filepath"
	"syscall"
)

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
