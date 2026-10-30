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

// howto - the second line of ctx: the three commands of the agent cycle.
const howto = "- take: tix take; step: tix note; end: tix set summary="

// ctxRoles - column roles in display order.
var ctxRoles = []string{"active", "waiting", "ready"}

// Input - the input of Build. PerColumn - how many tasks per column before compression, Limit - the rune limit
// of the text; 0 removes the limit (this is how JSON is built).
type Input struct {
	Tasks                     []*task.Task
	Cfg                       *config.Config
	Project, RulesPath, Actor string
	Now                       time.Time
	PerColumn, Limit          int
}

// Row - a task line. Detail: summary, "Waiting: ...", "k/n"; own claim - "@you".
type Row struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Claim  string `json:"claim"`
}

// Section - a column of tasks; More - how many tasks are hidden.
type Section struct {
	Column string `json:"column"`
	Rows   []Row  `json:"rows"`
	More   int    `json:"more"`
}

// Brief - the built ctx. In JSON the field names are lowercase, the limit applies to text only.
type Brief struct {
	Project  string    `json:"project"`
	Rules    string    `json:"rules"`
	Columns  string    `json:"columns"` // columns with roles: RULES.md speaks in roles, the board shows names
	Sections []Section `json:"sections"`
	Next     string    `json:"next"`
}

// Build builds a Brief with compression down to Limit. The tasks in Input are already selected
// by project. An empty Project counts tasks outside active and waiting instead of listing them.
func Build(in Input) Brief {
	b := Brief{Project: in.Project, Rules: in.RulesPath, Columns: in.Cfg.Roles(), Sections: []Section{}}
	if b.Project == "" {
		b.Project = "all"
	}
	var columns []config.Column
	if in.Project == "" {
		columns = in.Cfg.Columns
	} else {
		for _, role := range ctxRoles {
			if col, ok := in.Cfg.ByRole(role); ok {
				columns = append(columns, col)
			}
		}
	}
	for _, col := range columns {
		role := col.Role
		tasks := ranked(in.Tasks, col.Name)
		if role == "ready" {
			b.Next = nextID(tasks, in)
		}
		if len(tasks) == 0 {
			continue
		}
		sec := Section{Column: col.Name, Rows: make([]Row, 0, len(tasks))}
		if in.Project == "" && role != "active" && role != "waiting" {
			sec.More = len(tasks)
			b.Sections = append(b.Sections, sec)
			continue
		}
		for _, t := range tasks[:min(len(tasks), cmp.Or(in.PerColumn, len(tasks)))] {
			sec.Rows = append(sec.Rows, makeRow(t, role, in))
		}
		sec.More = len(tasks) - len(sec.Rows)
		b.Sections = append(b.Sections, sec)
	}
	return b.fit(in.Limit)
}

// fit reduces the number of tasks per column until the text fits into limit.
// There is never less than one line per column: a column without lines hides an active task.
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

// nextID returns the first ready task without a live claim of another actor.
func nextID(ready []*task.Task, in Input) string {
	for _, t := range ready {
		if c, ok := liveClaim(t, in.Now); !ok || c.Actor == in.Actor {
			return t.ID
		}
	}
	return ""
}

func makeRow(t *task.Task, role string, in Input) Row {
	r := Row{ID: t.ID, Title: oneLine(t.Title), Detail: detail(t, role)}
	if c, ok := liveClaim(t, in.Now); ok {
		r.Claim = actor.Short(c.Actor)
		if in.Actor != "" && c.Actor == in.Actor {
			r.Claim = "@you"
		}
	}
	return r
}

// detail: for waiting - the reason, for ready - checked/total, for active - the summary.
func detail(t *task.Task, role string) string {
	switch role {
	case "waiting":
		if t.Waiting != "" {
			return "Waiting: " + oneLine(t.Waiting)
		}
	case "ready":
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
	return oneLine(t.Summary())
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
	lines := []string{head, howto}
	if b.Columns != "" {
		lines = append(lines, "- columns: "+b.Columns)
	}
	if len(b.Sections) == 0 {
		lines = append(lines, "no tasks")
	}
	width := 0
	for _, s := range b.Sections {
		width = max(width, utf8.RuneCountInString(s.Column))
	}
	for _, s := range b.Sections {
		pad := s.Column + strings.Repeat(" ", width-utf8.RuneCountInString(s.Column)) + " "
		for _, r := range s.Rows {
			lines = append(lines, rowLine(pad, r))
		}
		if s.More > 0 {
			lines = append(lines, fmt.Sprintf("%s%d more", pad, s.More))
		}
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
	if r.Detail != "" {
		text += " | " + r.Detail
	}
	budget := maxLine - utf8.RuneCountInString(pad) - utf8.RuneCountInString(tail)
	if runes := []rune(text); len(runes) > budget {
		text = string(runes[:max(budget-3, 0)]) + "..."
	}
	return pad + text + tail
}
