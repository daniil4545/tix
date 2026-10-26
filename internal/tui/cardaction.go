package tui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
	"github.com/daniil4545/tix/internal/tui/view"
)

// cardActs - the state of card actions. open starts the jump into a session; nil - the system
// `open` command (tests substitute their own).
type cardActs struct{ open func(url string) error }

// fieldSpec - a field that `f` edits: the choice key, the core name and the value reader.
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

// startAnswer answers an agent ask (the ask field): comment `Answer: <text>`, then ask
// is cleared if the agent did not manage to ask a new question.
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

// answer - one task of the queue: Comment, then Set ask="" only for the same question.
// Called from the write goroutine.
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

// startField opens the field choice by one key; then a line with the field value.
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

// editField asks for the new field value and writes it through Set. Empty clears the field, the same
// value is not written, a core refusal goes to the status line.
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

// cardClick handles a click on the open card: the ask line, the fields line or a button of the
// actions line. The rest of the card does nothing.
func (m *Model) cardClick(x, y int) tea.Cmd {
	hit := view.HitCard(m.cardView(), x, y)
	switch hit.Kind {
	case view.CardAsk:
		m.setStatus("", false)
		return m.startAnswer()
	case view.CardCriterion:
		m.setStatus("", false)
		m.card.cursor = hit.Index
		m.fitScroll()
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

// startDelete asks for confirmation with one key: `y` deletes the task through the write queue,
// any other key cancels. From a card, the return to the board happens right after `y`.
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

// cardEnter - enter in a card: on a criterion line it toggles the mark, on a history entry
// it expands the series of edits.
func (m *Model) cardEnter() tea.Cmd {
	if m.card.cursor < m.card.nCrit {
		return m.toggleCriterion(m.card.cursor)
	}
	m.card.toggle()
	m.fitScroll()
	return nil
}

// toggleCriterion toggles a criterion: unchecked - Check, checked - uncheck by a body edit (Edit with
// base rev). The decision is taken from the fresh file inside the write queue: two enters in a row give
// a mark and an uncheck, even if the screen is not reread yet. The criterion text is compared with what
// the user saw; changed - refusal.
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

// cardButton - a press of an actions-line button: same as the key.
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
