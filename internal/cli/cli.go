// Package cli parses tix commands and prints the reply.
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

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/project"
)

// Env - the run environment: streams, variables, working folder.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Cwd            string
	Version        string
}

const (
	defaultLockTimeout = 5 * time.Second
	personalProject    = "personal"
	usage              = "tix ctx|inbox|take|move|set|check|done|backup|new|list|show|edit|comment|note|rm; " +
		"flags: -v, --as, --json; text starting with a dash goes after --"
)

// Exit codes.
const (
	exitOther    = 1
	exitBusy     = 2
	exitConflict = 3
	exitNotFound = 4
	exitRule     = 5
	exitInput    = 6
)

var idPattern = regexp.MustCompile(`^T-[0-9]+$`)

// inputError - invalid input: flag, id, argument count. Exit code 6.
type inputError struct{ msg string }

func (e *inputError) Error() string { return e.msg }

func inputf(format string, v ...any) error { return &inputError{msg: fmt.Sprintf(format, v...)} }

// ruleError - a refusal by a rule outside the core. Exit code 5.
type ruleError struct{ msg string }

func (e *ruleError) Error() string        { return e.msg }
func (e *ruleError) Is(target error) bool { return target == core.ErrRule }

// exitCode maps an error to an exit code via errors.Is.
func exitCode(err error) int {
	var in *inputError
	switch {
	case errors.Is(err, core.ErrClaimed), errors.Is(err, core.ErrLocked):
		return exitBusy
	case errors.Is(err, core.ErrConflict):
		return exitConflict
	case errors.Is(err, core.ErrNotFound):
		return exitNotFound
	case errors.Is(err, core.ErrRule):
		return exitRule
	case errors.As(err, &in), errors.Is(err, core.ErrNoIdentity), errors.Is(err, actor.ErrAmbiguous):
		return exitInput
	}
	return exitOther
}

// idError ties an error to a task: stderr `err T-12: reason`.
type idError struct {
	ID  string
	Err error
}

func (e *idError) Error() string { return e.Err.Error() }
func (e *idError) Unwrap() error { return e.Err }

// app - one tix run.
type app struct {
	env   Env
	cmd   string
	log   *slog.Logger
	store *core.Store
	root  string
	who   string // identity of the last write; empty until the command parsed the author
}

// Run executes a command and returns the exit code. Catches panics.
func Run(ctx context.Context, args []string, env Env) (code int) {
	a := &app{env: env, log: slog.New(slog.DiscardHandler)}
	defer func() {
		if r := recover(); r != nil {
			a.fail(fmt.Errorf("internal error: %v", r))
			code = exitOther
		}
	}()
	if err := a.dispatch(ctx, args); err != nil {
		a.fail(err)
		return exitCode(err)
	}
	if writeCmds[a.cmd] {
		a.recordFocus()
	}
	return 0
}

// writeCmds - commands after whose success the agent leaves the address of its tab.
var writeCmds = map[string]bool{
	"new": true, "edit": true, "comment": true, "note": true, "rm": true,
	"take": true, "move": true, "set": true, "check": true, "done": true,
}

func (a *app) dispatch(ctx context.Context, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 {
		return inputf("command required: %s", usage)
	}
	a.cmd, args = args[0], args[1:]
	if h, ok := commandHelp[a.cmd]; ok && wantsHelp(args) {
		a.out("tix %s; flags of every command: -v, --as agent:id", h)
		return nil
	}
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
	case "ctx":
		return a.cmdCtx(args)
	case "inbox":
		return a.cmdInbox(args)
	case "take":
		return a.cmdTake(args)
	case "move":
		return a.cmdMove(args)
	case "set":
		return a.cmdSet(args)
	case "check":
		return a.cmdCheck(args)
	case "done":
		return a.cmdDone(args)
	case "backup":
		return a.cmdBackup(ctx, args)
	case "version":
		a.out("%s", a.env.Version)
		return nil
	}
	if s := suggest(a.cmd); s != "" {
		return inputf("unknown command %s; did you mean %s?", a.cmd, s)
	}
	return inputf("unknown command %s; %s", a.cmd, usage)
}

// usageCmds - the commands of the usage line in usage order.
var usageCmds = []string{
	"ctx", "inbox", "take", "move", "set", "check", "done", "backup",
	"new", "list", "show", "edit", "comment", "note", "rm",
}

// suggest returns the closest command within edit distance 2 of a word,
// the first one in the usage order on a tie.
func suggest(word string) string {
	best, bestDist := "", 3
	for _, c := range usageCmds {
		if d := editDistance(word, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

// editDistance is the Damerau-Levenshtein (optimal string alignment) distance
// of two words: a swap of two adjacent letters counts as one edit.
func editDistance(a, b string) int {
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}

// commandHelp - the usage line of each command for `tix <command> --help`.
var commandHelp = map[string]string{
	"ctx":     "ctx [--project P] [--json] [--hook]",
	"inbox":   "inbox [--hook]",
	"new":     "new <title> [--to column|role] [--project P]",
	"list":    "list [--project P] [--status column] [--json]",
	"show":    "show <id> [-v] [--json]; notes from the history follow the file (last 5, all with -v)",
	"edit":    "edit <id> --base <rev> < body.md",
	"comment": "comment <id> <text>",
	"note":    "note <id> <text>",
	"rm":      "rm <id>",
	"take":    "take <id> [--steal|--release]",
	"move":    "move <id> <column|role> [--first|--after T-9] [-m text] [--force]",
	"set":     "set <id> field=value...; fields: title project waiting due tags ask external abstract summary",
	"check":   "check <id> N...",
	"done":    "done <id> [-m result] [--force]",
	"backup":  "backup",
}

// wantsHelp reports -h or --help among the flags; text after "--" does not count.
func wantsHelp(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--":
			return false
		case "-h", "--help":
			return true
		}
	}
	return false
}

// out prints a reply line to stdout.
func (a *app) out(format string, v ...any) {
	_, _ = fmt.Fprintf(a.env.Stdout, format+"\n", v...)
}

// warn prints a warning to stderr.
func (a *app) warn(format string, v ...any) {
	_, _ = fmt.Fprintf(a.env.Stderr, "warn "+format+"\n", v...)
}

// fail prints `err [id: ]reason` without git output and writes to the log.
func (a *app) fail(err error) {
	reason := firstLine(err.Error())
	var ie *idError
	if errors.As(err, &ie) {
		a.log.Error("command failed", "cmd", a.cmd, "id", ie.ID, "err", gitx.Detail(err))
		_, _ = fmt.Fprintf(a.env.Stderr, "err %s: %s\n", ie.ID, reason)
		return
	}
	a.log.Error("command failed", "cmd", a.cmd, "err", gitx.Detail(err))
	_, _ = fmt.Fprintf(a.env.Stderr, "err %s\n", reason)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// rootDir: TIX_HOME, else ~/tasks.
func (a *app) rootDir() (string, error) { return rootDir(a.env) }

// open opens the store; the log goes to .tix/log if the folder exists.
func (a *app) open() error {
	store, log, root, _, err := OpenStore(a.env)
	if log != nil {
		a.log = log
	}
	a.root = root
	a.store = store
	return err
}

// detectProject returns the project of the working folder if it is inside a project.
func (a *app) detectProject() (string, bool) {
	return project.Detect(a.env.Cwd, a.root, a.store.Config())
}

// normID expands a short id: `12` equals `T-12` (D48).
func normID(s string) (string, error) {
	if s != "" && strings.Trim(s, "0123456789") == "" {
		s = "T-" + s
	}
	if !idPattern.MatchString(s) {
		return "", inputf("bad task id %q, expected form T-12 or 12", s)
	}
	return s, nil
}

// taskID checks the task number in the first argument.
func taskID(args []string) (string, error) {
	if len(args) == 0 {
		return "", inputf("task id required, for example T-12")
	}
	return normID(args[0])
}

// lazyLog opens .tix/log on first write and only if the folder exists:
// writing the log must not create the store.
type lazyLog struct {
	dir  string
	file *os.File
}

func (l *lazyLog) Write(p []byte) (int, error) {
	if l.file == nil {
		// The path is set by the owner via TIX_HOME: no trust boundary is crossed.
		info, err := os.Stat(l.dir) //nolint:gosec
		if err != nil || !info.IsDir() {
			return len(p), nil
		}
		f, err := os.OpenFile(filepath.Join(l.dir, "log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec
		if err != nil {
			return len(p), nil //nolint:nilerr // the log must not fail the command
		}
		l.file = f
	}
	return l.file.Write(p)
}
