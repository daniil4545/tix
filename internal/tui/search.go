package tui

import (
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
	"github.com/daniil4545/tix/internal/tui/view"
)

const excerptLen = 80 // runes around the first match

// search - the text search opened by `f`: typed text is the query, arrows select, enter opens the card.
// tab switches between the board project and all projects, ctrl+t shows closed tasks.
type search struct {
	query  []rune
	sel    int
	all    bool // all projects, not only the board one
	closed bool // closed tasks too
	found  []*task.Task
	lines  []string // the matched line of each found task
}

func (m *Model) startSearch() {
	m.find = &search{all: m.project == ""}
	m.refind()
}

// refind runs the query over the tasks: the same match as `tix list --grep`.
func (m *Model) refind() {
	s := m.find
	s.found, s.lines = s.found[:0], s.lines[:0]
	needle := strings.ToLower(strings.TrimSpace(string(s.query)))
	if needle == "" {
		return
	}
	for _, t := range m.tasks {
		if m.project != "" && !s.all && !strings.EqualFold(t.Project, m.project) || !s.closed && m.kind(t.Status) == config.Closed {
			continue
		}
		if line, ok := t.MatchLine(needle); ok {
			s.found = append(s.found, t)
			s.lines = append(s.lines, line)
		}
	}
	idx := make([]int, len(s.found))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return core.IDNum(s.found[b].ID) - core.IDNum(s.found[a].ID) })
	found, lines := make([]*task.Task, len(idx)), make([]string, len(idx))
	for i, j := range idx {
		found[i], lines[i] = s.found[j], s.lines[j]
	}
	s.found, s.lines = found, lines
	s.sel = min(s.sel, max(len(found)-1, 0))
}

// excerpt is up to excerptLen runes of text around the first occurrence of the lower case needle.
func excerpt(text, needle string) string {
	rs := []rune(strings.Join(strings.Fields(text), " "))
	low := make([]rune, len(rs))
	for i, r := range rs {
		low[i] = unicode.ToLower(r)
	}
	at, n := 0, []rune(needle)
	for i := 0; i+len(n) <= len(low); i++ {
		if string(low[i:i+len(n)]) == string(n) {
			at = i
			break
		}
	}
	lo := max(0, at-excerptLen/3)
	hi := min(len(rs), lo+excerptLen)
	lo = max(0, hi-excerptLen)
	return string(rs[lo:hi])
}

func (m *Model) searchView() view.Search {
	s := m.find
	needle := strings.ToLower(strings.TrimSpace(string(s.query)))
	v := view.Search{Query: string(s.query), Sel: s.sel, All: s.all || m.project == "", Closed: s.closed, Project: m.project, W: m.w, H: m.h}
	for i, t := range s.found {
		v.Rows = append(v.Rows, view.SearchRow{ID: t.ID, Title: t.Title, Excerpt: excerpt(s.lines[i], needle)})
	}
	return v
}

// searchKey handles a key while the search is open: esc returns to the board as it was.
func (m *Model) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	s := m.find
	switch msg.String() {
	case "esc", "ctrl+c":
		m.find = nil
	case "enter":
		if len(s.found) == 0 {
			return nil
		}
		t := s.found[s.sel]
		m.find = nil
		if m.project != "" && !strings.EqualFold(t.Project, m.project) {
			m.project = strings.ToLower(t.Project)
		}
		m.selID = t.ID
		m.rebuild()
		return m.openCard()
	case "up", "ctrl+p":
		s.sel = max(s.sel-1, 0)
	case "down", "ctrl+n":
		s.sel = min(s.sel+1, max(len(s.found)-1, 0))
	case "tab":
		if m.project != "" { // with no board project the scope is always all
			s.all = !s.all
			m.refind()
		}
	case "ctrl+t":
		s.closed = !s.closed
		m.refind()
	case "backspace":
		if n := len(s.query); n > 0 {
			s.query, s.sel = s.query[:n-1], 0
			m.refind()
		}
	case "ctrl+u":
		s.query, s.sel = nil, 0
		m.refind()
	default:
		if msg.Text != "" {
			s.query, s.sel = append(s.query, []rune(msg.Text)...), 0
			m.refind()
		}
	}
	return nil
}
