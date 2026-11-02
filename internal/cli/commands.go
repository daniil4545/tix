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
	p, err := parseArgs(args, withCommon(flagSpec{"to": true, "project": true, "body-file": true}))
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
	res, err := a.store.CreateWithBody(title, proj, column, body, actor)
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

// column returns the status of a new task: --to, else idea.
func (a *app) column(p *parsed) (string, error) {
	if !p.set["to"] {
		return config.Idea, nil
	}
	return p.vals["to"], checkStatus(p.vals["to"])
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
		for _, e := range a.store.EpicsOf(id) {
			a.out("Epic: %s", e)
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
	External string   `json:"external"`
	Waiting  string   `json:"waiting"`
	Claim    string   `json:"claim"`
	Rank     float64  `json:"rank"`
	Created  string   `json:"created"`
	Tags     []string `json:"tags"`
	Rev      string   `json:"rev"`
	Path     string   `json:"path"`
	IdleDays int      `json:"idle_days,omitempty"` // only with --stale
}

func (a *app) cmdList(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{
		"project": true, "status": true, "json": false, "all": false, "grep": true, "stale": false,
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
		return a.printTasks(p, nil, nil)
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
	if p.set["status"] {
		if err := checkStatus(p.vals["status"]); err != nil {
			return err
		}
	}
	// an explicit --status shows its status even when it is closed
	hide := !p.set["all"] && !p.set["status"]
	needle := strings.ToLower(p.vals["grep"])
	var matched []*task.Task
	for _, t := range tasks {
		if hide && t.Status == config.Closed {
			continue
		}
		if !listMatches(t, needle) {
			continue
		}
		matched = append(matched, t)
	}
	matched = a.filter(matched, proj, p.vals["status"])
	if !p.set["stale"] {
		return a.printTasks(p, matched, nil)
	}
	stale, days, err := a.stale(matched)
	if err != nil {
		return err
	}
	return a.printTasks(p, stale, days)
}

// stale keeps the tasks without a move for staleAfter, most idle first, and their idle days.
// The whole history is read: the idle days need the last move, however old.
func (a *app) stale(tasks []*task.Task) ([]*task.Task, map[string]int, error) {
	moves, err := a.store.Moves(time.Time{})
	if err != nil {
		return nil, nil, err
	}
	its := idleTasks(tasks, moves, time.Now())
	out, days := make([]*task.Task, 0, len(its)), make(map[string]int, len(its))
	for _, it := range its {
		out = append(out, it.t)
		days[it.t.ID] = it.days
	}
	return out, days, nil
}

func listMatches(t *task.Task, needle string) bool {
	if strings.Contains(strings.ToLower(t.Title), needle) {
		return true
	}
	inAbstract := false
	for _, line := range strings.Split(t.Body, "\n") {
		if strings.HasPrefix(line, "## ") {
			inAbstract = strings.TrimRight(line, " \t\r") == "## Abstract"
			continue
		}
		if inAbstract && strings.Contains(strings.ToLower(line), needle) {
			return true
		}
	}
	for _, c := range t.Criteria() {
		if strings.Contains(strings.ToLower(c.Text), needle) {
			return true
		}
	}
	return false
}

// filter keeps the tasks of the project and column (an empty value means no filter)
// and sorts: statuses in board order, within a status by rank, then by number.
func (a *app) filter(tasks []*task.Task, proj, status string) []*task.Task {
	pos := func(s string) int {
		if i := slices.Index(config.Statuses, s); i >= 0 {
			return i
		}
		return len(config.Statuses)
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

func (a *app) printTasks(p *parsed, tasks []*task.Task, days map[string]int) error {
	if p.set["json"] {
		items := make([]listItem, 0, len(tasks))
		for _, t := range tasks {
			tags := t.Tags
			if tags == nil {
				tags = []string{}
			}
			items = append(items, listItem{
				ID: t.ID, Title: t.Title, Project: t.Project, Status: t.Status,
				External: t.External, Waiting: t.Waiting, Claim: t.Claim,
				Rank: t.Rank, Created: t.Created.Format(task.CreatedLayout), Tags: tags, Rev: t.Rev, Path: t.Path,
				IdleDays: days[t.ID],
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
		if t.IsEpic() {
			line = status + " " + t.ID + " [epic] " + t.Title
		}
		if d, ok := days[t.ID]; ok {
			line += fmt.Sprintf(" | idle %dd", d)
		}
		a.out("%s", line)
	}
	return nil
}
