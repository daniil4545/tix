package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
)

// bodyTemplate - the body of a new task in the editor: the template from RULES.md, section "Ticket template".
const bodyTemplate = "## Abstract\n\n## Summary\n\n## Details\n\n- [ ] \n"

// defaultProject - the project of a new task if there is no filter and no launch project.
const defaultProject = "personal"

// taskCreated - the task is created; the body editor opens next.
type taskCreated struct{ op opDone }

// startNew asks for the title and creates a task in column col of the board.
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

// createTask queues Create to the write queue; success arrives as taskCreated. A column whose entry
// check refuses a new task (ready, waiting, closed) gets it in the inbox column, the title is kept.
func (m *Model) createTask(title, project, column string) tea.Cmd {
	write := m.submit("new", "created", nil, func() (core.Result, error) {
		res, err := m.d.Store.Create(title, project, column, m.d.Actor)
		inbox, ok := m.d.Store.Config().ByRole("inbox")
		if !errors.Is(err, core.ErrRule) || !ok || inbox.Name == column {
			return res, err
		}
		res, err = m.d.Store.Create(title, project, inbox.Name, m.d.Actor)
		if err == nil {
			res.Info = "created in " + inbox.Name + ": add criteria, then move it to " + column
		}
		return res, err
	})
	return func() tea.Msg {
		msg := write()
		if op, ok := msg.(opDone); ok && op.err == nil {
			return taskCreated{op}
		}
		return msg
	}
}

// created puts the cursor on the new task and opens the editor on the body template.
// Leaving without an edit keeps the body empty: the copy equals orig.
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
