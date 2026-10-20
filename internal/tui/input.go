package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// prompt - строка ввода внизу экрана. done получает непустой текст после enter.
type prompt struct {
	label string
	text  []rune
	done  func(text string) tea.Cmd
}

func (p *prompt) String() string { return string(p.text) }

// insert добавляет текст; переводы строк заменяются пробелами.
func (p *prompt) insert(s string) {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	p.text = append(p.text, []rune(s)...)
}

// ask открывает строку ввода; пустой текст на enter - отмена.
func (m *Model) ask(label string, done func(text string) tea.Cmd) {
	m.input = &prompt{label: label, done: done}
	m.setStatus("", false)
}

// withInput заменяет последнюю строку кадра (строку состояния) строкой ввода.
func withInput(frame, line string) string {
	i := strings.LastIndex(frame, "\n")
	if i < 0 {
		return line
	}
	return frame[:i+1] + line
}

// inputKey разбирает клавишу в строке ввода.
func (m *Model) inputKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.input
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
