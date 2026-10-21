package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/brief"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
)

const (
	unknownActor   = "unknown"
	ctxPerColumn   = 5
	ctxTextLimit   = 2000
	rulesFile      = "RULES.md"
	closedRole     = "closed"
	backupFallback = "origin"
	hookStdinLimit = 64 << 10
)

// actorOf returns the author of a write: --as, then the environment (actor.Resolve);
// identity not found - unknown.
func (a *app) actorOf(p *parsed) (string, error) {
	flag := ""
	if p.set["as"] {
		flag = p.vals["as"]
		if strings.TrimSpace(flag) == "" {
			return "", inputf("bad --as: empty")
		}
		if err := actor.Valid(flag); err != nil {
			return "", inputf("bad --as: %s", firstLine(err.Error()))
		}
	}
	name, known := actor.Resolve(flag, a.env.Getenv)
	if !known {
		return unknownActor, nil
	}
	if err := actor.Valid(name); err != nil {
		return "", inputf("bad actor %q: %s", name, firstLine(err.Error()))
	}
	a.who = name
	return name, nil
}

// what returns the tail of the ok line: the core's Info or a fallback text.
func what(res core.Result, fallback string) string {
	if res.Info != "" {
		return res.Info
	}
	return fallback
}

// target parses `<id>` and checks the number of positional arguments.
func target(p *parsed, positional int, usage string) (string, error) {
	id, err := taskID(p.pos)
	if err != nil {
		return "", err
	}
	if len(p.pos) != positional {
		return "", &idError{ID: id, Err: inputf("usage: %s", usage)}
	}
	return id, nil
}

func (a *app) cmdCtx(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"project": true, "json": false, "hook": false}))
	if err != nil {
		return err
	}
	if len(p.pos) > 0 {
		return inputf("ctx takes no arguments, extra: %s", p.pos[0])
	}
	if p.set["hook"] {
		a.exportModel()
	}
	who, err := a.actorOf(p)
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	var tasks []*task.Task
	// Empty store: nothing to read.
	if _, statErr := os.Stat(a.root); statErr == nil {
		var errs []error
		tasks, errs = a.store.List()
		for _, e := range errs {
			a.warn("%s", firstLine(e.Error()))
		}
	}
	proj := p.vals["project"]
	if !p.set["project"] {
		proj, _ = a.detectProject()
	}
	tasks = a.filter(tasks, proj, "")
	if len(tasks) == 0 && !p.set["json"] {
		a.out("no tasks")
		return nil
	}
	in := brief.Input{
		Tasks: tasks, Cfg: a.store.Config(), Project: proj,
		RulesPath: tilde(filepath.Join(a.root, rulesFile), a.env.Getenv("HOME")),
		Now:       time.Now(), PerColumn: ctxPerColumn,
	}
	if who != unknownActor {
		in.Actor = who
	}
	if !p.set["json"] {
		in.Limit = ctxTextLimit
	}
	b := brief.Build(in)
	if b.Project == "" {
		b.Project = "all"
	}
	if p.set["json"] {
		return a.printJSON(b)
	}
	a.out("%s", strings.TrimRight(b.Text(), "\n"))
	return nil
}

// hookReadTimeout - the shared hook stdin read timeout: the runner may not have closed the pipe.
var hookReadTimeout = 2 * time.Second

// readHookModel reads one JSON object from stdin (up to hookStdinLimit) and returns model.
// Timeout or non-JSON input - empty. On timeout the reading goroutine lives until the process ends.
func readHookModel(r io.Reader) string {
	done := make(chan string, 1)
	go func() {
		var in struct {
			Model string `json:"model"`
		}
		if json.NewDecoder(io.LimitReader(r, hookStdinLimit)).Decode(&in) != nil {
			done <- ""
			return
		}
		done <- in.Model
	}()
	select {
	case model := <-done:
		return model
	case <-time.After(hookReadTimeout):
		return ""
	}
}

// exportModel reads the SessionStart hook JSON from stdin and appends `export TIX_MODEL='<model>'`; quotes stop glob in [1m]
// in CLAUDE_ENV_FILE. No file, empty or foreign input, a model that fails validation - silently
// skipped: the hook must not break the session. A shell executes the file, so the value is validated.
func (a *app) exportModel() {
	path := a.env.Getenv("CLAUDE_ENV_FILE")
	if path == "" || a.env.Stdin == nil {
		return
	}
	model := readHookModel(a.env.Stdin)
	if !actor.ValidModel(model) {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the path is set by Claude Code
	if err != nil {
		a.warn("model not exported: %s", firstLine(err.Error()))
		return
	}
	line := "export TIX_MODEL='" + model + "'\n"
	// A foreign last line without a newline would merge with ours.
	if st, statErr := f.Stat(); statErr == nil && st.Size() > 0 {
		last := make([]byte, 1)
		if _, readErr := f.ReadAt(last, st.Size()-1); readErr == nil && last[0] != '\n' {
			line = "\n" + line
		}
	}
	_, werr := f.WriteString(line)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		a.warn("model not exported: %s", firstLine(werr.Error()))
	}
}

// tilde shortens the home folder to ~.
func tilde(path, home string) string {
	if home != "" && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

func (a *app) cmdTake(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"steal": false, "release": false}))
	if err != nil {
		return err
	}
	id, err := target(p, 1, "tix take <id> [--steal|--release]")
	if err != nil {
		return err
	}
	if p.set["steal"] && p.set["release"] {
		return &idError{ID: id, Err: inputf("--steal and --release exclude each other")}
	}
	who, err := a.actorOf(p)
	if err != nil {
		return err
	}
	mode := core.Take
	switch {
	case p.set["steal"]:
		mode = core.Steal
	case p.set["release"]:
		mode = core.Release
	}
	if err := a.open(); err != nil {
		return err
	}
	res, err := a.store.Take(id, mode, who)
	if err != nil {
		return wrap(id, err)
	}
	a.reply(p, res, id, what(res, "take"))
	return nil
}

func (a *app) cmdMove(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"first": false, "after": true, "m": true, "force": false}))
	if err != nil {
		return err
	}
	id, err := target(p, 2, "tix move <id> <column> [--first|--after T-9] [-m text] [--force]")
	if err != nil {
		return err
	}
	opts, err := moveOpts(p)
	if err != nil {
		return &idError{ID: id, Err: err}
	}
	who, err := a.actorOf(p)
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	res, err := a.store.Move(id, p.pos[1], opts, who)
	if err != nil {
		return wrap(id, err)
	}
	a.reply(p, res, id, what(res, "move"))
	return nil
}

// moveOpts builds MoveOpts from the flags --first, --after, -m, --force.
func moveOpts(p *parsed) (core.MoveOpts, error) {
	opts := core.MoveOpts{Message: p.vals["m"], Force: p.set["force"]}
	if p.set["first"] && p.set["after"] {
		return opts, inputf("--first and --after exclude each other")
	}
	opts.Place.First = p.set["first"]
	if p.set["after"] {
		after, err := normID(p.vals["after"])
		if err != nil {
			return opts, err
		}
		opts.Place.After = after
	}
	return opts, nil
}

func (a *app) cmdSet(args []string) error {
	p, err := parseArgs(args, withCommon(nil))
	if err != nil {
		return err
	}
	id, err := taskID(p.pos)
	if err != nil {
		return err
	}
	if len(p.pos) < 2 {
		return &idError{ID: id, Err: inputf("usage: tix set <id> field=value...")}
	}
	fields := make([]core.Field, 0, len(p.pos)-1)
	names := make([]string, 0, len(p.pos)-1)
	for _, arg := range p.pos[1:] {
		name, value, ok := strings.Cut(arg, "=")
		if !ok || name == "" {
			return &idError{ID: id, Err: inputf("bad field %q, expected name=value", arg)}
		}
		fields = append(fields, core.Field{Name: name, Value: value})
		names = append(names, name)
	}
	who, err := a.actorOf(p)
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	res, err := a.store.Set(id, fields, who)
	if err != nil {
		return wrap(id, err)
	}
	a.reply(p, res, id, what(res, strings.Join(names, ",")))
	return nil
}

func (a *app) cmdCheck(args []string) error {
	p, err := parseArgs(args, withCommon(nil))
	if err != nil {
		return err
	}
	id, err := taskID(p.pos)
	if err != nil {
		return err
	}
	if len(p.pos) < 2 {
		return &idError{ID: id, Err: inputf("usage: tix check <id> N...")}
	}
	nums := make([]int, 0, len(p.pos)-1)
	for _, arg := range p.pos[1:] {
		n, convErr := strconv.Atoi(arg)
		if convErr != nil || n < 1 {
			return &idError{ID: id, Err: inputf("bad criterion number %q, expected 1, 2, ...", arg)}
		}
		nums = append(nums, n)
	}
	who, err := a.actorOf(p)
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	res, err := a.store.Check(id, nums, who)
	if err != nil {
		return wrap(id, err)
	}
	a.reply(p, res, id, what(res, "check"))
	return nil
}

func (a *app) cmdDone(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"m": true, "force": false}))
	if err != nil {
		return err
	}
	id, err := target(p, 1, "tix done <id> [-m result] [--force]")
	if err != nil {
		return err
	}
	who, err := a.actorOf(p)
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	col, ok := a.store.Config().ByRole(closedRole)
	if !ok {
		return &idError{ID: id, Err: &ruleError{msg: "config.toml has no column with role closed"}}
	}
	res, err := a.store.Move(id, col.Name, core.MoveOpts{Message: p.vals["m"], Force: p.set["force"]}, who)
	if err != nil {
		return wrap(id, err)
	}
	a.reply(p, res, id, what(res, "done"))
	return nil
}

func (a *app) cmdBackup(ctx context.Context, args []string) error {
	p, err := parseArgs(args, withCommon(nil))
	if err != nil {
		return err
	}
	if len(p.pos) > 0 {
		return inputf("backup takes no arguments, extra: %s", p.pos[0])
	}
	if err := a.open(); err != nil {
		return err
	}
	res, err := a.store.Backup(ctx)
	if err != nil {
		return err
	}
	remote := strings.TrimPrefix(what(res, backupFallback), "backup ")
	line := "ok backup " + remote
	if p.set["v"] && res.Commit != "" {
		line += " " + res.Commit
	}
	a.out("%s", line)
	if res.Warning != "" {
		a.warn("backup: commit not recorded")
	}
	return nil
}
