package tui

import (
	tea "charm.land/bubbletea/v2"

	"tracker/internal/core"
)

// bodyTemplate - тело новой задачи в редакторе: шаблон из RULES.md, раздел "Ticket template".
const bodyTemplate = "## Abstract\n\n## Summary\n\n## Details\n\n- [ ] \n"

// defaultProject - проект новой задачи, если нет ни фильтра, ни проекта запуска.
const defaultProject = "personal"

// taskCreated - задача создана; редактор тела откроется следом.
type taskCreated struct{ op opDone }

// startNew спрашивает заголовок и создаёт задачу в колонке col доски.
func (m *Model) startNew(col int) tea.Cmd {
	if col < 0 || col >= len(m.board.Cols) {
		return nil
	}
	column := m.board.Cols[col].Name
	if column == unknownColumn {
		m.setStatus("new: unknown column", true)
		return nil
	}
	project := firstNonEmpty(m.project, m.d.Project, defaultProject)
	m.ask("title:", func(title string) tea.Cmd {
		return m.createTask(title, project, column)
	})
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// createTask ставит Create в очередь записи; успех приходит как taskCreated.
func (m *Model) createTask(title, project, column string) tea.Cmd {
	write := m.submit("new", "created", nil, func() (core.Result, error) {
		return m.d.Store.Create(title, project, column, m.d.Actor)
	})
	return func() tea.Msg {
		msg := write()
		if op, ok := msg.(opDone); ok && op.err == nil {
			return taskCreated{op}
		}
		return msg
	}
}

// created ставит курсор на новую задачу и открывает редактор на шаблоне тела.
// Выход без правки оставляет тело пустым: копия равна orig.
func (m *Model) created(msg taskCreated) tea.Cmd {
	id := msg.op.res.ID
	msg.op.id = id
	m.report(msg.op)
	m.selID = id
	return tea.Batch(m.loadCmd(), m.guard(func() tea.Msg {
		_, rev, err := m.d.Store.Get(id)
		if err != nil {
			return opDone{id: id, err: err}
		}
		path, err := m.writeCopy(id, bodyTemplate)
		if err != nil {
			return opDone{id: id, err: err}
		}
		return editReady{s: &editSession{id: id, path: path, rev: rev, orig: bodyTemplate}}
	}))
}
