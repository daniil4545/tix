package tui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
	"github.com/daniil4545/tix/internal/tui/view"
)

// cardActs - состояние действий карточки. open запускает переход в сессию; nil - команда `open`
// системы (тесты подставляют свою).
type cardActs struct{ open func(url string) error }

// fieldSpec - поле, которое правит `f`: клавиша выбора, имя в ядре и чтение значения.
type fieldSpec struct {
	key, word, name string
	value           func(*task.Task) string
}

var cardFields = []fieldSpec{
	{"t", "title", "title", func(t *task.Task) string { return t.Title }},
	{"d", "due", "due", func(t *task.Task) string { return t.Due }},
	{"g", "tags", "tags", func(t *task.Task) string { return strings.Join(t.Tags, ",") }},
	{"a", "ask", "ask", func(t *task.Task) string { return t.Ask }},
	{"w", "waiting", "waiting", func(t *task.Task) string { return t.Waiting }},
	{"s", "summary", "summary", func(t *task.Task) string { return section(t.Body, "Summary") }},
	{"x", "external", "external", func(t *task.Task) string { return t.External }},
}

// startAnswer отвечает на вопрос агента (поле ask): комментарий `Answer: <текст>`, затем ask
// очищается, если агент не успел задать новый вопрос.
func (m *Model) startAnswer() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	ask := strings.TrimSpace(t.Ask)
	if ask == "" {
		m.setStatus(t.ID+": no question", false)
		return nil
	}
	id := t.ID
	m.ask("answer:", func(text string) tea.Cmd {
		return m.submit(id, "answered", nil, func() (core.Result, error) {
			return m.answer(id, ask, text)
		})
	})
	return nil
}

// answer - одна задача очереди: Comment, затем Set ask="" только при том же вопросе.
// Вызывается из горутины записи.
func (m *Model) answer(id, ask, text string) (core.Result, error) {
	res, err := m.d.Store.Comment(id, "Answer: "+text, m.d.Actor)
	if err != nil {
		return res, err
	}
	cur, _, err := m.d.Store.Get(id)
	if err != nil {
		res.Warning = "answer saved, ask not cleared: " + errText(err)
		return res, nil
	}
	if strings.TrimSpace(cur.Ask) != ask {
		return core.Result{Warning: "new question"}, nil
	}
	res, err = m.d.Store.Set(id, []core.Field{{Name: "ask", Value: ""}}, m.d.Actor)
	if err != nil {
		res.Warning = "answer saved, ask not cleared: " + errText(err)
		return res, nil
	}
	res.Info = "answered"
	return res, nil
}

// startField открывает выбор поля одной клавишей; затем строка со значением поля.
func (m *Model) startField() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	id := t.ID
	opts := make([]pickOpt, len(cardFields))
	for i, f := range cardFields {
		opts[i] = pickOpt{key: f.key, word: f.word, run: func() tea.Cmd { return m.editField(id, f) }}
	}
	m.choose("field:", opts)
	return nil
}

// editField спрашивает новое значение поля и пишет его через Set. Пусто очищает поле, то же
// значение не пишется, отказ ядра попадает в строку состояния.
func (m *Model) editField(id string, f fieldSpec) tea.Cmd {
	t := m.byID(id)
	if t == nil {
		m.setStatus(id+" removed", true)
		return nil
	}
	cur := f.value(t)
	if strings.Contains(cur, "\n") {
		m.setStatus(id+": "+f.name+" has several lines, use e", false)
		return nil
	}
	m.askWith(f.word+":", cur, true, func(text string) tea.Cmd {
		if text == cur {
			m.setStatus(id+": no change", false)
			return nil
		}
		return m.submit(id, f.name, nil, func() (core.Result, error) {
			return m.d.Store.Set(id, []core.Field{{Name: f.name, Value: text}}, m.d.Actor)
		})
	})
	return nil
}

// cardClick разбирает клик по открытой карточке: строка вопроса, строка полей или кнопка
// строки действий. Остальное место карточки - ничего.
func (m *Model) cardClick(x, y int) tea.Cmd {
	hit := view.HitCard(m.cardView(), x, y)
	switch hit.Kind {
	case view.CardAsk:
		m.setStatus("", false)
		return m.startAnswer()
	case view.CardCriterion:
		m.setStatus("", false)
		m.card.cursor = hit.Index
		return m.toggleCriterion(hit.Index)
	case view.CardFields:
		m.setStatus("", false)
		return m.startField()
	case view.CardAction:
		m.setStatus("", false)
		return m.cardButton(hit.Key)
	}
	return nil
}

// startDelete спрашивает подтверждение одной клавишей: `y` удаляет задачу через очередь записи,
// любая другая клавиша отменяет. Из карточки возврат на доску идёт сразу после `y`.
func (m *Model) startDelete() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	id := t.ID
	m.confirm("delete "+id+"? y/n", func() tea.Cmd {
		if m.showing && m.card.id == id {
			m.showing = false
		}
		return m.submit(id, "removed", nil, func() (core.Result, error) {
			return m.d.Store.Remove(id, m.d.Actor)
		})
	})
	return nil
}

// cardEnter - enter в карточке: на строке критерия переключает отметку, на записи истории
// раскрывает серию правок.
func (m *Model) cardEnter() tea.Cmd {
	if m.card.cursor < m.card.nCrit {
		return m.toggleCriterion(m.card.cursor)
	}
	m.card.toggle()
	return nil
}

// toggleCriterion переключает критерий: не отмечен - Check, отмечен - снять правкой тела (Edit с
// базой rev). Решение берётся из свежего файла внутри очереди записи: два enter подряд дают
// отметку и снятие, даже если экран ещё не перечитан. Текст критерия сверяется с тем, что
// видел пользователь; изменился - отказ.
func (m *Model) toggleCriterion(idx int) tea.Cmd {
	t := m.byID(m.card.id)
	if t == nil {
		return nil
	}
	crit := t.Criteria()
	if idx < 0 || idx >= len(crit) {
		return nil
	}
	id, text := t.ID, crit[idx].Text
	return m.submit(id, "toggled", nil, func() (core.Result, error) {
		cur, rev, err := m.d.Store.Get(id)
		if err != nil {
			return core.Result{}, err
		}
		now := cur.Criteria()
		if idx >= len(now) || now[idx].Text != text {
			return core.Result{}, errors.New("criterion changed meanwhile, not toggled")
		}
		n := now[idx].N
		if !now[idx].Done {
			return m.d.Store.Check(id, []int{n}, m.d.Actor)
		}
		if _, err := cur.Uncheck(n); err != nil {
			return core.Result{}, err
		}
		res, err := m.d.Store.Edit(id, rev, cur.Body, m.d.Actor)
		res.Info = "unchecked"
		return res, err
	})
}

// cardButton - нажатие кнопки строки действий: то же, что клавиша.
func (m *Model) cardButton(k string) tea.Cmd {
	switch k {
	case "a":
		return m.startAnswer()
	case "f":
		return m.startField()
	case "g":
		return m.startFocus()
	case "del":
		return m.startDelete()
	case "esc":
		m.showing = false
		return nil
	}
	return m.taskKey(k)
}
