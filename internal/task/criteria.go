package task

import (
	"fmt"
	"regexp"
	"strings"
)

const detailsHeading = "## Details"

var reCriterion = regexp.MustCompile(`^(\s*[-*] \[)( |x|X)(\] )(.*)$`)

// Criterion - критерий готовности из раздела "## Details": номер с 1, текст, отметка.
type Criterion struct {
	N    int
	Text string
	Done bool
}

// criterionLines возвращает строки тела и индексы строк-критериев раздела "## Details".
// Строки внутри блока кода (``` или ~~~) не критерии и не заголовки.
func (t *Task) criterionLines() (lines []string, at []int) {
	lines = strings.Split(t.Body, "\n")
	in := false
	fence := ""
	for i, ln := range lines {
		if fence = fenceAfter(ln, fence); fence != "" || isFenceLine(ln) {
			continue
		}
		switch {
		case strings.TrimRight(ln, " \t\r") == detailsHeading:
			in = true
		case strings.HasPrefix(ln, "## "):
			in = false
		case in && reCriterion.MatchString(strings.TrimRight(ln, "\r")):
			at = append(at, i)
		}
	}
	return lines, at
}

// fenceAfter - открытое ограждение блока кода после строки ln ("" - вне блока).
func fenceAfter(ln, cur string) string {
	mark := fenceMark(ln)
	switch {
	case cur == "" && mark != "":
		return mark
	case cur != "" && mark == cur:
		return ""
	}
	return cur
}

// fenceMark - ограждение, которым начинается строка: "```", "~~~" или "".
func fenceMark(ln string) string {
	ln = strings.TrimLeft(ln, " ")
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(ln, m) {
			return m
		}
	}
	return ""
}

// isFenceLine: строка сама ограждение (закрывающая строка блока уже сброшена fenceAfter).
func isFenceLine(ln string) bool { return fenceMark(ln) != "" }

// CriterionRows - индексы строк-критериев в тексте раздела, без блоков кода.
func CriterionRows(text string) []int {
	var at []int
	fence := ""
	for i, ln := range strings.Split(text, "\n") {
		if fence = fenceAfter(ln, fence); fence != "" || isFenceLine(ln) {
			continue
		}
		if IsCriterion(ln) {
			at = append(at, i)
		}
	}
	return at
}

// Criteria возвращает критерии раздела "## Details".
func (t *Task) Criteria() []Criterion {
	lines, at := t.criterionLines()
	out := make([]Criterion, 0, len(at))
	for n, i := range at {
		m := reCriterion.FindStringSubmatch(strings.TrimRight(lines[i], "\r"))
		out = append(out, Criterion{N: n + 1, Text: strings.TrimSpace(m[4]), Done: m[2] != " "})
	}
	return out
}

// IsCriterion: строка вида "- [ ] текст" или "- [x] текст".
func IsCriterion(line string) bool { return reCriterion.MatchString(strings.TrimRight(line, "\r")) }

// Uncheck снимает отметки критериев с номерами nums. changed=false: все уже сняты.
// Номер вне 1..n - ошибка, ничего не меняется.
func (t *Task) Uncheck(nums ...int) (changed bool, err error) {
	lines, at := t.criterionLines()
	for _, n := range nums {
		if n < 1 || n > len(at) {
			return false, fmt.Errorf("no criterion %d: task has %d", n, len(at))
		}
	}
	for _, n := range nums {
		i := at[n-1]
		cr := strings.TrimSuffix(lines[i], "\r")
		m := reCriterion.FindStringSubmatch(cr)
		if m[2] == " " {
			continue
		}
		lines[i] = m[1] + " " + m[3] + m[4] + strings.TrimPrefix(lines[i], cr)
		changed = true
	}
	if changed {
		t.Body = strings.Join(lines, "\n")
	}
	return changed, nil
}

// Check отмечает критерии с номерами nums. changed=false: все уже отмечены.
// Номер вне 1..n - ошибка, ничего не меняется.
func (t *Task) Check(nums ...int) (changed bool, err error) {
	lines, at := t.criterionLines()
	for _, n := range nums {
		if n < 1 || n > len(at) {
			return false, fmt.Errorf("no criterion %d: task has %d", n, len(at))
		}
	}
	for _, n := range nums {
		i := at[n-1]
		cr := strings.TrimSuffix(lines[i], "\r")
		m := reCriterion.FindStringSubmatch(cr)
		if m[2] != " " {
			continue
		}
		lines[i] = m[1] + "x" + m[3] + m[4] + strings.TrimPrefix(lines[i], cr)
		changed = true
	}
	if changed {
		t.Body = strings.Join(lines, "\n")
	}
	return changed, nil
}
