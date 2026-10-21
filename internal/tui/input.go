package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/tui/view"
)

// prompt - the input line at the bottom of the screen. done gets the text after enter. Empty text is passed
// only with allowEmpty, otherwise it cancels the input. pick not nil - choice mode: one key
// from the list runs an action, no text is typed.
type prompt struct {
	label      string
	text       []rune
	allowEmpty bool
	done       func(text string) tea.Cmd
	pick       []pickOpt
	confirm    func() tea.Cmd // not nil - a yes/no question: `y` runs confirm, anything else cancels
}

// pickOpt - a choice pair: the key, the word after it and the action.
type pickOpt struct {
	key, word string
	run       func() tea.Cmd
}

// pickSep separates pairs in the choice line.
const pickSep = "  "

func (p *prompt) String() string {
	if p.pick != nil {
		return pickText(p.pick)
	}
	return string(p.text)
}

// pickText - the line of choice pairs: `t title  d due`.
func pickText(opts []pickOpt) string {
	parts := make([]string, len(opts))
	for i, o := range opts {
		parts[i] = o.key + " " + o.word
	}
	return strings.Join(parts, pickSep)
}

// insert adds text; line breaks are replaced by spaces. In choice mode no text is typed.
func (p *prompt) insert(s string) {
	if p.pick != nil || p.confirm != nil {
		return
	}
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	p.text = append(p.text, []rune(s)...)
}

// ask opens the input line; empty text on enter is a cancel.
func (m *Model) ask(label string, done func(text string) tea.Cmd) {
	m.askWith(label, "", false, done)
}

// askWith opens the input line with an initial text. allowEmpty: an empty enter passes ""
// (clear the field), otherwise it cancels the input.
func (m *Model) askWith(label, initial string, allowEmpty bool, done func(string) tea.Cmd) {
	p := &prompt{label: label, allowEmpty: allowEmpty, done: done}
	p.insert(initial)
	m.input = p
	m.setStatus("", false)
}

// confirm opens a yes/no question with one key: `y` runs yes, any other key cancels.
func (m *Model) confirm(label string, yes func() tea.Cmd) {
	m.input = &prompt{label: label, confirm: yes}
	m.setStatus("", false)
}

// choose opens the choice line with one key.
func (m *Model) choose(label string, opts []pickOpt) {
	m.input = &prompt{label: label, pick: opts}
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
	if p.pick != nil {
		return m.pickKey(msg.String())
	}
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
		if text == "" && !p.allowEmpty {
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

// pickKey: esc cancels the choice, a key from the list runs the action, other keys are ignored.
func (m *Model) pickKey(k string) tea.Cmd {
	switch k {
	case "esc", "ctrl+c":
		m.input = nil
		m.setStatus("cancelled", false)
		return nil
	}
	for _, o := range m.input.pick {
		if o.key == k {
			m.input = nil
			return o.run()
		}
	}
	return nil
}

// inputClick: a click while the input line is open. In choice mode the pair under the cursor equals its
// key; in a normal line a click does nothing.
func (m *Model) inputClick(x, y int) tea.Cmd {
	p := m.input
	if p == nil || p.pick == nil || y != m.frameHeight()-1 {
		return nil
	}
	// The line is drawn as RenderInput: the prompt, a space, the text (the tail if space is short).
	text := []rune(p.String())
	shift := 0
	if m.w > 0 {
		shift = max(len(text)-max(m.w-len([]rune(p.label))-2, 0), 0)
	}
	origin := len([]rune(p.label)) + 1 - shift
	pos := 0
	for _, o := range p.pick {
		width := len([]rune(o.key + " " + o.word))
		// A pair cut off on the left is not clickable.
		if pos >= shift && x >= origin+pos && x < origin+pos+width {
			return m.pickKey(o.key)
		}
		pos += width + len(pickSep)
	}
	return nil
}

// frameHeight - the number of frame lines that View draws now.
func (m *Model) frameHeight() int {
	frame := ""
	if m.showing {
		frame = view.RenderCard(m.cardView())
	} else {
		frame = view.RenderBoard(m.boardView())
	}
	return strings.Count(frame, "\n") + 1
}
