package task

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// sections - body sections in canonical order; SetSection writes the first two.
var sections = []string{"Abstract", "Summary", "Details"}

// SetField changes a frontmatter field: title, project, waiting, due, tags, ask, external.
// The value is one line; title is never empty; due is 2006-01-02 or empty.
func (t *Task) SetField(name, value string) error {
	switch name {
	case "title", "project", "waiting", "due", "ask", "external":
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s must be one line", name)
		}
	case "tags":
		t.Tags = splitTags(value)
		return nil
	default:
		return fmt.Errorf("field %q cannot be set: allowed title project waiting due tags ask external", name)
	}
	if name == "title" && strings.TrimSpace(value) == "" {
		return errors.New("title is empty")
	}
	if name == "due" && value != "" {
		if _, err := time.Parse(time.DateOnly, value); err != nil {
			return fmt.Errorf("due %q: want 2006-01-02", value)
		}
	}
	*t.stringField(name) = value
	return nil
}

// splitTags splits a comma-separated list; empty elements are dropped.
func splitTags(value string) []string {
	tags := []string{}
	for _, tag := range strings.Split(value, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// SetSection replaces the section text (Abstract, Summary). "## " inside the text is an error.
// No section - it is inserted by the canonical order Abstract, Summary, Details.
func (t *Task) SetSection(name, text string) error {
	i := slices.IndexFunc(sections[:2], func(s string) bool { return strings.EqualFold(s, name) })
	if i < 0 {
		return fmt.Errorf("section %q cannot be set: allowed Abstract Summary", name)
	}
	heading := "## " + sections[i]
	text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), " \t\n")
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(ln, "## ") {
			return errors.New("text must not contain a line starting with \"## \"")
		}
	}
	lines := strings.SplitAfter(t.Body, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	start := slices.IndexFunc(lines, func(ln string) bool { return strings.TrimRight(ln, " \t\r\n") == heading })
	if start < 0 {
		t.insertSection(lines, i, heading, text)
		return nil
	}
	end := start + 1
	for end < len(lines) && !strings.HasPrefix(lines[end], "## ") {
		end++
	}
	body := lines[start+1 : end]
	old := strings.TrimRight(strings.ReplaceAll(strings.Join(body, ""), "\r\n", "\n"), " \t\n")
	if old == text {
		return nil
	}
	blanks := 0
	for blanks < len(body) && strings.TrimSpace(body[len(body)-1-blanks]) == "" {
		blanks++
	}
	if end < len(lines) {
		blanks = max(blanks, 1)
	}
	span := heading + "\n" + sectionText(text) + strings.Repeat("\n", blanks)
	t.Body = strings.Join(lines[:start], "") + span + strings.Join(lines[end:], "")
	return nil
}

// sectionText prints the section text with a line break; empty text - empty.
func sectionText(text string) string {
	if text == "" {
		return ""
	}
	return text + "\n"
}

// insertSection inserts a new section before the first section that comes later in
// the canonical order, otherwise at the end of the body.
func (t *Task) insertSection(lines []string, idx int, heading, text string) {
	at := len(lines)
	for j, ln := range lines {
		if slices.Contains(sections[idx+1:], strings.TrimPrefix(strings.TrimRight(ln, " \t\r\n"), "## ")) &&
			strings.HasPrefix(ln, "## ") {
			at = j
			break
		}
	}
	var b strings.Builder
	b.WriteString(strings.Join(lines[:at], ""))
	if at > 0 && strings.TrimSpace(lines[at-1]) != "" {
		if !strings.HasSuffix(lines[at-1], "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(heading + "\n" + sectionText(text))
	if at < len(lines) || t.hasComments() {
		b.WriteString("\n")
	}
	b.WriteString(strings.Join(lines[at:], ""))
	t.Body = b.String()
}
