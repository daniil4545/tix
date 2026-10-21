package brief_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/daniil4545/tix/internal/brief"
	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/task"
)

func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

var now = time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)

func mk(id, title, status string, rank float64) *task.Task {
	return &task.Task{ID: id, Title: title, Status: status, Rank: rank, Project: "p"}
}

func input(tasks ...*task.Task) brief.Input {
	return brief.Input{Tasks: tasks, Cfg: config.Default(), Project: "p", RulesPath: "/h/RULES.md",
		Actor: "claude:me", Now: now, PerColumn: 5, Limit: 2000}
}

func section(b brief.Brief, column string) (brief.Section, bool) {
	for _, s := range b.Sections {
		if s.Column == column {
			return s, true
		}
	}
	return brief.Section{}, false
}

// Spec 2.2: up to 5 per column by rank, then "K more"; next is the first in ready.
func TestBuild_SevenReady_FiveRowsTwoMore_NextIsFirst(t *testing.T) {
	defer guard(t)
	var tasks []*task.Task
	for i := 7; i >= 1; i-- { // input order is not by rank
		tasks = append(tasks, mk(fmt.Sprintf("T-%d", i), "task", "todo", float64(i)))
	}
	tasks = append(tasks, mk("T-20", "idea", "idea", 1), mk("T-21", "finished", "done", 1))
	b := brief.Build(input(tasks...))
	s, ok := section(b, "todo")
	if !ok || len(s.Rows) != 5 || s.More != 2 {
		t.Fatalf("section todo: %+v, want 5 lines and More=2", s)
	}
	for i, r := range s.Rows {
		if want := fmt.Sprintf("T-%d", i+1); r.ID != want {
			t.Errorf("line %d: %s, want %s (by rank)", i, r.ID, want)
		}
	}
	if b.Next != "T-1" {
		t.Errorf("Next = %q, want T-1", b.Next)
	}
	text := b.Text()
	if !strings.Contains(text, "2 more") || !strings.Contains(text, "next T-1") {
		t.Errorf("the text lacks `2 more` or `next T-1`:\n%s", text)
	}
	for _, c := range []string{"idea", "done"} {
		if _, has := section(b, c); has {
			t.Errorf("column %s without role active, waiting, ready got into ctx", c)
		}
	}
	if strings.Contains(text, "T-20") || strings.Contains(text, "T-21") {
		t.Errorf("tasks from inbox and closed are in the text:\n%s", text)
	}
}

func TestBuild_ClaimAndDetail(t *testing.T) {
	defer guard(t)
	own := mk("T-1", "mine", "doing", 1)
	own.Claim = "claude:me until 2026-10-20T14:00"
	other := mk("T-2", "theirs", "doing", 2)
	other.Claim = "codex:zed until 2026-10-20T14:00"
	expired := mk("T-3", "stale", "doing", 3)
	expired.Claim = "ghost:old until 2026-10-20T11:00"
	waiting := mk("T-4", "stuck", "blocked", 1)
	waiting.Waiting = "need key"
	withSummary := mk("T-5", "sum", "todo", 1)
	withSummary.Body = "## Summary\nFirst line.\nSecond.\n"
	crit := mk("T-6", "crit", "todo", 2)
	crit.Body = "## Details\n- [x] a\n- [ ] b\n- [ ] c\n"

	b := brief.Build(input(own, other, expired, waiting, withSummary, crit))
	rows := map[string]brief.Row{}
	for _, s := range b.Sections {
		for _, r := range s.Rows {
			rows[r.ID] = r
		}
	}
	if rows["T-1"].Claim != "@you" {
		t.Errorf("own claim = %q, want @you", rows["T-1"].Claim)
	}
	if c := rows["T-2"].Claim; c == "" || c == "@you" || !strings.Contains(c, "zed") {
		t.Errorf("other's claim = %q, want the owner name", c)
	}
	if c := rows["T-3"].Claim; c != "" {
		t.Errorf("expired claim is shown: %q", c)
	}
	if !strings.Contains(b.Text(), "stale") || strings.Contains(b.Text(), "ghost") {
		t.Errorf("a task with an expired claim must be in ctx without an owner:\n%s", b.Text())
	}
	if d := rows["T-4"].Detail; !strings.Contains(d, "Waiting: need key") {
		t.Errorf("Detail waiting = %q", d)
	}
	if d := rows["T-5"].Detail; d != "First line." {
		t.Errorf("Detail with summary = %q, want the first line", d)
	}
	if d := rows["T-6"].Detail; !strings.Contains(d, "1/3") {
		t.Errorf("Detail with criteria = %q, want 1/3", d)
	}
}

// Spec 2.2: up to 2000 characters, a line up to 160; compression does not lose the task count.
func TestBuild_LongTitles_FitLimit_CountsStayTrue(t *testing.T) {
	defer guard(t)
	long := strings.Repeat("Очень длинное название задачи ", 12)
	var tasks []*task.Task
	for i := 1; i <= 40; i++ {
		st := []string{"todo", "doing", "blocked"}[i%3]
		tk := mk(fmt.Sprintf("T-%d", i), long, st, float64(i))
		tk.Body = "## Summary\n" + long + "\n"
		tasks = append(tasks, tk)
	}
	b := brief.Build(input(tasks...))
	text := b.Text()
	if n := utf8.RuneCountInString(text); n > 2000 {
		t.Errorf("text is %d runes, limit 2000", n)
	}
	for _, line := range strings.Split(text, "\n") {
		if n := utf8.RuneCountInString(line); n > 160 {
			t.Errorf("line is %d runes, limit 160: %q", n, line)
		}
	}
	total := 0
	for _, s := range b.Sections {
		total += len(s.Rows) + s.More
	}
	if total != 40 {
		t.Errorf("lines and More give %d tasks, want 40", total)
	}
	if !regexp.MustCompile(`(?m)^next T-\d+$`).MatchString(text) {
		t.Errorf("the next line is lost after compression:\n%s", text)
	}
}

func TestText_HeaderRulesAndRowLines(t *testing.T) {
	defer guard(t)
	text := brief.Build(input(mk("T-12", "Fix it", "doing", 1), mk("T-17", "Next one", "todo", 1))).Text()
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if !strings.HasPrefix(lines[0], "project p") || !strings.Contains(lines[0], "rules: /h/RULES.md") {
		t.Errorf("first line %q, want `project p   rules: /h/RULES.md`", lines[0])
	}
	if !strings.Contains(text, "tix take") || !strings.Contains(text, "tix note") {
		t.Errorf("no cycle line `take: tix take; step: tix note; ...`:\n%s", text)
	}
	if !regexp.MustCompile(`(?m)^doing\s+T-12 Fix it`).MatchString(text) || !regexp.MustCompile(`(?m)^todo\s+T-17 Next one`).MatchString(text) {
		t.Errorf("task lines do not match the format `<column> T-N <title>`:\n%s", text)
	}
	if last := lines[len(lines)-1]; last != "next T-17" {
		t.Errorf("last line %q, want `next T-17`", last)
	}
}

func TestBrief_JSON_LowercaseFields(t *testing.T) {
	defer guard(t)
	tk := mk("T-1", "x", "todo", 1)
	raw, err := json.Marshal(brief.Build(input(tk)))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"project", "rules", "sections", "next"} {
		if _, has := m[k]; !has {
			t.Errorf("JSON lacks field %q: %s", k, raw)
		}
	}
	secs, _ := m["sections"].([]any)
	if len(secs) == 0 {
		t.Fatalf("sections is empty: %s", raw)
	}
	sec, _ := secs[0].(map[string]any)
	for _, k := range []string{"column", "rows", "more"} {
		if _, has := sec[k]; !has {
			t.Errorf("JSON section lacks %q: %s", k, raw)
		}
	}
	rows, _ := sec["rows"].([]any)
	row, _ := rows[0].(map[string]any)
	for _, k := range []string{"id", "title", "detail", "claim"} {
		if _, has := row[k]; !has {
			t.Errorf("JSON line lacks %q: %s", k, raw)
		}
	}
}

// The third ctx line lists columns with roles: the agent sees the name of the waiting-role column.
func TestText_ColumnsLine(t *testing.T) {
	defer guard(t)
	b := brief.Build(input(mk("T-1", "x", "todo", 1)))
	want := "- columns: idea=inbox todo=ready doing=active blocked=waiting done=closed"
	if lines := strings.Split(b.Text(), "\n"); len(lines) < 3 || lines[2] != want {
		t.Errorf("third ctx line, want %q:\n%s", want, b.Text())
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"columns":"idea=inbox todo=ready`) {
		t.Errorf("JSON lacks columns: %s", raw)
	}
}
