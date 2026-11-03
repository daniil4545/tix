package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/brief"
	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
)

const (
	unknownActor   = "unknown"
	ctxPerColumn   = 5
	ctxTextLimit   = 2000
	rulesFile      = "RULES.md"
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
	name, known, err := actor.Resolve(flag, a.env.Getenv)
	if err != nil {
		return "", err
	}
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
	// ctx only marks own tasks: an ambiguous name leaves them unmarked, the first write names the fix.
	who, err := a.actorOf(p)
	if errors.Is(err, actor.ErrAmbiguous) {
		who, err = unknownActor, nil
	}
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	var tasks []*task.Task
	now := time.Now()
	movesC := make(chan movesResult, 1) // buffered: an early return does not block the walk
	// Empty store: nothing to read.
	if _, statErr := os.Stat(a.root); statErr != nil {
		movesC <- movesResult{}
	} else {
		// The git walk runs while the files are read; this function waits for it below.
		go func() { movesC <- movesOf(a.store, now.Add(-staleAfter)) }()
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
	all := tasks
	tasks = a.filter(tasks, proj, "")
	if len(tasks) == 0 && !p.set["json"] {
		a.out("no tasks")
		return nil
	}
	in := brief.Input{
		Tasks: tasks, All: all, Project: proj,
		RulesPath: tilde(filepath.Join(a.root, rulesFile), a.env.Getenv("HOME")),
		Now:       now, PerColumn: ctxPerColumn,
		Path: a.store.Config().Paths[strings.ToLower(proj)],
	}
	if who != unknownActor {
		in.Actor = who
	}
	if !p.set["json"] {
		in.Limit = ctxTextLimit
	}
	var m movesResult
	select {
	case m = <-movesC:
	case <-time.After(staleWait):
		m.err = errors.New("stale line skipped: git log is slow")
	}
	in.Stale = a.staleIDs(m, tasks, now)
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

// staleAfter - how long an idea or open task may stay without a move before ctx names it.
const staleAfter = 14 * 24 * time.Hour

// staleWait - how long ctx waits for the git walk: the stale line is optional, the session start hook is not.
const staleWait = 2 * time.Second

// movesResult - the answer of the git walk of ctx.
type movesResult struct {
	moves map[string]time.Time
	err   error
}

// staleIDs returns the ids of the stale tasks by the git walk m; a failed walk is a warning and no line.
func (a *app) staleIDs(m movesResult, tasks []*task.Task, now time.Time) []string {
	if m.err != nil {
		a.warn("%s", firstLine(m.err.Error()))
		return nil
	}
	var ids []string
	for _, it := range idleTasks(tasks, m.moves, now) {
		ids = append(ids, it.t.ID)
	}
	return ids
}

// movesOf runs the git walk of ctx: one window of staleAfter, not the whole history.
func movesOf(s *core.Store, since time.Time) movesResult {
	m, err := s.Moves(since)
	return movesResult{moves: m, err: err}
}

// idle - a task without a move and its idle days.
type idle struct {
	t    *task.Task
	days int
}

// idleTasks returns the idea and open tasks without a move for staleAfter, most idle first. The last move is
// the newest of moves and created: a task created by hand has no commit of its own.
func idleTasks(tasks []*task.Task, moves map[string]time.Time, now time.Time) []idle {
	var out []idle
	for _, t := range tasks {
		if t.Status != config.Idea && t.Status != config.Open {
			continue
		}
		last := t.Created
		if m := moves[t.ID]; m.After(last) {
			last = m
		}
		if last.IsZero() {
			continue // no created field and no move: the age is unknown
		}
		if d := now.Sub(last); d >= staleAfter {
			out = append(out, idle{t: t, days: int(d / (24 * time.Hour))})
		}
	}
	slices.SortStableFunc(out, func(x, y idle) int { return y.days - x.days })
	return out
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
	p, err := parseArgs(args, withCommon(flagSpec{"steal": false, "release": false, "hook": false}))
	if err != nil {
		return err
	}
	if p.set["release"] && len(p.pos) == 0 {
		return a.releaseAll(p)
	}
	if p.set["hook"] {
		return inputf("--hook works only with take --release and no number")
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
	if mode != core.Release {
		a.projectLine(res.ID)
	}
	return nil
}

// releaseAll - `take --release` without a number: drops every live claim of the actor, one commit.
// With --hook (SessionEnd) the actor comes from the hook input and any failure is silent: the session is ending.
func (a *app) releaseAll(p *parsed) error {
	if p.set["steal"] {
		return inputf("--steal and --release exclude each other")
	}
	var who string
	if p.set["hook"] {
		who = a.hookEndActor()
		if who == "" {
			return nil
		}
	} else {
		var err error
		if who, err = a.actorOf(p); err != nil {
			return err
		}
	}
	if err := a.open(); err != nil {
		return a.hookQuiet(p, err)
	}
	ids, res, err := a.store.ReleaseAll(who)
	if err != nil {
		return a.hookQuiet(p, err)
	}
	if len(ids) == 0 {
		if !p.set["hook"] {
			a.out("ok no claims")
		}
		return nil
	}
	if !p.set["hook"] {
		a.reply(p, res, strings.Join(ids, " "), "released")
	}
	return nil
}

// hookQuiet - a hook never fails the session: the error goes to the log, code 0.
func (a *app) hookQuiet(p *parsed, err error) error {
	if !p.set["hook"] {
		return err
	}
	if a.log != nil {
		a.log.Warn("release hook failed", "op", "release_all", "err", firstLine(err.Error()))
	}
	return nil
}

// hookEndActor reads the SessionEnd input and returns the session actor, "" if it is not a SessionEnd of a session.
func (a *app) hookEndActor() string {
	if a.env.Stdin == nil {
		return ""
	}
	in, ok := readHookInput(a.env.Stdin)
	if !ok || in.Event != eventEnd || in.AgentID != "" {
		return ""
	}
	return actor.HookActor(a.env.Getenv, in.SessionID, in.TurnID != "")
}

// projectLine prints where the task's project lives and which instruction files lie in its root.
// A project with no path (or an unreadable task) prints nothing.
func (a *app) projectLine(id string) {
	if len(a.store.Config().Paths) == 0 {
		return // no catalog, no extra git call per take
	}
	t, _, err := a.store.Get(id)
	if err != nil {
		return
	}
	dir := a.store.Config().Paths[strings.ToLower(t.Project)]
	if dir == "" {
		return
	}
	line := "project: " + t.Project + " " + dir
	var docs []string
	for _, f := range []string{"AGENTS.md", "CLAUDE.md"} {
		if st, err := os.Stat(filepath.Join(dir, f)); err == nil && st.Mode().IsRegular() {
			docs = append(docs, f)
		}
	}
	if len(docs) > 0 {
		line += " docs: " + strings.Join(docs, " ")
	}
	a.out("%s", line)
}

func (a *app) cmdMove(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"first": false, "after": true, "m": true, "force": false}))
	if err != nil {
		return err
	}
	id, err := target(p, 2, "tix move <id> <status> [--first|--after T-9] [-m text] [--force]")
	if err != nil {
		return err
	}
	opts, err := moveOpts(p)
	if err != nil {
		return &idError{ID: id, Err: err}
	}
	if err := checkStatus(p.pos[1]); err != nil {
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
	res, err := a.store.Move(id, config.Closed, core.MoveOpts{Message: p.vals["m"], Force: p.set["force"]}, who)
	if err != nil {
		return wrap(id, err)
	}
	a.reply(p, res, id, what(res, "done"))
	return nil
}

// checkStatus refuses a word that is not one of the five statuses; an old column word gets a hint. Exit code 6.
func checkStatus(name string) error {
	if err := config.Check(name); err != nil {
		return inputf("%v", err)
	}
	return nil
}
