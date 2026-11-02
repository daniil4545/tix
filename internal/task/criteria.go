package task

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const detailsHeading = "## Details"

var reTaskLine = regexp.MustCompile(`^T-\d+(\s|$)`)

var reCriterion = regexp.MustCompile(`^(\s*[-*] \[)( |x|X)(\] )(.*)$`)

// Criterion - a readiness criterion from the "## Details" section: number from 1, text, check mark.
type Criterion struct {
	N    int
	Text string
	Done bool
}

// criterionLines returns the body lines and the indexes of criterion lines of the "## Details" section.
// Lines inside a code block (``` or ~~~) are neither criteria nor headings.
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

// fenceAfter - the code block fence open after line ln ("" - outside a block).
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

// fenceMark - the fence a line starts with: "```", "~~~" or "".
func fenceMark(ln string) string {
	ln = strings.TrimLeft(ln, " ")
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(ln, m) {
			return m
		}
	}
	return ""
}

// isFenceLine: the line is itself a fence (the closing line of a block is already reset by fenceAfter).
func isFenceLine(ln string) bool { return fenceMark(ln) != "" }

// CriterionRows - the indexes of criterion lines in the section text, without code blocks.
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

// Criteria returns the criteria of the "## Details" section.
func (t *Task) Criteria() []Criterion {
	lines, at := t.criterionLines()
	out := make([]Criterion, 0, len(at))
	for n, i := range at {
		m := reCriterion.FindStringSubmatch(strings.TrimRight(lines[i], "\r"))
		out = append(out, Criterion{N: n + 1, Text: strings.TrimSpace(m[4]), Done: m[2] != " "})
	}
	return out
}

// IsCriterion: a line of the form "- [ ] text" or "- [x] text".
func IsCriterion(line string) bool { return reCriterion.MatchString(strings.TrimRight(line, "\r")) }

// Uncheck clears the check marks of the criteria numbered nums. changed=false: all already cleared.
// A number outside 1..n is an error, nothing changes.
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

// Check marks the criteria numbered nums. changed=false: all already marked.
// A number outside 1..n is an error, nothing changes.
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

// IsEpic: the task carries the tag epic, so its criteria that start with T-N are task lines.
func (t *Task) IsEpic() bool { return slices.Contains(t.Tags, "epic") }

// LineTask returns the task id a criterion text starts with, or "" for a plain criterion.
func LineTask(text string) string {
	m := reTaskLine.FindString(text)
	return strings.TrimSpace(m)
}

// SetLine sets the check mark of the criteria that name task id. changed=false: nothing to do.
func (t *Task) SetLine(id string, done bool) (changed bool) {
	var nums []int
	for _, c := range t.Criteria() {
		if c.Done != done && LineTask(c.Text) == id {
			nums = append(nums, c.N)
		}
	}
	if len(nums) == 0 {
		return false
	}
	if done {
		_, _ = t.Check(nums...)
	} else {
		_, _ = t.Uncheck(nums...)
	}
	return true
}
