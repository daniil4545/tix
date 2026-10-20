package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"tracker/internal/core"
	"tracker/internal/task"
)

// reply печатает `ok <id> <что>`; -v добавляет хеш коммита.
// Упавший коммит - предупреждение в stderr, код 0.
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

// wrap привязывает ошибку ядра к задаче и подсказывает, что делать с конфликтом.
func wrap(id string, err error) error {
	if errors.Is(err, core.ErrConflict) {
		err = fmt.Errorf("%w; run tk show %s again", err, id)
	}
	return &idError{ID: id, Err: err}
}

func (a *app) cmdNew(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"to": true, "project": true}))
	if err != nil {
		return err
	}
	actor, err := p.actor()
	if err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(p.pos, " "))
	if title == "" {
		return errors.New("title required: tk new <title>")
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

// column возвращает колонку новой задачи: --to, иначе inbox, иначе первая.
func (a *app) column(p *parsed) (string, error) {
	cols := a.store.Config().Columns
	if len(cols) == 0 {
		return "", errors.New("config.toml has no columns")
	}
	want := p.vals["to"]
	if !p.set["to"] {
		want = "inbox"
	}
	for _, c := range cols {
		if c.Name == want {
			return want, nil
		}
	}
	if !p.set["to"] {
		return cols[0].Name, nil
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return "", fmt.Errorf("unknown column %q; available: %s", want, strings.Join(names, ", "))
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
	actor, err := p.actor()
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(p.pos[1:], " "))
	if text == "" {
		return &idError{ID: id, Err: fmt.Errorf("text required: tk %s %s <text>", a.cmd, id)}
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
	actor, err := p.actor()
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
	actor, err := p.actor()
	if err != nil {
		return err
	}
	if p.vals["base"] == "" {
		return &idError{ID: id, Err: fmt.Errorf("--base <rev> from tk show %s required", id)}
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

// readBody читает новое тело из stdin; пустое тело - отказ.
func (a *app) readBody() (string, error) {
	if a.env.Stdin == nil {
		return "", errors.New("task body not passed on stdin")
	}
	data, err := io.ReadAll(a.env.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", errors.New("empty body on stdin")
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
	t, rev, err := a.store.Get(id)
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
	// Файл как есть: Obsidian мог записать его не в каноническом виде.
	data, err := os.ReadFile(t.Path)
	if err != nil {
		data = t.Render()
	}
	a.out("rev %s", rev)
	_, _ = a.env.Stdout.Write(data)
	return nil
}

func (a *app) printJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("json: %w", err)
	}
	_, _ = a.env.Stdout.Write(append(data, '\n'))
	return nil
}

// listItem - задача в `list --json`: поля Task без body и comments.
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
		return fmt.Errorf("list takes no arguments, extra: %s", p.pos[0])
	}
	root, err := a.rootDir()
	if err != nil {
		return err
	}
	// Пустая база: читать нечего, папку list не создаёт.
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

// filter оставляет задачи проекта и колонки (пустое значение - без фильтра)
// и сортирует: колонки по config.toml, внутри по rank, затем по номеру.
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
		if (proj == "" || t.Project == proj) && (status == "" || t.Status == status) {
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
		return idNumber(x.ID) < idNumber(y.ID)
	})
	return out
}

func idNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "T-"))
	return n
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
