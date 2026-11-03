package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/tui/view"
)

// picker - the project list opened by `p`: typed letters narrow it, arrows select, enter sets the filter.
type picker struct {
	all   []view.PickItem // "all" first, then the catalog by name
	query []rune
	sel   int
}

// startPicker opens the list over the board; the cursor starts on the current filter.
func (m *Model) startPicker() {
	items := []view.PickItem{{Name: "all", Value: ""}}
	seen := map[string]int{} // Alpha and alpha are one project: one row, counts summed
	for _, e := range m.d.Store.Catalog() {
		v := strings.ToLower(e.Name)
		if i, ok := seen[v]; ok {
			items[i].Tasks += e.Tasks
			continue
		}
		seen[v] = len(items)
		items = append(items, view.PickItem{Name: e.Name, Value: v, Tasks: e.Tasks, Count: true})
	}
	p := &picker{all: items}
	for i, it := range items {
		if strings.EqualFold(it.Value, m.project) {
			p.sel = i
		}
	}
	m.pick = p
}

// shown - the items that contain the query, ignoring case.
func (p *picker) shown() []view.PickItem {
	q := strings.ToLower(string(p.query))
	var out []view.PickItem
	for _, it := range p.all {
		if strings.Contains(strings.ToLower(it.Name), q) {
			out = append(out, it)
		}
	}
	return out
}

func (m *Model) pickerView() view.Picker {
	p := m.pick
	items := p.shown()
	return view.Picker{Query: string(p.query), Items: items, Sel: min(p.sel, max(len(items)-1, 0)), W: m.w, H: m.h}
}

// pickerKey handles a key while the list is open: esc keeps the old filter.
func (m *Model) pickerKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.pick
	items := p.shown()
	switch k := msg.String(); k {
	case "esc", "ctrl+c":
		m.pick = nil
	case "enter":
		if len(items) > 0 {
			m.project = items[min(p.sel, len(items)-1)].Value
			m.pick = nil
			m.rebuild()
		}
	case "up", "ctrl+p":
		p.sel = max(p.sel-1, 0)
	case "down", "ctrl+n":
		p.sel = min(p.sel+1, max(len(items)-1, 0))
	case "backspace":
		if n := len(p.query); n > 0 {
			p.query, p.sel = p.query[:n-1], 0
		}
	case "ctrl+u":
		p.query, p.sel = nil, 0
	default:
		if msg.Text != "" {
			p.query, p.sel = append(p.query, []rune(msg.Text)...), 0
		}
	}
	return nil
}
