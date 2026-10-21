package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/tui/view"
)

// prompt - строка ввода внизу экрана. done получает текст после enter. Пустой текст отдаётся
// только при allowEmpty, иначе он отменяет ввод. pick не nil - режим выбора: одна клавиша
// из списка запускает действие, текст не вводится.
type prompt struct {
	label      string
	text       []rune
	allowEmpty bool
	done       func(text string) tea.Cmd
	pick       []pickOpt
	confirm    func() tea.Cmd // не nil - вопрос да/нет: `y` запускает confirm, прочее отменяет
}

// pickOpt - пара выбора: клавиша, слово после неё и действие.
type pickOpt struct {
	key, word string
	run       func() tea.Cmd
}

// pickSep разделяет пары в строке выбора.
const pickSep = "  "

func (p *prompt) String() string {
	if p.pick != nil {
		return pickText(p.pick)
	}
	return string(p.text)
}

// pickText - строка пар выбора: `t title  d due`.
func pickText(opts []pickOpt) string {
	parts := make([]string, len(opts))
	for i, o := range opts {
		parts[i] = o.key + " " + o.word
	}
	return strings.Join(parts, pickSep)
}

// insert добавляет текст; переводы строк заменяются пробелами. В режиме выбора текст не вводится.
func (p *prompt) insert(s string) {
	if p.pick != nil || p.confirm != nil {
		return
	}
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	p.text = append(p.text, []rune(s)...)
}

// ask открывает строку ввода; пустой текст на enter - отмена.
func (m *Model) ask(label string, done func(text string) tea.Cmd) {
	m.askWith(label, "", false, done)
}

// askWith открывает строку ввода с начальным текстом. allowEmpty: пустой enter отдаёт ""
// (очистить поле), иначе отменяет ввод.
func (m *Model) askWith(label, initial string, allowEmpty bool, done func(string) tea.Cmd) {
	p := &prompt{label: label, allowEmpty: allowEmpty, done: done}
	p.insert(initial)
	m.input = p
	m.setStatus("", false)
}

// confirm открывает вопрос да/нет одной клавишей: `y` запускает yes, любая другая клавиша отменяет.
func (m *Model) confirm(label string, yes func() tea.Cmd) {
	m.input = &prompt{label: label, confirm: yes}
	m.setStatus("", false)
}

// choose открывает строку выбора одной клавишей.
func (m *Model) choose(label string, opts []pickOpt) {
	m.input = &prompt{label: label, pick: opts}
	m.setStatus("", false)
}

// withInput заменяет последнюю строку кадра (строку действий или подсказку) строкой ввода.
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

// pickKey: esc отменяет выбор, клавиша из списка запускает действие, прочие клавиши игнорируются.
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

// inputClick: клик при открытой строке ввода. В режиме выбора пара под курсором равна её
// клавише; в обычной строке клик ничего не делает.
func (m *Model) inputClick(x, y int) tea.Cmd {
	p := m.input
	if p == nil || p.pick == nil || y != m.frameHeight()-1 {
		return nil
	}
	// Строка рисуется как RenderInput: приглашение, пробел, текст (при нехватке места - хвост).
	text := []rune(p.String())
	shift := 0
	if m.w > 0 {
		shift = max(len(text)-max(m.w-len([]rune(p.label))-2, 0), 0)
	}
	origin := len([]rune(p.label)) + 1 - shift
	pos := 0
	for _, o := range p.pick {
		width := len([]rune(o.key + " " + o.word))
		// Обрезанная слева пара не кликается.
		if pos >= shift && x >= origin+pos && x < origin+pos+width {
			return m.pickKey(o.key)
		}
		pos += width + len(pickSep)
	}
	return nil
}

// frameHeight - число строк кадра, который сейчас рисует View.
func (m *Model) frameHeight() int {
	frame := ""
	if m.showing {
		frame = view.RenderCard(m.cardView())
	} else {
		frame = view.RenderBoard(m.boardView())
	}
	return strings.Count(frame, "\n") + 1
}
