package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/search"
	"github.com/daniil4545/tix/internal/task"
)

// reply prints `ok <id> <what>`; -v adds the commit hash.
// A failed commit is a warning on stderr, code 0.
func (a *app) reply(p *parsed, res core.Result, id, what string) {
	if res.ID != "" {
		id = res.ID
	}
	line := "ok " + id + " " + what
	if p.set["v"] && res.Commit != "" {
		line += " " + res.Commit
	}
	a.out("%s", line)
	for _, e := range res.Ready {
		a.out("epic %s: all tasks closed, ready for acceptance", e)
	}
	for _, n := range res.Notices {
		_, _ = fmt.Fprintln(a.env.Stderr, n)
	}
	if res.Warning != "" {
		a.warn("%s: %s", id, res.Warning)
	}
}

// wrap ties a core error to a task and hints what to do about a conflict.
func wrap(id string, err error) error {
	if errors.Is(err, core.ErrConflict) {
		err = fmt.Errorf("%w; run tix show %s again", err, id)
	}
	return &idError{ID: id, Err: err}
}

func (a *app) cmdNew(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"to": true, "project": true, "body-file": true, "parent": true}))
	if err != nil {
		return err
	}
	actor, err := a.actorOf(p)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(p.pos, " "))
	if title == "" {
		return inputf("title required: tix new <title>")
	}
	if err := a.open(); err != nil {
		return err
	}
	column, err := a.column(p)
	if err != nil {
		return err
	}
	proj := p.vals["project"]
	if proj == "" {
		var ok bool
		if proj, ok = a.detectProject(); !ok {
			proj = personalProject
		}
	}
	body, err := a.newBody(p.vals["body-file"])
	if err != nil {
		return err
	}
	res, err := a.store.CreateUnder(p.vals["parent"], title, proj, column, body, actor)
	if err != nil {
		return err
	}
	a.reply(p, res, "", column)
	return nil
}

// newBody reads --body-file, "-" being stdin (as gh issue create --body-file). Stdin is never read
// without the flag: an agent shell may hold it open with no writer, and a shell loop feeds its own lines.
func (a *app) newBody(path string) (string, error) {
	var data []byte
	var err error
	switch {
	case path == "":
		return "", nil
	case path == "-" && a.env.Stdin != nil:
		data, err = io.ReadAll(a.env.Stdin)
	case path == "-":
		return "", nil
	default:
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.env.Cwd, path)
		}
		data, err = os.ReadFile(path) //nolint:gosec // the caller names its own body file
	}
	if err != nil {
		return "", inputf("body file: %v", err)
	}
	return string(data), nil
}

// cfg returns the config of the store; before the store is open - the default one.
func (a *app) cfg() *config.Config {
	if a.store == nil {
		return config.Default()
	}
	return a.store.Config()
}

// checkStatus refuses a name that is not a configured status; an old column word gets a hint. Exit code 6.
func (a *app) checkStatus(name string) error {
	if err := a.cfg().CheckStatus(name); err != nil {
		return inputf("%v", err)
	}
	return nil
}

// label prints a status as its name, or "name [kind]" when the name is not the kind word; "?" for none.
func (a *app) label(status string) string {
	if status == "" {
		return "?"
	}
	if k, ok := a.cfg().KindOf(status); ok && k != status {
		return status + " [" + k + "]"
	}
	return status
}

// column returns the status of a new task: --to, else the default status of the kind idea.
func (a *app) column(p *parsed) (string, error) {
	if !p.set["to"] {
		return a.cfg().DefaultOf(config.Idea), nil
	}
	return p.vals["to"], a.checkStatus(p.vals["to"])
}

func (a *app) cmdText(args []string) error {
	p, err := parseArgs(args, withCommon(nil))
	if err != nil {
		return err
	}
	id, err := taskID(p.pos)
	if err != nil {
		return err
	}
	actor, err := a.actorOf(p)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(p.pos[1:], " "))
	if text == "" {
		return &idError{ID: id, Err: inputf("text required: tix %s %s <text>", a.cmd, id)}
	}
	if err := a.open(); err != nil {
		return err
	}
	op := a.store.Comment
	if a.cmd == "note" {
		op = a.store.Note
	}
	res, err := op(id, text, actor)
	if err != nil {
		return wrap(id, err)
	}
	a.reply(p, res, id, a.cmd)
	return nil
}

func (a *app) cmdRemove(args []string) error {
	p, err := parseArgs(args, withCommon(nil))
	if err != nil {
		return err
	}
	id, err := taskID(p.pos)
	if err != nil {
		return err
	}
	actor, err := a.actorOf(p)
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	res, err := a.store.Remove(id, actor)
	if err != nil {
		return wrap(id, err)
	}
	a.reply(p, res, id, "rm")
	return nil
}

func (a *app) cmdEdit(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"base": true}))
	if err != nil {
		return err
	}
	id, err := taskID(p.pos)
	if err != nil {
		return err
	}
	actor, err := a.actorOf(p)
	if err != nil {
		return err
	}
	if p.vals["base"] == "" {
		return &idError{ID: id, Err: inputf("--base <rev> from tix show %s required", id)}
	}
	body, err := a.readBody()
	if err != nil {
		return &idError{ID: id, Err: err}
	}
	if err := a.open(); err != nil {
		return err
	}
	res, err := a.store.Edit(id, p.vals["base"], body, actor)
	if err != nil {
		return wrap(id, err)
	}
	a.reply(p, res, id, "edit")
	return nil
}

// readBody reads the new body from stdin; an empty body is a refusal.
func (a *app) readBody() (string, error) {
	if a.env.Stdin == nil {
		return "", inputf("task body not passed on stdin")
	}
	data, err := io.ReadAll(a.env.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", inputf("empty body on stdin")
	}
	return string(data), nil
}

func (a *app) cmdShow(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"json": false}))
	if err != nil {
		return err
	}
	id, err := taskID(p.pos)
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	if !p.set["json"] {
		sh, err := a.store.ShowNotes(id)
		if err != nil {
			return wrap(id, err)
		}
		// The file as is: Obsidian may have written it in a non-canonical form.
		a.out("rev %s", sh.Rev)
		if l := a.label(sh.Task.Status); l != sh.Task.Status {
			a.out("Status: %s", l)
		}
		for _, e := range a.store.EpicsOf(id) {
			a.out("Epic: %s", e)
		}
		a.printFamily(id, sh.Task)
		if after := a.waitingOn(sh.Task); len(after) > 0 {
			a.out("after %s", strings.Join(after, ", "))
		}
		_, _ = a.env.Stdout.Write(sh.Data)
		a.printNotes(id, sh, p.set["v"])
		return nil
	}
	t, rev, _, err := a.store.Show(id)
	if err != nil {
		return wrap(id, err)
	}
	t.Rev = rev
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if t.Comments == nil {
		t.Comments = []task.Comment{}
	}
	return a.printJSON(t)
}

// printFamily prints the parent line of a child and the children of a parent, as written, one step deep.
func (a *app) printFamily(id string, t *task.Task) {
	if t.Parent != "" {
		if a.store.HasTask(t.Parent) {
			a.out("Parent: %s", t.Parent)
		} else {
			a.out("Parent: %s (not found)", t.Parent)
		}
	}
	kids, closed := a.store.Children(id)
	if len(kids) == 0 {
		return
	}
	a.out("Children: %d/%d", closed, len(kids))
	for _, k := range kids {
		a.out("  %s %s %s", a.label(k.Status), k.ID, k.Title)
	}
}

// showNotesDefault - how many latest notes `show` prints without -v.
const showNotesDefault = 5

// printNotes appends the note section after the file: the last showNotesDefault notes, all with -v.
// Notes live in the store history, not in the file. A history read failure is a warning, show goes on.
func (a *app) printNotes(id string, sh core.Shown, all bool) {
	notes := sh.Notes
	if sh.NotesErr != nil {
		a.warn("notes of %s not read: %s", id, firstLine(sh.NotesErr.Error()))
		return
	}
	if len(notes) == 0 {
		return
	}
	head := fmt.Sprintf("Notes (%d)", len(notes))
	if !all && len(notes) > showNotesDefault {
		head = fmt.Sprintf("Notes (last %d of %d, -v shows all)", showNotesDefault, len(notes))
		notes = notes[len(notes)-showNotesDefault:]
	}
	var sb strings.Builder
	sb.WriteString("\n## " + head + "\n")
	for _, n := range notes {
		actor := n.Actor
		if actor == "" {
			actor = "-"
		}
		fmt.Fprintf(&sb, "- %s %s: %s\n", n.At.Format("2006-01-02 15:04"), actor, n.Text)
	}
	_, _ = a.env.Stdout.Write([]byte(sb.String()))
}

func (a *app) printJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("json: %w", err)
	}
	_, _ = a.env.Stdout.Write(append(data, '\n'))
	return nil
}

// listItem - a task in `list --json`: Task fields without body and comments.
type listItem struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Project  string   `json:"project"`
	Status   string   `json:"status"`
	Kind     string   `json:"kind"`
	External string   `json:"external"`
	Waiting  string   `json:"waiting"`
	Claim    string   `json:"claim"`
	Rank     float64  `json:"rank"`
	Created  string   `json:"created"`
	Tags     []string `json:"tags"`
	Rev      string   `json:"rev"`
	Path     string   `json:"path"`
	IdleDays int      `json:"idle_days,omitempty"` // only with --stale
	Parent   string   `json:"parent,omitempty"`
	// Children and ChildrenClosed are set for a parent only.
	Children       *int `json:"children,omitempty"`
	ChildrenClosed *int `json:"children_closed,omitempty"`
}

func (a *app) cmdList(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{
		"project": true, "status": true, "kind": true, "json": false, "all": false, "grep": true, "like": true, "stale": false,
	}))
	if err != nil {
		return err
	}
	if len(p.pos) > 0 {
		return inputf("list takes no arguments, extra: %s", p.pos[0])
	}
	root, err := a.rootDir()
	if err != nil {
		return err
	}
	// Empty store: nothing to read, list does not create the folder.
	if _, statErr := os.Stat(root); errors.Is(statErr, os.ErrNotExist) {
		return a.printTasks(p, nil, nil, nil, nil)
	}
	if err := a.open(); err != nil {
		return err
	}
	tasks, errs := a.store.List()
	for _, e := range errs {
		a.warn("%s", firstLine(e.Error()))
	}
	notClosed := task.NotClosed(tasks, a.cfg())
	proj := p.vals["project"]
	if !p.set["project"] {
		proj, _ = a.detectProject()
	}
	if err := a.checkFilters(p); err != nil {
		return err
	}
	// an explicit --status or --kind shows its tasks even when they are closed
	hide := !p.set["all"] && !p.set["status"] && !p.set["kind"]
	lq, proj, err := likeQuery(p, a.cfg(), proj, tasks)
	if err != nil {
		return err
	}
	needle := strings.ToLower(p.vals["grep"])
	var matched []*task.Task
	for _, t := range tasks {
		if hide && a.kindOf(t.Status) == config.Closed {
			continue
		}
		if !t.Matches(needle) {
			continue
		}
		matched = append(matched, t)
	}
	matched = a.filter(matched, proj, p.vals["status"], p.vals["kind"])
	matched = similar(p.set["like"], lq, matched)
	if !p.set["stale"] {
		return a.printTasks(p, matched, tasks, nil, notClosed)
	}
	stale, days, err := a.stale(matched)
	if err != nil {
		return err
	}
	return a.printTasks(p, stale, tasks, days, notClosed)
}

// waitingOn returns the After tasks of t that are not closed; the store is read only when t has After.
func (a *app) waitingOn(t *task.Task) []string {
	if len(t.After()) == 0 {
		return nil
	}
	all, errs := a.store.List()
	for _, e := range errs {
		a.warn("%s", firstLine(e.Error()))
	}
	return task.WaitingOn(t, task.NotClosed(all, a.cfg()))
}

// similar keeps the tasks resembling the query, most similar first; off - tasks as they are.
func similar(on bool, q search.Query, tasks []*task.Task) []*task.Task {
	if !on {
		return tasks
	}
	byID := make(map[string]*task.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	var out []*task.Task
	for _, r := range search.Similar(q, tasks) {
		out = append(out, byID[r.ID])
	}
	return out
}

// likeQuery turns --like into a query (zero without the flag) and the project to list:
// T-N takes the title and, without --project, the project of that task.
func likeQuery(p *parsed, cfg *config.Config, proj string, tasks []*task.Task) (search.Query, string, error) {
	arg := p.vals["like"]
	q := search.Query{Text: arg, Project: proj, All: p.set["all"] || p.set["status"] || p.set["kind"], Config: cfg}
	if !p.set["like"] || !strings.HasPrefix(arg, "T-") {
		return q, proj, nil
	}
	i := slices.IndexFunc(tasks, func(t *task.Task) bool { return t.ID == arg })
	if i < 0 {
		return q, proj, fmt.Errorf("%s: %w", arg, core.ErrNotFound)
	}
	q.Text, q.SelfID = tasks[i].Title, arg
	if !p.set["project"] {
		q.Project, proj = tasks[i].Project, tasks[i].Project
	}
	return q, proj, nil
}

// stale keeps the tasks without a move for staleAfter, most idle first, and their idle days.
// The whole history is read: the idle days need the last move, however old.
func (a *app) stale(tasks []*task.Task) ([]*task.Task, map[string]int, error) {
	moves, err := a.store.Moves(time.Time{})
	if err != nil {
		return nil, nil, err
	}
	its := idleTasks(tasks, moves, time.Now(), a.cfg())
	out, days := make([]*task.Task, 0, len(its)), make(map[string]int, len(its))
	for _, it := range its {
		out = append(out, it.t)
		days[it.t.ID] = it.days
	}
	return out, days, nil
}

// checkFilters refuses a --status that is not a configured name and a --kind that is not a kind.
func (a *app) checkFilters(p *parsed) error {
	if p.set["status"] {
		if err := a.checkStatus(p.vals["status"]); err != nil {
			return err
		}
	}
	if p.set["kind"] && !slices.Contains(config.Kinds, p.vals["kind"]) {
		return inputf("unknown kind %q; kinds: %s", p.vals["kind"], strings.Join(config.Kinds, " "))
	}
	return nil
}

// kindOf returns the kind of a status; "" for a name that is not in the config.
func (a *app) kindOf(status string) string {
	k, _ := a.cfg().KindOf(status)
	return k
}

// filter keeps the tasks of the project, status name and kind (an empty value means no filter)
// and sorts: statuses in config order, within a status by rank, then by number.
func (a *app) filter(tasks []*task.Task, proj, status, kind string) []*task.Task {
	list := a.cfg().StatusList()
	pos := func(s string) int {
		if i := slices.IndexFunc(list, func(x config.Status) bool { return x.Name == s }); i >= 0 {
			return i
		}
		return len(list)
	}
	var out []*task.Task
	for _, t := range tasks {
		if (proj == "" || strings.EqualFold(t.Project, proj)) && (status == "" || t.Status == status) &&
			(kind == "" || a.kindOf(t.Status) == kind) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if pos(x.Status) != pos(y.Status) {
			return pos(x.Status) < pos(y.Status)
		}
		if x.Rank != y.Rank {
			return x.Rank < y.Rank
		}
		return core.IDNum(x.ID) < core.IDNum(y.ID)
	})
	return out
}

func (a *app) printTasks(p *parsed, tasks, all []*task.Task, days map[string]int, notClosed map[string]bool) error {
	counts := task.ChildCounts(all, a.cfg())
	if p.set["json"] {
		items := make([]listItem, 0, len(tasks))
		for _, t := range tasks {
			tags := t.Tags
			if tags == nil {
				tags = []string{}
			}
			it := listItem{
				ID: t.ID, Title: t.Title, Project: t.Project, Status: t.Status, Kind: a.kindOf(t.Status),
				External: t.External, Waiting: t.Waiting, Claim: t.Claim,
				Rank: t.Rank, Created: t.Created.Format(task.CreatedLayout), Tags: tags, Rev: t.Rev, Path: t.Path,
				IdleDays: days[t.ID], Parent: t.Parent,
			}
			if c, ok := counts[t.ID]; ok {
				it.ChildrenClosed, it.Children = &c[0], &c[1]
			}
			items = append(items, it)
		}
		return a.printJSON(items)
	}
	if len(tasks) == 0 {
		a.out("no tasks")
		return nil
	}
	for _, t := range tasks {
		status := a.label(t.Status)
		line := status + " " + t.ID + " " + t.Title
		if c, ok := counts[t.ID]; ok {
			line = fmt.Sprintf("%s %s [epic %d/%d] %s", status, t.ID, c[0], c[1], t.Title)
		} else if t.IsEpic() {
			line = status + " " + t.ID + " [epic] " + t.Title
		}
		if d, ok := days[t.ID]; ok {
			line += fmt.Sprintf(" | idle %dd", d)
		}
		if after := task.WaitingOn(t, notClosed); len(after) > 0 {
			line += " | after " + strings.Join(after, ", ")
		}
		a.out("%s", line)
	}
	return nil
}
