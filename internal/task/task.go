// Package task parses and prints a task file. No IO.
package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Comment - one comment after the "## Comments" heading.
type Comment struct {
	At    time.Time `json:"at"`
	Actor string    `json:"actor"`
	Text  string    `json:"text"`
}

// Task - a whole task. JSON names: the frontmatter fields, rev, body, comments.
type Task struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Project  string    `json:"project"`
	Status   string    `json:"status"`
	External string    `json:"external"`
	Waiting  string    `json:"waiting"`
	Claim    string    `json:"claim"`
	Due      string    `json:"due"`
	Ask      string    `json:"ask"`
	Rank     float64   `json:"rank"`
	Created  time.Time `json:"created"`
	Tags     []string  `json:"tags"`
	Body     string    `json:"body"` // up to "## Comments"
	Comments []Comment `json:"comments"`
	Rev      string    `json:"rev"` // blob id from Go, not from the file
	Path     string    `json:"path"`

	// src keeps the parsed file: foreign frontmatter fields, order and the text before
	// "## Comments" are preserved by Render. nil for a task created in code.
	src *source
}

// MarshalJSON writes created as local time like in the file (CreatedLayout), not RFC 3339.
func (t Task) MarshalJSON() ([]byte, error) {
	type plain Task
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if t.Comments == nil {
		t.Comments = []Comment{}
	}
	return json.Marshal(struct {
		plain
		Created string `json:"created"`
	}{plain: plain(t), Created: t.Created.Format(CreatedLayout)})
}

type source struct {
	raw      string // frontmatter as in the file, without "---" lines
	heading  bool   // the file has "## Comments"
	preamble string // text under the heading before the first marker
}

const (
	commentsHeading = "## Comments"
	summaryHeading  = "## Summary"
	CreatedLayout   = "2006-01-02T15:04:05"
	commentLayout   = "2006-01-02T15:04"
	maxSlug         = 40
)

var (
	reMarker  = regexp.MustCompile(`^<!-- c: (\S+) \| (.*) -->[ \t\r]*$`)
	reYAMLErr = regexp.MustCompile(`line (\d+): `)
	reDate    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

	timeLayouts = []string{CreatedLayout, commentLayout, time.RFC3339, "2006-01-02"}

	// frontmatter key order of a new task
	fieldOrder = []string{"id", "title", "project", "status", "rank", "created",
		"external", "waiting", "claim", "due", "tags", "ask"}
	requiredFields = map[string]bool{"id": true, "title": true, "project": true,
		"status": true, "rank": true, "created": true}
)

func parseErr(path string, line int, format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", path, line, fmt.Sprintf(format, args...))
}

func parseTime(s string) (time.Time, error) {
	for _, l := range timeLayouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("bad time " + strconv.Quote(s))
}

// Parse parses a task file. The error carries the path and line.
func Parse(path string, data []byte) (*Task, error) {
	text := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	fm, rest, restLine, err := splitFrontmatter(path, text)
	if err != nil {
		return nil, err
	}
	t := &Task{Path: path, Tags: []string{}, Comments: []Comment{}, src: &source{raw: fm}}
	if err := t.parseFields(path, fm); err != nil {
		return nil, err
	}
	if t.ID == "" {
		return nil, parseErr(path, 2, "frontmatter has no id")
	}
	loc := findHeading(rest)
	if loc == nil {
		t.Body = rest
		return t, nil
	}
	t.Body = rest[:loc[0]]
	t.src.heading = true
	after := rest[loc[1]:]
	afterLine := restLine + strings.Count(rest[:loc[1]], "\n")
	return t, t.parseComments(path, after, afterLine+1)
}

// findHeading looks for the line "## Comments" (spaces, tabs and \r may follow it).
// Returns [start, end of line without the break] or nil. Replaces the regexp
// (?m)^## Comments[ \t\r]*$: on a list of 1000 files the regexp took about a quarter of parsing.
func findHeading(s string) []int {
	for from := 0; ; {
		i := strings.Index(s[from:], commentsHeading)
		if i < 0 {
			return nil
		}
		start := from + i
		end := start + len(commentsHeading)
		from = end
		if start > 0 && s[start-1] != '\n' {
			continue
		}
		tail := end
		for tail < len(s) && (s[tail] == ' ' || s[tail] == '\t' || s[tail] == '\r') {
			tail++
		}
		if tail == len(s) || s[tail] == '\n' {
			return []int{start, tail}
		}
	}
}

// splitFrontmatter separates the text between "---" lines from the rest of the file.
// restLine - the number of the line where the rest begins.
func splitFrontmatter(path, text string) (fm, rest string, restLine int, err error) {
	first, tail, _ := strings.Cut(text, "\n")
	if strings.TrimRight(first, " \t\r") != "---" {
		return "", "", 0, parseErr(path, 1, "no frontmatter: file does not start with ---")
	}
	pos, line := 0, 2
	for pos <= len(tail) {
		end := strings.IndexByte(tail[pos:], '\n')
		var ln string
		next := len(tail) + 1
		if end < 0 {
			ln = tail[pos:]
		} else {
			ln = tail[pos : pos+end]
			next = pos + end + 1
		}
		if strings.TrimRight(ln, " \t\r") == "---" {
			if next > len(tail) {
				next = len(tail)
			}
			return tail[:pos], tail[next:], line + 1, nil
		}
		if end < 0 {
			break
		}
		pos, line = next, line+1
	}
	return "", "", 0, parseErr(path, 1, "frontmatter is not closed by a --- line")
}

func (t *Task) parseFields(path, fm string) error {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		line, msg := 1, strings.TrimPrefix(err.Error(), "yaml: ")
		if m := reYAMLErr.FindStringSubmatchIndex(msg); m != nil {
			n, _ := strconv.Atoi(msg[m[2]:m[3]])
			line, msg = n+1, msg[m[1]:]
		}
		return parseErr(path, line, "%s", msg)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return parseErr(path, 2, "frontmatter must be a key map")
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i].Value, m.Content[i+1]
		if err := t.setField(key, val); err != nil {
			return parseErr(path, val.Line+1, "field %s: %s", key, err)
		}
	}
	return nil
}

// setField puts a node value into a task field; foreign keys are skipped.
func (t *Task) setField(key string, val *yaml.Node) error {
	switch key {
	case "id", "title", "project", "status", "external", "waiting", "claim", "due", "ask":
		if val.Kind != yaml.ScalarNode {
			return errors.New("string required")
		}
		*t.stringField(key) = scalarText(val)
	case "rank":
		r, err := parseRank(val)
		if err != nil {
			return err
		}
		t.Rank = r
	case "created":
		if val.Kind != yaml.ScalarNode {
			return errors.New("date required")
		}
		if val.Value == "" {
			return nil
		}
		c, err := parseTime(val.Value)
		if err != nil {
			return err
		}
		t.Created = c
	case "tags":
		tags, err := nodeTags(val)
		if err != nil {
			return err
		}
		t.Tags = tags
	}
	return nil
}

func (t *Task) stringField(key string) *string {
	switch key {
	case "id":
		return &t.ID
	case "title":
		return &t.Title
	case "project":
		return &t.Project
	case "status":
		return &t.Status
	case "external":
		return &t.External
	case "waiting":
		return &t.Waiting
	case "claim":
		return &t.Claim
	case "due":
		return &t.Due
	default:
		return &t.Ask
	}
}

func scalarText(n *yaml.Node) string {
	if n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return ""
	}
	return n.Value
}

func parseRank(n *yaml.Node) (float64, error) {
	if n.Kind != yaml.ScalarNode {
		return 0, errors.New("number required")
	}
	if scalarText(n) == "" {
		return 0, nil
	}
	r, err := strconv.ParseFloat(n.Value, 64)
	if err != nil {
		return 0, errors.New("number required, not " + strconv.Quote(n.Value))
	}
	return r, nil
}

func nodeTags(n *yaml.Node) ([]string, error) {
	tags := []string{}
	switch n.Kind {
	case yaml.ScalarNode:
		if s := scalarText(n); s != "" {
			tags = append(tags, s)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return nil, errors.New("tag must be a string")
			}
			tags = append(tags, c.Value)
		}
	default:
		return nil, errors.New("list required")
	}
	return tags, nil
}

// parseComments parses the text under the heading by markers; line - the line where it starts.
func (t *Task) parseComments(path, after string, line int) error {
	var (
		cur *Comment
		buf []string
	)
	flush := func() {
		text := strings.Trim(strings.Join(buf, "\n"), "\r\n")
		if cur == nil {
			t.src.preamble = text
		} else {
			cur.Text = strings.ReplaceAll(text, "--&gt;", "-->")
			t.Comments = append(t.Comments, *cur)
		}
		buf = nil
	}
	for i, ln := range strings.Split(after, "\n") {
		var m []string
		if strings.HasPrefix(ln, "<!-- c: ") {
			m = reMarker.FindStringSubmatch(ln)
		}
		if m == nil {
			buf = append(buf, ln)
			continue
		}
		at, err := parseTime(m[1])
		if err != nil {
			return parseErr(path, line+i, "comment marker: %s", err)
		}
		flush()
		cur = &Comment{At: at, Actor: m[2]}
	}
	flush()
	return nil
}

// Render prints a task file: frontmatter, body, comments.
func (t *Task) Render() []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	b.WriteString(t.renderFrontmatter())
	b.WriteString("---\n")
	b.WriteString(t.Body)
	heading := len(t.Comments) > 0 || (t.src != nil && (t.src.heading || t.src.preamble != ""))
	if !heading {
		return b.Bytes()
	}
	b.WriteString(commentsHeading + "\n")
	if t.src != nil && t.src.preamble != "" {
		b.WriteString(t.src.preamble + "\n")
		if len(t.Comments) > 0 {
			b.WriteString("\n")
		}
	}
	for i, c := range t.Comments {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "<!-- c: %s | %s -->\n%s\n", c.At.Format(commentLayout), c.Actor,
			strings.ReplaceAll(c.Text, "-->", "--&gt;"))
	}
	return b.Bytes()
}

func (t *Task) renderFrontmatter() string {
	var doc yaml.Node
	if t.src != nil && t.src.raw != "" {
		_ = yaml.Unmarshal([]byte(t.src.raw), &doc) // already parsed in Parse
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	m := doc.Content[0]
	changed := false
	for _, key := range fieldOrder {
		if t.syncField(m, key) {
			changed = true
		}
	}
	if !changed && t.src != nil && t.src.raw != "" {
		return t.src.raw
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return t.src.raw
	}
	_ = enc.Close()
	return out.String()
}

// syncField brings a key in the node to the field value; true - the node changed.
func (t *Task) syncField(m *yaml.Node, key string) bool {
	var val *yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			val = m.Content[i+1]
			break
		}
	}
	want := t.fieldNode(key)
	if val == nil {
		if want == nil && !requiredFields[key] {
			return false
		}
		if want == nil {
			want = plainNode("")
		}
		m.Content = append(m.Content, plainNode(key), want)
		return true
	}
	if t.sameField(key, val) {
		return false
	}
	if want == nil && !requiredFields[key] {
		removeKey(m, key)
		return true
	}
	if want == nil {
		want = plainNode("")
	}
	want.LineComment, want.HeadComment, want.FootComment = val.LineComment, val.HeadComment, val.FootComment
	*val = *want
	return true
}

// removeKey removes a key-value pair from a map node.
func removeKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func plainNode(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }

// fieldNode builds a value node; nil - an empty optional field.
func (t *Task) fieldNode(key string) *yaml.Node {
	switch key {
	case "rank":
		return plainNode(strconv.FormatFloat(t.Rank, 'f', -1, 64))
	case "created":
		return plainNode(t.Created.Format(CreatedLayout))
	case "tags":
		if len(t.Tags) == 0 {
			return &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, tag := range t.Tags {
			seq.Content = append(seq.Content, stringNode(tag))
		}
		return seq
	}
	s := *t.stringField(key)
	if s == "" {
		return nil
	}
	if key == "due" && reDate.MatchString(s) {
		return plainNode(s)
	}
	return stringNode(s)
}

// stringNode wraps a string in quotes when YAML would otherwise read it differently.
func stringNode(s string) *yaml.Node {
	var n yaml.Node
	if err := n.Encode(s); err != nil {
		return plainNode(s)
	}
	return &n
}

func (t *Task) sameField(key string, val *yaml.Node) bool {
	switch key {
	case "rank":
		r, err := parseRank(val)
		return err == nil && r == t.Rank
	case "created":
		if val.Kind != yaml.ScalarNode {
			return false
		}
		c, err := parseTime(val.Value)
		if val.Value == "" {
			return t.Created.IsZero()
		}
		return err == nil && c.Equal(t.Created)
	case "tags":
		tags, err := nodeTags(val)
		return err == nil && slices.Equal(tags, t.Tags)
	}
	return val.Kind == yaml.ScalarNode && scalarText(val) == *t.stringField(key)
}

// SetBody replaces the body. "## Comments" in the body is an error.
func (t *Task) SetBody(body string) error {
	if findHeading(body) != nil {
		return errors.New("body must not contain the section " + commentsHeading)
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if strings.TrimRight(body, "\n \t") == "" {
		t.Body = ""
		return nil
	}
	body = strings.TrimRight(body, "\n") + "\n"
	if t.hasComments() {
		body += "\n"
	}
	t.Body = body
	return nil
}

func (t *Task) hasComments() bool {
	return len(t.Comments) > 0 || (t.src != nil && (t.src.heading || t.src.preamble != ""))
}

// AddComment appends a comment with a time and author marker.
func (t *Task) AddComment(at time.Time, actor, text string) {
	if !t.hasComments() && t.Body != "" && !strings.HasSuffix(t.Body, "\n\n") {
		if strings.HasSuffix(t.Body, "\n") {
			t.Body += "\n"
		} else {
			t.Body += "\n\n"
		}
	}
	if t.src == nil {
		t.src = &source{}
	}
	t.src.heading = true
	t.Comments = append(t.Comments, Comment{At: at, Actor: actor, Text: strings.Trim(text, "\r\n")})
}

// Summary returns the first line of the summary for list.
func (t *Task) Summary() string {
	in := false
	for _, ln := range strings.Split(t.Body, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case !in:
			in = ln == summaryHeading
		case strings.HasPrefix(ln, "## "):
			return ""
		case ln != "":
			return ln
		}
	}
	return ""
}

// FileName builds the file name "T-N slug.md" from the id and the title (spec 8, mockup).
func FileName(id, title string) string {
	return id + " " + slug(title) + ".md"
}

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya",
}

// translitRune gives the ASCII replacement of a rune; "-" - the separator.
func translitRune(r rune) string {
	if s, ok := translit[r]; ok {
		return s
	}
	if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
		return string(r)
	}
	return "-"
}

func slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		for _, c := range translitRune(r) {
			if c != '-' {
				b.WriteRune(c)
				dash = false
				continue
			}
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > maxSlug {
		out = strings.Trim(out[:maxSlug], "-")
	}
	if out == "" {
		return "task"
	}
	return out
}
