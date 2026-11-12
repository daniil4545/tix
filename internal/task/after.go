package task

import (
	"regexp"
	"slices"
	"strings"

	"github.com/daniil4545/tix/internal/config"
)

var reAfter = regexp.MustCompile(`(?i)^after:\s*(.*)$`)

var reTaskID = regexp.MustCompile(`^T-\d+$`)

// After returns the ids named by the body lines "After: id, id": any letter case, each id once.
// Lines inside a code block are skipped.
func (t *Task) After() []string {
	var ids []string
	fence := ""
	for _, ln := range strings.Split(t.Body, "\n") {
		if fence = fenceAfter(ln, fence); fence != "" || isFenceLine(ln) {
			continue
		}
		m := reAfter.FindStringSubmatch(strings.TrimRight(ln, " \t\r"))
		if m == nil {
			continue
		}
		for _, f := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			if reTaskID.MatchString(f) && !slices.Contains(ids, f) {
				ids = append(ids, f)
			}
		}
	}
	return ids
}

// NotClosed is the set of ids of the tasks that are not of the kind closed; a status that is not
// in cfg counts as not closed.
func NotClosed(all []*Task, cfg *config.Config) map[string]bool {
	m := make(map[string]bool, len(all))
	for _, t := range all {
		if k, _ := cfg.KindOf(t.Status); k != config.Closed {
			m[t.ID] = true
		}
	}
	return m
}

// WaitingOn returns the After ids of t that are in notClosed. An id without a task counts as closed.
func WaitingOn(t *Task, notClosed map[string]bool) []string {
	var open []string
	for _, id := range t.After() {
		if notClosed[id] {
			open = append(open, id)
		}
	}
	return open
}

// ChildCounts returns, for every task that is the parent of some task in all, {closed, all} of its children.
// A status that is not in cfg counts as not closed.
func ChildCounts(all []*Task, cfg *config.Config) map[string][2]int {
	m := map[string][2]int{}
	for _, t := range all {
		if t.Parent == "" {
			continue
		}
		c := m[t.Parent]
		if k, _ := cfg.KindOf(t.Status); k == config.Closed {
			c[0]++
		}
		c[1]++
		m[t.Parent] = c
	}
	return m
}
