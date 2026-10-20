// Package brief собирает вывод `tk ctx`. Без IO.
package brief

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"tracker/internal/actor"
	"tracker/internal/config"
	"tracker/internal/task"
)

// maxLine - длина строки задачи в тексте, руны.
const maxLine = 160

// howto - вторая строка ctx: три команды цикла агента.
const howto = "- take: tk take; step: tk note; end: tk set summary="

// ctxRoles - роли колонок в порядке показа.
var ctxRoles = []string{"active", "waiting", "ready"}

// Input - вход Build. PerColumn - сколько задач на колонку до сжатия, Limit - лимит рун
// текста; 0 снимает ограничение (так собирается JSON).
type Input struct {
	Tasks                     []*task.Task
	Cfg                       *config.Config
	Project, RulesPath, Actor string
	Now                       time.Time
	PerColumn, Limit          int
}

// Row - строка задачи. Detail: summary, "Waiting: ...", "k/n"; свой claim - "@you".
type Row struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Claim  string `json:"claim"`
}

// Section - колонка с ролью active, waiting или ready; More - сколько задач скрыто.
type Section struct {
	Column string `json:"column"`
	Rows   []Row  `json:"rows"`
	More   int    `json:"more"`
}

// Brief - собранный ctx. В JSON имена полей в нижнем регистре, лимит действует только на текст.
type Brief struct {
	Project  string    `json:"project"`
	Rules    string    `json:"rules"`
	Sections []Section `json:"sections"`
	Next     string    `json:"next"`
}

// Build собирает Brief: роли active, waiting, ready; сжатие до Limit. Задачи в Input
// уже отобраны по проекту. Пустой Project печатается как "all".
func Build(in Input) Brief {
	b := Brief{Project: in.Project, Rules: in.RulesPath, Sections: []Section{}}
	if b.Project == "" {
		b.Project = "all"
	}
	for _, role := range ctxRoles {
		col, ok := in.Cfg.ByRole(role)
		if !ok {
			continue
		}
		tasks := ranked(in.Tasks, col.Name)
		if role == "ready" {
			b.Next = nextID(tasks, in)
		}
		if len(tasks) == 0 {
			continue
		}
		sec := Section{Column: col.Name, Rows: make([]Row, 0, len(tasks))}
		for _, t := range tasks[:min(len(tasks), cmp.Or(in.PerColumn, len(tasks)))] {
			sec.Rows = append(sec.Rows, makeRow(t, role, in))
		}
		sec.More = len(tasks) - len(sec.Rows)
		b.Sections = append(b.Sections, sec)
	}
	return b.fit(in.Limit)
}

// fit уменьшает число задач на колонку, пока текст не влезет в limit.
// Меньше одной строки на колонку не бывает: колонка без строк прячет активную задачу.
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

// shrink возвращает копию, где в каждой колонке не больше per строк.
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

// ranked возвращает задачи колонки по rank, при равенстве по id.
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

// liveClaim возвращает захват, если он не истёк.
func liveClaim(t *task.Task, now time.Time) (task.Claim, bool) {
	c, ok := t.ClaimInfo()
	return c, ok && now.Before(c.Until)
}

// nextID возвращает первую задачу ready без живого чужого захвата.
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

// detail: у waiting - причина, у ready - отмечено/всего, у active - summary.
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

// oneLine склеивает пробелы и переводы строк в один пробел.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Text печатает Brief строками `tk ctx`.
func (b Brief) Text() string {
	head := "project " + b.Project
	if b.Rules != "" {
		head += "   rules: " + b.Rules
	}
	lines := []string{head, howto}
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

// rowLine печатает "<колонка> id title | detail  claim" не длиннее maxLine рун.
// Лишнее отрезается от title и detail, claim остаётся.
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
