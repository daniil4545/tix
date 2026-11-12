// Package brief builds the output of `tix ctx`. No IO.
package brief

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/task"
)

// maxLine - the length of a task line in the text, in runes.
const maxLine = 160

// maxStaleIDs - how many ids the stale line names.
const maxStaleIDs = 10

// howto - the second line of ctx: the three commands of the agent cycle.
const howto = "- take: tix take; step: tix note; end: tix done"

// ctxKinds - the kinds a project ctx lists, in display order.
var ctxKinds = []string{config.InProgress, config.Blocked, config.Open}

// Input - the input of Build. PerColumn - how many tasks per column before compression, Limit - the rune limit
// of the text; 0 removes the limit (this is how JSON is built).
type Input struct {
	Tasks                     []*task.Task
	All                       []*task.Task // every task of the store, to tell which After ids are closed; empty - Tasks
	Project, RulesPath, Actor string
	Path                      string // the project folder; empty - no line
	Now                       time.Time
	PerColumn, Limit          int
	Stale                     []string          // idea and open tasks without a move for 14 days, most idle first
	Config                    *config.Config    // the statuses and their kinds; nil - the default statuses
	counts                    map[string][2]int // {closed, all} children per parent, set by Build
}

// Row - a task line. Detail: "Waiting: ...", "k/n"; own claim - "@you".
type Row struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Claim  string `json:"claim"`
	Epic   bool   `json:"epic,omitempty"`
	// Children and ChildrenClosed: the progress of a parent; zero for a task without children.
	Children       int `json:"children,omitempty"`
	ChildrenClosed int `json:"children_closed,omitempty"`
}

// Section - a column of tasks; More - how many tasks are hidden.
type Section struct {
	Column string `json:"column"`
	Kind   string `json:"kind"`
	Rows   []Row  `json:"rows"`
	More   int    `json:"more"`
}

// label prints the column as its name, or "name [kind]" when the name is not the kind word.
func (s Section) label() string {
	if s.Kind == "" || s.Kind == s.Column {
		return s.Column
	}
	return s.Column + " [" + s.Kind + "]"
}

// Brief - the built ctx. In JSON the field names are lowercase, the limit applies to text only.
type Brief struct {
	Project  string    `json:"project"`
	Rules    string    `json:"rules"`
	Path     string    `json:"path,omitempty"`
	Sections []Section `json:"sections"`
	Next     string    `json:"next"`
	Stale    []string  `json:"stale"`
}

// Build builds a Brief with compression down to Limit. The tasks in Input are already selected
// by project. An empty Project counts tasks outside in_progress and blocked instead of listing them.
func Build(in Input) Brief {
	b := Brief{Project: in.Project, Rules: in.RulesPath, Path: in.Path, Sections: []Section{}, Stale: in.Stale}
	if b.Stale == nil {
		b.Stale = []string{}
	}
	if b.Project == "" {
		b.Project = "all"
	}
	cfg := in.Config
	if cfg == nil {
		cfg = config.Default()
	}
	statuses := cfg.StatusList()
	if in.Project != "" {
		statuses = projectStatuses(statuses)
	}
	known := in.All
	if len(known) == 0 {
		known = in.Tasks
	}
	in.counts = task.ChildCounts(known, cfg)
	for _, st := range statuses {
		status, kind := st.Name, st.Kind
		tasks := ranked(in.Tasks, status)
		if kind == config.Open && b.Next == "" {
			b.Next = nextID(tasks, in, task.NotClosed(known, cfg))
		}
		if len(tasks) == 0 {
			continue
		}
		sec := Section{Column: status, Kind: kind, Rows: make([]Row, 0, len(tasks))}
		if in.Project == "" && kind != config.InProgress && kind != config.Blocked {
			sec.More = len(tasks)
			b.Sections = append(b.Sections, sec)
			continue
		}
		for _, t := range tasks[:min(len(tasks), cmp.Or(in.PerColumn, len(tasks)))] {
			sec.Rows = append(sec.Rows, makeRow(t, kind, in))
		}
		sec.More = len(tasks) - len(sec.Rows)
		b.Sections = append(b.Sections, sec)
	}
	return b.fit(in.Limit)
}

// projectStatuses keeps the statuses a project ctx lists: the kinds of ctxKinds in that order,
// within a kind in config order.
func projectStatuses(all []config.Status) []config.Status {
	var out []config.Status
	for _, k := range ctxKinds {
		for _, s := range all {
			if s.Kind == k {
				out = append(out, s)
			}
		}
	}
	return out
}

// fit reduces the number of tasks per column until the text fits into limit.
// There is never less than one line per column: a status without lines hides an in_progress task.
func (b Brief) fit(limit int) Brief {
	if limit <= 0 {
		return b
	}
	top := 0
	for _, s := range b.Sections {
		top = max(top, len(s.Rows))
	}
	for per := top - 1; per >= 1 && utf8.RuneCountInString(b.Text()) > limit; per-- {
		b = b.shrink(per)
	}
	return b
}

// shrink returns a copy where each column has at most per lines.
func (b Brief) shrink(per int) Brief {
	out := b
	out.Sections = make([]Section, len(b.Sections))
	for i, s := range b.Sections {
		if len(s.Rows) > per {
			s.More += len(s.Rows) - per
			s.Rows = s.Rows[:per]
		}
		out.Sections[i] = s
	}
	return out
}

// ranked returns the column tasks by rank, by id on a tie.
func ranked(tasks []*task.Task, column string) []*task.Task {
	var out []*task.Task
	for _, t := range tasks {
		if t.Status == column {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b *task.Task) int {
		return cmp.Or(cmp.Compare(a.Rank, b.Rank), cmp.Compare(len(a.ID), len(b.ID)), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// liveClaim returns the claim if it has not expired.
func liveClaim(t *task.Task, now time.Time) (task.Claim, bool) {
	c, ok := t.ClaimInfo()
	return c, ok && now.Before(c.Until)
}

// nextID returns the first open task without a live claim of another actor and without an unclosed After task.
func nextID(ready []*task.Task, in Input, notClosed map[string]bool) string {
	for _, t := range ready {
		if len(task.WaitingOn(t, notClosed)) > 0 {
			continue
		}
		if c, ok := liveClaim(t, in.Now); !ok || c.Actor == in.Actor {
			return t.ID
		}
	}
	return ""
}

func makeRow(t *task.Task, kind string, in Input) Row {
	r := Row{ID: t.ID, Title: oneLine(t.Title), Detail: detail(t, kind), Epic: t.IsEpic()}
	if c, ok := in.counts[t.ID]; ok {
		r.Epic, r.ChildrenClosed, r.Children = true, c[0], c[1]
	}
	if c, ok := liveClaim(t, in.Now); ok {
		r.Claim = actor.Short(c.Actor)
		if in.Actor != "" && c.Actor == in.Actor {
			r.Claim = "@you"
		}
	}
	return r
}

// detail: for the kind blocked - the reason, for open - checked/total, otherwise empty.
func detail(t *task.Task, kind string) string {
	switch kind {
	case config.Blocked:
		if t.Waiting != "" {
			return "Waiting: " + oneLine(t.Waiting)
		}
	case config.Open:
		if cs := t.Criteria(); len(cs) > 0 {
			done := 0
			for _, c := range cs {
				if c.Done {
					done++
				}
			}
			return fmt.Sprintf("%d/%d", done, len(cs))
		}
	}
	return ""
}

// oneLine collapses spaces and line breaks into one space.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Text prints the Brief as the lines of `tix ctx`.
func (b Brief) Text() string {
	head := "project " + b.Project
	if b.Rules != "" {
		head += "   rules: " + b.Rules
	}
	lines := []string{head}
	if b.Path != "" {
		lines = append(lines, "path: "+b.Path)
	}
	lines = append(lines, howto)
	if len(b.Sections) == 0 {
		lines = append(lines, "no tasks")
	}
	width := 0
	for _, s := range b.Sections {
		width = max(width, utf8.RuneCountInString(s.label()))
	}
	for _, s := range b.Sections {
		pad := s.label() + strings.Repeat(" ", width-utf8.RuneCountInString(s.label())) + " "
		for _, r := range s.Rows {
			lines = append(lines, rowLine(pad, r))
		}
		if s.More > 0 {
			lines = append(lines, fmt.Sprintf("%s%d more", pad, s.More))
		}
	}
	if len(b.Stale) > 0 {
		ids := strings.Join(b.Stale[:min(len(b.Stale), maxStaleIDs)], ", ")
		if len(b.Stale) > maxStaleIDs {
			ids += ", ..."
		}
		lines = append(lines, fmt.Sprintf("stale 14d+: %d (%s)", len(b.Stale), ids))
	}
	if b.Next != "" {
		lines = append(lines, "next "+b.Next)
	}
	return strings.Join(lines, "\n")
}

// rowLine prints "<column> id title | detail  claim" no longer than maxLine runes.
// The excess is cut from title and detail, claim stays.
func rowLine(pad string, r Row) string {
	tail := ""
	if r.Claim != "" {
		tail = "  " + r.Claim
	}
	text := r.ID + " " + r.Title
	switch {
	case r.Children > 0:
		text = fmt.Sprintf("%s [epic %d/%d] %s", r.ID, r.ChildrenClosed, r.Children, r.Title)
	case r.Epic:
		text = r.ID + " [epic] " + r.Title
	}
	if r.Detail != "" {
		text += " | " + r.Detail
	}
	budget := maxLine - utf8.RuneCountInString(pad) - utf8.RuneCountInString(tail)
	if runes := []rune(text); len(runes) > budget {
		text = string(runes[:max(budget-3, 0)]) + "..."
	}
	return pad + text + tail
}
