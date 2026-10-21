package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/task"
)

// notesLoaded - последние заметки задач из git; err - git не ответил, пульс идёт без заметок.
type notesLoaded struct {
	notes map[string]gitx.Note
	err   error
}

// notesCmd читает заметки одним вызовом git на перечитывание доски.
func (m *Model) notesCmd() tea.Cmd {
	return m.guard(func() tea.Msg {
		bin, err := gitx.Resolve(m.d.GitBin)
		if err != nil {
			return notesLoaded{err: err}
		}
		notes, err := gitx.Repo{Dir: m.d.Root, Bin: bin}.Notes()
		return notesLoaded{notes: notes, err: err}
	})
}

// setNotes принимает заметки и перестраивает доску; ошибка git - заметок нет, доска работает.
func (m *Model) setNotes(msg notesLoaded) {
	if msg.err != nil {
		m.log.Warn("pulse notes", "err", gitx.Detail(msg.err))
	}
	m.notes = msg.notes
	m.rebuild()
}

// note - строка заметки для доски: возраст и текст; только для задачи в колонке active.
func (m *Model) note(t *task.Task, now time.Time) string {
	if m.cfg.RoleOf(t.Status) != "active" {
		return ""
	}
	n, ok := m.notes[t.ID]
	if !ok {
		return ""
	}
	return age(now.Sub(n.At)) + " " + firstLine(n.Text)
}

// age - возраст коротко: 5m, 3h, 2d; меньше минуты - 0m.
func age(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dm", max(int(d/time.Minute), 0))
	}
}
