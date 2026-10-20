package task

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// sections - разделы тела в каноническом порядке; SetSection пишет первые два.
var sections = []string{"Abstract", "Summary", "Details"}

// SetField меняет поле frontmatter: title, waiting, due, tags, ask, external.
// Значение - одна строка; title не бывает пустым; due - 2006-01-02 или пусто.
func (t *Task) SetField(name, value string) error {
	switch name {
	case "title", "waiting", "due", "ask", "external":
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s must be one line", name)
		}
	case "tags":
		t.Tags = splitTags(value)
		return nil
	default:
		return fmt.Errorf("field %q cannot be set: allowed title waiting due tags ask external", name)
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

// splitTags делит список через запятую; пустые элементы отбрасываются.
func splitTags(value string) []string {
	tags := []string{}
	for _, tag := range strings.Split(value, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// SetSection заменяет текст раздела (Abstract, Summary). "## " внутри текста - ошибка.
// Нет раздела - он вставляется по каноническому порядку Abstract, Summary, Details.
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

// sectionText печатает текст раздела с переводом строки; пустой текст - пусто.
func sectionText(text string) string {
	if text == "" {
		return ""
	}
	return text + "\n"
}

// insertSection вставляет новый раздел перед первым разделом, что идёт позже в
// каноническом порядке, иначе в конец тела.
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
