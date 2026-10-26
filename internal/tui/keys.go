package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/task"
)

// Messages between background commands and Update.
type (
	watchStart struct{}            // time to start the watcher
	watchReady struct{ err error } // the watcher started or failed
	fsEvent    struct{}            // something changed in the root
	reloadTick struct{}            // the event merge window closed
	loaded     struct {            // a fresh task list
		tasks   []*task.Task
		skipped int
	}
)

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	if m.input != nil {
		return m.inputKey(msg)
	}
	return m.dispatch(msg.String())
}

// dispatch runs key k; a click on a hint button calls it with the name of the same key.
func (m *Model) dispatch(k string) tea.Cmd {
	if m.grab != nil {
		return m.grabKey(k)
	}
	if m.help {
		return m.helpKey(k)
	}
	m.setStatus("", false)
	if k == "q" || k == "ctrl+c" {
		return tea.Quit
	}
	if m.showing {
		return m.cardKey(k)
	}
	return m.boardKey(k)
}

func (m *Model) boardKey(k string) tea.Cmd {
	switch k {
	case "h", "left":
		m.moveCursor(-1, 0)
	case "l", "right":
		m.moveCursor(1, 0)
	case "j", "down":
		m.moveCursor(0, 1)
	case "k", "up":
		m.moveCursor(0, -1)
	case "J":
		return m.reorderKey(1)
	case "K":
		return m.reorderKey(-1)
	case "o":
		m.toggleOrder()
	case "p":
		m.cycleProject()
	case "enter":
		return m.openCard()
	case "space":
		m.startGrab()
	case "n":
		return m.startNew(m.board.Col)
	case "a":
		return m.startAnswer()
	case "g":
		return m.startFocus()
	case "delete", "backspace":
		return m.startDelete()
	case "?":
		m.help = true
	default:
		return m.taskKey(k)
	}
	return nil
}

func (m *Model) cardKey(k string) tea.Cmd {
	switch k {
	case "esc":
		m.showing = false
	case "j", "down":
		m.card.step(1)
		m.fitScroll()
	case "k", "up":
		m.card.step(-1)
		m.fitScroll()
	case "enter":
		return m.cardEnter()
	case "left", "h":
		return m.siblingCard(-1)
	case "right", "l":
		return m.siblingCard(1)
	case "a":
		return m.startAnswer()
	case "f":
		return m.startField()
	case "g":
		return m.startFocus()
	case "delete", "backspace":
		return m.startDelete()
	case "?":
		m.help = true
	default:
		return m.taskKey(k)
	}
	return nil
}

// siblingCard opens the previous (dir -1) or next task of the same column; at the edge - nothing.
func (m *Model) siblingCard(dir int) tea.Cmd {
	for _, ts := range m.colTasks {
		for i, t := range ts {
			if t.ID != m.card.id {
				continue
			}
			if i+dir < 0 || i+dir >= len(ts) || !m.selectTask(ts[i+dir].ID) {
				return nil
			}
			return m.openCard()
		}
	}
	return nil
}

// helpKey: help closes with esc or ?; q and ctrl+c quit, anything else is ignored.
func (m *Model) helpKey(k string) tea.Cmd {
	switch k {
	case "esc", "?":
		m.help = false
	case "q", "ctrl+c":
		return tea.Quit
	}
	return nil
}

// taskKey - keys shared by the board and the card: an action on the current task.
func (m *Model) taskKey(k string) tea.Cmd {
	switch k {
	case "e":
		return m.startEdit()
	case "c":
		return m.startComment()
	}
	return nil
}

// event handles non-keyboard messages.
func (m *Model) event(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case watchStart:
		return m.guard(m.startWatch)
	case watchReady:
		if msg.err != nil {
			m.setStatus("live update off: "+firstLine(msg.err.Error()), true)
			return nil
		}
		// Edits between the first load and the watcher start gave no event: reload once.
		return tea.Batch(m.waitEvent(), m.settle())
	case fsEvent:
		return tea.Batch(m.waitEvent(), m.settle())
	case reloadTick:
		m.pending = false
		return m.loadCmd()
	case loaded:
		return m.afterLoad(msg)
	case notesLoaded:
		m.setNotes(msg)
	case opDone:
		return m.opResult(msg)
	case taskCreated:
		return m.created(msg)
	case historyMsg:
		m.setHistoryMsg(msg)
	case editReady:
		return runEditor(msg.s)
	case editorDone:
		return m.editFinished(msg)
	case failed:
		m.setStatus(firstLine(msg.err.Error()), true)
	}
	return nil
}

// settle merges file events into one reread.
func (m *Model) settle() tea.Cmd {
	if m.pending {
		return nil
	}
	m.pending = true
	return tea.Tick(settleDelay, func(time.Time) tea.Msg { return reloadTick{} })
}
