package task

import "strings"

// MatchLine reports the first line of the title, the Abstract or the criteria that contains
// needle, ignoring case. needle must be lower case; the empty needle matches the title.
func (t *Task) MatchLine(needle string) (string, bool) {
	if strings.Contains(strings.ToLower(t.Title), needle) {
		return t.Title, true
	}
	inAbstract := false
	for _, line := range strings.Split(t.Body, "\n") {
		if strings.HasPrefix(line, "## ") {
			inAbstract = strings.TrimRight(line, " \t\r") == "## Abstract"
			continue
		}
		if inAbstract && strings.Contains(strings.ToLower(line), needle) {
			return line, true
		}
	}
	for _, c := range t.Criteria() {
		if strings.Contains(strings.ToLower(c.Text), needle) {
			return c.Text, true
		}
	}
	return "", false
}

// Matches reports whether needle (lower case) is in the title, the Abstract or a criterion.
func (t *Task) Matches(needle string) bool {
	_, ok := t.MatchLine(needle)
	return ok
}
