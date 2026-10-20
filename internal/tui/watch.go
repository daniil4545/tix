package tui

import (
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// startWatch открывает наблюдатель за корнем базы. Его горутина живёт до конца ctx.
func (m *Model) startWatch() tea.Msg {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return watchReady{err: err}
	}
	if err := w.Add(m.d.Root); err != nil {
		_ = w.Close()
		return watchReady{err: err}
	}
	go m.pump(w)
	return watchReady{}
}

// pump передаёт события файлов задач в канал events (ёмкость 1: лишнее склеивается).
func (m *Model) pump(w *fsnotify.Watcher) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Error("watcher panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	defer func() { _ = w.Close() }()
	for {
		select {
		case <-m.done:
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if relevant(ev) {
				select {
				case m.events <- struct{}{}:
				default:
				}
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			m.log.Warn("watcher error", "err", err)
		}
	}
}

// relevant: изменение файла задачи. Папка .tracker и .git шума не дают, chmod не меняет текст.
func relevant(ev fsnotify.Event) bool {
	name := filepath.Base(ev.Name)
	return strings.HasPrefix(name, "T-") && strings.HasSuffix(name, ".md") &&
		ev.Op&^fsnotify.Chmod != 0
}

// waitEvent ждёт события наблюдателя; конец ctx снимает ожидание.
func (m *Model) waitEvent() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-m.events:
			return fsEvent{}
		case <-m.done:
			return nil
		}
	}
}

// loadCmd перечитывает все задачи; git не трогает.
func (m *Model) loadCmd() tea.Cmd {
	return m.guard(func() tea.Msg {
		tasks, errs := m.d.Store.List()
		return loaded{tasks: tasks, skipped: len(errs)}
	})
}

// afterLoad применяет список; открытая карточка обновляется или закрывается, если задачи нет.
func (m *Model) afterLoad(msg loaded) tea.Cmd {
	m.apply(msg.tasks, msg.skipped)
	if !m.showing {
		return nil
	}
	t := m.byID(m.card.id)
	if t == nil {
		m.showing = false
		m.setStatus(m.card.id+" removed", false)
		return nil
	}
	return m.historyCmd(t)
}
