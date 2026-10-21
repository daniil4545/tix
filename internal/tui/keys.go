package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/task"
)

// Сообщения между фоновыми командами и Update.
type (
	watchStart struct{}            // пора запускать наблюдатель
	watchReady struct{ err error } // наблюдатель запущен или не смог
	fsEvent    struct{}            // в корне что-то изменилось
	reloadTick struct{}            // окно склейки событий закрылось
	loaded     struct {            // свежий список задач
		tasks   []*task.Task
		skipped int
	}
)

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	if m.input != nil {
		return m.inputKey(msg)
	}
	m.setStatus("", false)
	if k := msg.String(); k == "q" || k == "ctrl+c" {
		return tea.Quit
	}
	if m.showing {
		return m.cardKey(msg.String())
	}
	return m.boardKey(msg.String())
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
	case "k", "up":
		m.card.step(-1)
	case "enter":
		m.card.toggle()
	default:
		return m.taskKey(k)
	}
	return nil
}

// taskKey - клавиши, общие для доски и карточки: действие над текущей задачей.
func (m *Model) taskKey(k string) tea.Cmd {
	switch k {
	case "m":
		return m.startMove(1)
	case "M":
		return m.startMove(-1)
	case "e":
		return m.startEdit()
	case "c":
		return m.startComment()
	}
	return nil
}

// event разбирает не клавиатурные сообщения.
func (m *Model) event(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case watchStart:
		return m.guard(m.startWatch)
	case watchReady:
		if msg.err != nil {
			m.setStatus("live update off: "+firstLine(msg.err.Error()), true)
			return nil
		}
		return m.waitEvent()
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

// settle склеивает события файлов в одно перечитывание.
func (m *Model) settle() tea.Cmd {
	if m.pending {
		return nil
	}
	m.pending = true
	return tea.Tick(settleDelay, func(time.Time) tea.Msg { return reloadTick{} })
}
