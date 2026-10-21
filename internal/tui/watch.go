package tui

import (
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// startWatch opens a watcher on the store root. Its goroutine lives until ctx ends.
func (m *Model) startWatch() tea.Msg {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return watchReady{err: err}
	}
	if err := w.Add(m.d.Root); err != nil {
		_ = w.Close()
		return watchReady{err: err}
	}
	// tix note - an empty commit: files do not change, only the HEAD log is visible.
	if err := w.Add(filepath.Join(m.d.Root, ".git", "logs")); err != nil {
		m.log.Warn("pulse watch off", "err", err)
	}
	go m.pump(w)
	return watchReady{}
}

// pump passes task file events to the events channel (capacity 1: extras are merged).
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

// relevant: a task file change or a HEAD log change (a commit without a file edit). Other things in .tix and .git
// give no noise, chmod does not change the text.
func relevant(ev fsnotify.Event) bool {
	if ev.Op&^fsnotify.Chmod == 0 {
		return false
	}
	name := filepath.Base(ev.Name)
	if name == "HEAD" {
		return filepath.Base(filepath.Dir(ev.Name)) == "logs"
	}
	return strings.HasPrefix(name, "T-") && strings.HasSuffix(name, ".md")
}

// waitEvent waits for a watcher event; the end of ctx lifts the wait.
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

// loadCmd rereads all tasks; it does not touch git (the pulse notes are read by notesCmd).
func (m *Model) loadCmd() tea.Cmd {
	return m.guard(func() tea.Msg {
		tasks, errs := m.d.Store.List()
		return loaded{tasks: tasks, skipped: len(errs)}
	})
}

// afterLoad applies the list; the open card is updated or closed if the task is gone.
func (m *Model) afterLoad(msg loaded) tea.Cmd {
	m.apply(msg.tasks, msg.skipped)
	if !m.showing {
		return m.notesCmd()
	}
	t := m.byID(m.card.id)
	if t == nil {
		m.showing = false
		m.setStatus(m.card.id+" removed", false)
		return m.notesCmd()
	}
	return tea.Batch(m.historyCmd(t), m.notesCmd())
}
