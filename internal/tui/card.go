package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/actor"
	"tracker/internal/gitx"
	"tracker/internal/history"
	"tracker/internal/task"
	"tracker/internal/tui/view"
)

// cardState - карточка: курсор по истории и раскрытые серии правок.
type cardState struct {
	id      string
	entries []history.Entry
	rows    []shown
	mtime   time.Time
	cursor  int
	open    map[string]bool // раскрытые серии "N edits"
	loaded  bool            // история пришла хотя бы раз
}

// shown - строка истории на экране. group - индекс серии в entries, -1 если запись одиночная.
type shown struct {
	entry history.Entry
	group int
}

// historyMsg - история карточки из git и комментариев файла.
type historyMsg struct {
	id      string
	entries []history.Entry
	mtime   time.Time // запасная дата изменения, если истории нет
	err     error
}

// openCard открывает карточку выбранной задачи; история грузится в фоне.
func (m *Model) openCard() tea.Cmd {
	t := m.selected()
	if t == nil {
		return nil
	}
	m.showing = true
	m.card = cardState{id: t.ID, open: map[string]bool{}}
	return m.historyCmd(t)
}

// historyCmd читает историю задачи; первый кадр карточки её не ждёт.
func (m *Model) historyCmd(t *task.Task) tea.Cmd {
	return m.guard(func() tea.Msg {
		var mtime time.Time
		if info, err := os.Stat(t.Path); err == nil {
			mtime = info.ModTime()
		}
		bin, err := gitx.Resolve(m.d.GitBin)
		if err != nil {
			return historyMsg{id: t.ID, mtime: mtime, err: err}
		}
		es, err := history.Load(gitx.Repo{Dir: m.d.Root, Bin: bin}, t)
		return historyMsg{id: t.ID, entries: es, mtime: mtime, err: err}
	})
}

// set принимает историю и ставит курсор на последнюю запись при первой загрузке.
func (c *cardState) set(msg historyMsg) {
	c.entries, c.mtime = msg.entries, msg.mtime
	c.rebuild()
	if !c.loaded {
		c.cursor = max(len(c.rows)-1, 0)
	}
	c.loaded = true
	c.cursor = min(c.cursor, max(len(c.rows)-1, 0))
}

func (m *Model) setHistoryMsg(msg historyMsg) {
	if !m.showing || m.card.id != msg.id {
		return
	}
	m.card.set(msg)
	if msg.err != nil {
		m.setStatus("history: "+firstLine(msg.err.Error()), true)
	}
}

// rebuild раскладывает записи в строки экрана: раскрытая серия заменяется своими правками.
func (c *cardState) rebuild() {
	c.rows = c.rows[:0]
	for i, e := range c.entries {
		switch {
		case len(e.Folded) == 0:
			c.rows = append(c.rows, shown{entry: e, group: -1})
		case c.open[groupKey(e)]:
			for _, f := range e.Folded {
				c.rows = append(c.rows, shown{entry: f, group: i})
			}
		default:
			c.rows = append(c.rows, shown{entry: e, group: i})
		}
	}
}

func groupKey(e history.Entry) string {
	return fmt.Sprintf("%d %s", e.Folded[0].At.UnixNano(), e.Actor)
}

// changed - время последней записи истории (лента идёт от старых к новым), без истории - mtime файла.
func (c *cardState) changed() time.Time {
	if len(c.entries) == 0 {
		return c.mtime
	}
	return c.entries[len(c.entries)-1].At
}

// step двигает курсор по истории.
func (c *cardState) step(d int) {
	c.cursor = min(max(c.cursor+d, 0), max(len(c.rows)-1, 0))
}

// toggle раскрывает серию правок под курсором или сворачивает раскрытую.
func (c *cardState) toggle() {
	if c.cursor >= len(c.rows) || c.rows[c.cursor].group < 0 {
		return
	}
	g := c.rows[c.cursor].group
	key := groupKey(c.entries[g])
	c.open[key] = !c.open[key]
	c.rebuild()
	for i, r := range c.rows {
		if r.group == g {
			c.cursor = i
			return
		}
	}
}

// cardView собирает кадр карточки из текущего файла задачи.
func (m *Model) cardView() view.Card {
	c := view.Card{ID: m.card.id, Cursor: m.card.cursor, W: m.w, H: m.h}
	c.Status, c.IsErr = m.statusText()
	c.History = make([]history.Entry, len(m.card.rows))
	for i, r := range m.card.rows {
		c.History[i] = r.entry
	}
	t := m.byID(m.card.id)
	if t == nil {
		return c
	}
	c.Title, c.Column, c.Ask = t.Title, t.Status, strings.TrimSpace(t.Ask)
	c.Fields = fields(t, time.Now(), m.card.changed())
	c.Abstract, c.Summary = section(t.Body, "Abstract"), section(t.Body, "Summary")
	c.Details = section(t.Body, "Details")
	if crit := t.Criteria(); len(crit) > 0 {
		c.Checks = checks(crit)
	}
	return c
}

// fields - строка полей карточки: проект, внешняя связь, claim, ожидание, срок, теги, дата изменения.
func fields(t *task.Task, now time.Time, changed time.Time) string {
	parts := []string{t.Project}
	if t.External != "" {
		parts = append(parts, t.External)
	}
	if c, ok := t.ClaimInfo(); ok && c.Until.After(now) {
		parts = append(parts, "@"+actor.Short(c.Actor)+" until "+c.Until.Format("15:04"))
	}
	if strings.TrimSpace(t.Waiting) != "" {
		parts = append(parts, "waiting for: "+t.Waiting)
	}
	if t.Due != "" {
		parts = append(parts, "due "+dueText(t.Due))
	}
	if len(t.Tags) > 0 {
		parts = append(parts, "#"+strings.Join(t.Tags, " #"))
	}
	if !changed.IsZero() {
		parts = append(parts, "changed "+changed.Format("02.01 15:04"))
	}
	return strings.Join(parts, "   ")
}

// section возвращает текст раздела "## name" тела задачи без пустых краёв.
func section(body, name string) string {
	var out []string
	in := false
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimRight(ln, " \t\r")
		switch {
		case strings.HasPrefix(ln, "## "):
			if in {
				return strings.TrimSpace(strings.Join(out, "\n"))
			}
			in = strings.EqualFold(strings.TrimPrefix(ln, "## "), name)
		case in:
			out = append(out, ln)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
