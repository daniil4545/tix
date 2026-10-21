package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/core"
)

// editSession - one body edit in $EDITOR. rev - the file version to edit against;
// orig - the text in the copy at the editor start.
type editSession struct {
	id, path, rev, orig string
	conflicted          bool
}

type (
	editReady  struct{ s *editSession } // the copy is ready, the editor can start
	editorDone struct {                 // the editor is closed
		s   *editSession
		err error
	}
)

const conflictMarker = "<<<<<<<"

// startEdit reads the task with its rev and puts a copy of the body into .tix/tmp.
func (m *Model) startEdit() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	id := t.ID
	return m.guard(func() tea.Msg {
		cur, rev, err := m.d.Store.Get(id)
		if err != nil {
			return opDone{id: id, err: err}
		}
		path, err := m.writeCopy(id, cur.Body)
		if err != nil {
			return opDone{id: id, err: err}
		}
		return editReady{s: &editSession{id: id, path: path, rev: rev, orig: cur.Body}}
	})
}

// writeCopy creates .tix/tmp/ed-<id>-<n>.md (0600) with the text; n is the first free one.
func (m *Model) writeCopy(id, text string) (string, error) {
	dir := filepath.Join(m.d.Root, ".tix", "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("editor copy: %w", err)
	}
	for n := 1; ; n++ {
		path := filepath.Join(dir, fmt.Sprintf("ed-%s-%d.md", id, n))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the TUI builds the path
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("editor copy: %w", err)
		}
		_, err = f.WriteString(text)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(path)
			return "", fmt.Errorf("editor copy: %w", err)
		}
		return path, nil
	}
}

// runEditor starts $VISUAL, $EDITOR or vi on the copy: without a shell, split by spaces.
func runEditor(s *editSession) tea.Cmd {
	args := []string{"vi"}
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if f := strings.Fields(os.Getenv(key)); len(f) > 0 {
			args = f
			break
		}
	}
	cmd := exec.Command(args[0], append(args[1:], s.path)...) //nolint:gosec,noctx // the editor is set by the environment owner; Bubble Tea controls the process life
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDone{s: s, err: err} })
}

// editFinished compares the copy with what was and queues the edit.
func (m *Model) editFinished(msg editorDone) tea.Cmd {
	s := msg.s
	if msg.err != nil {
		m.keepCopy(s, "editor failed: "+firstLine(msg.err.Error()))
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		m.keepCopy(s, "read copy: "+firstLine(err.Error()))
		return nil
	}
	text := string(data)
	// After a conflict an untouched copy is a cancel, but in markers it is a human edit: the copy stays.
	switch {
	case same(text, s.orig) && s.conflicted:
		m.keepCopy(s, "edit cancelled")
		return nil
	case same(text, s.orig):
		_ = os.Remove(s.path)
		m.setStatus(s.id+" no changes", false)
		return nil
	case strings.Contains(text, conflictMarker):
		m.keepCopy(s, "conflict markers left")
		return nil
	}
	return m.submit(s.id, "edit saved", s, func() (core.Result, error) {
		return m.d.Store.Edit(s.id, s.rev, text, m.d.Actor)
	})
}

// editResult handles the outcome of an edit write: success, conflict (editor again) or refusal.
func (m *Model) editResult(msg opDone) tea.Cmd {
	s := msg.edit
	var ce *core.ConflictError
	switch {
	case msg.err == nil:
		_ = os.Remove(s.path)
		m.report(msg)
	case errors.As(msg.err, &ce):
		if err := os.WriteFile(s.path, []byte(ce.Merged), 0o600); err != nil {
			m.keepCopy(s, "write copy: "+firstLine(err.Error()))
			return nil
		}
		s.rev, s.orig, s.conflicted = ce.Rev, ce.Merged, true
		m.setStatus(s.id+": edit conflict, resolve the markers", true)
		return tea.Batch(runEditor(s), m.loadCmd())
	default:
		m.keepCopy(s, errText(msg.err))
	}
	return m.loadCmd()
}

// keepCopy keeps the copy; the error line starts with its path, the same line goes to the log.
func (m *Model) keepCopy(s *editSession, reason string) {
	lead := "copy kept: "
	// Narrow screen: the path matters more than the label, it is fully visible if shorter than the width.
	if m.w > 0 && len(lead)+len(s.path) > m.w && len(s.path) < m.w {
		lead = ""
	}
	line := fmt.Sprintf("%s%s - %s: %s", lead, s.path, s.id, reason)
	m.setStatus(line, true)
	m.log.Warn(line, "id", s.id, "path", s.path, "reason", reason)
}

func same(a, b string) bool { return strings.TrimRight(a, "\n") == strings.TrimRight(b, "\n") }
