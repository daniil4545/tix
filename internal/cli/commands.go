package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/daniil4545/tix/internal/core"
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
	if res.Warning != "" {
		a.warn("%s: commit not recorded", id)
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
	p, err := parseArgs(args, withCommon(flagSpec{"to": true, "project": true}))
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
	res, err := a.store.Create(title, proj, column, actor)
	if err != nil {
		return err
	}
	a.reply(p, res, "", column)
	return nil
}

// column returns the column of a new task: --to by name or role, else the inbox role column, else the first.
func (a *app) column(p *parsed) (string, error) {
	cols := a.store.Config().Columns
	if len(cols) == 0 {
		return "", errors.New("config.toml has no columns")
	}
	want := a.store.Config().Resolve(p.vals["to"])
	for _, c := range cols {
		if p.set["to"] && c.Name == want || !p.set["to"] && c.Role == "inbox" {
			return c.Name, nil
		}
	}
	if !p.set["to"] {
		return cols[0].Name, nil
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return "", inputf("unknown column %q; available: %s", want, strings.Join(names, ", "))
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
	t, rev, data, err := a.store.Show(id)
	if err != nil {
		return wrap(id, err)
	}
	t.Rev = rev
	if p.set["json"] {
		if t.Tags == nil {
			t.Tags = []string{}
		}
		if t.Comments == nil {
			t.Comments = []task.Comment{}
		}
		return a.printJSON(t)
	}
	// The file as is: Obsidian may have written it in a non-canonical form.
	a.out("rev %s", rev)
	_, _ = a.env.Stdout.Write(data)
	a.printNotes(id, p.set["v"])
	return nil
}

// showNotesDefault - how many latest notes `show` prints without -v.
const showNotesDefault = 5

// printNotes appends the note section after the file: the last showNotesDefault notes, all with -v.
// Notes live in the store history, not in the file. A history read failure is a warning, show goes on.
func (a *app) printNotes(id string, all bool) {
	notes, err := a.store.Notes(id)
	if err != nil {
		a.warn("notes of %s not read: %s", id, firstLine(err.Error()))
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
	External string   `json:"external"`
	Waiting  string   `json:"waiting"`
	Claim    string   `json:"claim"`
	Due      string   `json:"due"`
	Ask      string   `json:"ask"`
	Rank     float64  `json:"rank"`
	Created  string   `json:"created"`
	Tags     []string `json:"tags"`
	Rev      string   `json:"rev"`
	Path     string   `json:"path"`
}

func (a *app) cmdList(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"project": true, "status": true, "json": false}))
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
		return a.printTasks(p, nil)
	}
	if err := a.open(); err != nil {
		return err
	}
	tasks, errs := a.store.List()
	for _, e := range errs {
		a.warn("%s", firstLine(e.Error()))
	}
	proj := p.vals["project"]
	if !p.set["project"] {
		proj, _ = a.detectProject()
	}
	return a.printTasks(p, a.filter(tasks, proj, p.vals["status"]))
}

// filter keeps the tasks of the project and column (an empty value means no filter)
// and sorts: columns by config.toml, within a column by rank, then by number.
func (a *app) filter(tasks []*task.Task, proj, status string) []*task.Task {
	order := map[string]int{}
	for i, c := range a.store.Config().Columns {
		order[c.Name] = i
	}
	pos := func(s string) int {
		if i, ok := order[s]; ok {
			return i
		}
		return len(order)
	}
	var out []*task.Task
	for _, t := range tasks {
		if (proj == "" || strings.EqualFold(t.Project, proj)) && (status == "" || t.Status == status) {
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

func (a *app) printTasks(p *parsed, tasks []*task.Task) error {
	if p.set["json"] {
		items := make([]listItem, 0, len(tasks))
		for _, t := range tasks {
			tags := t.Tags
			if tags == nil {
				tags = []string{}
			}
			items = append(items, listItem{
				ID: t.ID, Title: t.Title, Project: t.Project, Status: t.Status,
				External: t.External, Waiting: t.Waiting, Claim: t.Claim, Due: t.Due,
				Ask: t.Ask, Rank: t.Rank, Created: t.Created.Format(task.CreatedLayout), Tags: tags, Rev: t.Rev, Path: t.Path,
			})
		}
		return a.printJSON(items)
	}
	if len(tasks) == 0 {
		a.out("no tasks")
		return nil
	}
	for _, t := range tasks {
		status := t.Status
		if status == "" {
			status = "?"
		}
		line := status + " " + t.ID + " " + t.Title
		if sum := t.Summary(); sum != "" {
			line += " | " + sum
		}
		a.out("%s", line)
	}
	return nil
}
