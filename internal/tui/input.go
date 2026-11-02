package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// prompt - the input line at the bottom of the screen. done gets the text after enter; empty text cancels the input.
type prompt struct {
	label   string
	text    []rune
	done    func(text string) tea.Cmd
	confirm func() tea.Cmd // not nil - a yes/no question: `y` runs confirm, anything else cancels
}

func (p *prompt) String() string {
	return string(p.text)
}

// insert adds text; line breaks are replaced by spaces. In a yes/no question no text is typed.
func (p *prompt) insert(s string) {
	if p.confirm != nil {
		return
	}
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	p.text = append(p.text, []rune(s)...)
}

// ask opens the input line; empty text on enter is a cancel.
func (m *Model) ask(label string, done func(text string) tea.Cmd) {
	m.input = &prompt{label: label, done: done}
	m.setStatus("", false)
}

// confirm opens a yes/no question with one key: `y` runs yes, any other key cancels.
func (m *Model) confirm(label string, yes func() tea.Cmd) {
	m.input = &prompt{label: label, confirm: yes}
	m.setStatus("", false)
}

// withInput replaces the last frame line (the actions line or the hint) with the input line.
func withInput(frame, line string) string {
	i := strings.LastIndex(frame, "\n")
	if i < 0 {
		return line
	}
	return frame[:i+1] + line
}

// inputKey handles a key in the input line.
func (m *Model) inputKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.input
	if p.confirm != nil {
		m.input = nil
		if msg.String() == "y" {
			return p.confirm()
		}
		m.setStatus("cancelled", false)
		return nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.input = nil
		m.setStatus("cancelled", false)
	case "enter":
		m.input = nil
		text := strings.TrimSpace(p.String())
		if text == "" {
			m.setStatus("cancelled", false)
			return nil
		}
		return p.done(text)
	case "backspace":
		if n := len(p.text); n > 0 {
			p.text = p.text[:n-1]
		}
	case "ctrl+u":
		p.text = nil
	default:
		p.insert(msg.Text)
	}
	return nil
}
