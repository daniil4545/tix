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
		t.Fatalf("паника: %v", r)
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

// Спека 2.2: до 5 на колонку по rank, дальше "ещё K"; следующая - первая в ready.
func TestBuild_SevenReady_FiveRowsTwoMore_NextIsFirst(t *testing.T) {
	defer guard(t)
	var tasks []*task.Task
	for i := 7; i >= 1; i-- { // порядок входа не по rank
		tasks = append(tasks, mk(fmt.Sprintf("T-%d", i), "task", "todo", float64(i)))
	}
	tasks = append(tasks, mk("T-20", "idea", "idea", 1), mk("T-21", "finished", "done", 1))
	b := brief.Build(input(tasks...))
	s, ok := section(b, "todo")
	if !ok || len(s.Rows) != 5 || s.More != 2 {
		t.Fatalf("секция todo: %+v, ждали 5 строк и More=2", s)
	}
	for i, r := range s.Rows {
		if want := fmt.Sprintf("T-%d", i+1); r.ID != want {
			t.Errorf("строка %d: %s, ждали %s (по rank)", i, r.ID, want)
		}
	}
	if b.Next != "T-1" {
		t.Errorf("Next = %q, ждали T-1", b.Next)
	}
	text := b.Text()
	if !strings.Contains(text, "2 more") || !strings.Contains(text, "next T-1") {
		t.Errorf("в тексте нет `2 more` или `next T-1`:\n%s", text)
	}
	for _, c := range []string{"idea", "done"} {
		if _, has := section(b, c); has {
			t.Errorf("колонка %s без роли active, waiting, ready попала в ctx", c)
		}
	}
	if strings.Contains(text, "T-20") || strings.Contains(text, "T-21") {
		t.Errorf("задачи из inbox и closed в тексте:\n%s", text)
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
		t.Errorf("свой claim = %q, ждали @you", rows["T-1"].Claim)
	}
	if c := rows["T-2"].Claim; c == "" || c == "@you" || !strings.Contains(c, "zed") {
		t.Errorf("чужой claim = %q, ждали имя владельца", c)
	}
	if c := rows["T-3"].Claim; c != "" {
		t.Errorf("истёкший claim показан: %q", c)
	}
	if !strings.Contains(b.Text(), "stale") || strings.Contains(b.Text(), "ghost") {
		t.Errorf("задача с истёкшим claim должна быть в ctx без владельца:\n%s", b.Text())
	}
	if d := rows["T-4"].Detail; !strings.Contains(d, "Waiting: need key") {
		t.Errorf("Detail waiting = %q", d)
	}
	if d := rows["T-5"].Detail; d != "First line." {
		t.Errorf("Detail со сводкой = %q, ждали первую строку", d)
	}
	if d := rows["T-6"].Detail; !strings.Contains(d, "1/3") {
		t.Errorf("Detail с критериями = %q, ждали 1/3", d)
	}
}

// Спека 2.2: до 2000 знаков, строка до 160; сжатие не теряет счёт задач.
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
		t.Errorf("текст %d рун, предел 2000", n)
	}
	for _, line := range strings.Split(text, "\n") {
		if n := utf8.RuneCountInString(line); n > 160 {
			t.Errorf("строка %d рун, предел 160: %q", n, line)
		}
	}
	total := 0
	for _, s := range b.Sections {
		total += len(s.Rows) + s.More
	}
	if total != 40 {
		t.Errorf("строки и More дают %d задач, ждали 40", total)
	}
	if !regexp.MustCompile(`(?m)^next T-\d+$`).MatchString(text) {
		t.Errorf("после сжатия пропала строка next:\n%s", text)
	}
}

func TestText_HeaderRulesAndRowLines(t *testing.T) {
	defer guard(t)
	text := brief.Build(input(mk("T-12", "Fix it", "doing", 1), mk("T-17", "Next one", "todo", 1))).Text()
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if !strings.HasPrefix(lines[0], "project p") || !strings.Contains(lines[0], "rules: /h/RULES.md") {
		t.Errorf("первая строка %q, ждали `project p   rules: /h/RULES.md`", lines[0])
	}
	if !strings.Contains(text, "tix take") || !strings.Contains(text, "tix note") {
		t.Errorf("нет строки цикла `take: tix take; step: tix note; ...`:\n%s", text)
	}
	if !regexp.MustCompile(`(?m)^doing\s+T-12 Fix it`).MatchString(text) || !regexp.MustCompile(`(?m)^todo\s+T-17 Next one`).MatchString(text) {
		t.Errorf("строки задач не по формату `<колонка> T-N <название>`:\n%s", text)
	}
	if last := lines[len(lines)-1]; last != "next T-17" {
		t.Errorf("последняя строка %q, ждали `next T-17`", last)
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
			t.Errorf("в JSON нет поля %q: %s", k, raw)
		}
	}
	secs, _ := m["sections"].([]any)
	if len(secs) == 0 {
		t.Fatalf("sections пуст: %s", raw)
	}
	sec, _ := secs[0].(map[string]any)
	for _, k := range []string{"column", "rows", "more"} {
		if _, has := sec[k]; !has {
			t.Errorf("в секции JSON нет %q: %s", k, raw)
		}
	}
	rows, _ := sec["rows"].([]any)
	row, _ := rows[0].(map[string]any)
	for _, k := range []string{"id", "title", "detail", "claim"} {
		if _, has := row[k]; !has {
			t.Errorf("в строке JSON нет %q: %s", k, raw)
		}
	}
}

// Третья строка ctx - колонки с ролями: агент видит, какое имя у колонки роли waiting.
func TestText_ColumnsLine(t *testing.T) {
	defer guard(t)
	b := brief.Build(input(mk("T-1", "x", "todo", 1)))
	want := "- columns: idea=inbox todo=ready doing=active blocked=waiting done=closed"
	if lines := strings.Split(b.Text(), "\n"); len(lines) < 3 || lines[2] != want {
		t.Errorf("третья строка ctx, ждали %q:\n%s", want, b.Text())
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"columns":"idea=inbox todo=ready`) {
		t.Errorf("в JSON нет columns: %s", raw)
	}
}
