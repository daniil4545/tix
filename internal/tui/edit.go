package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tracker/internal/core"
)

// editSession - одна правка тела в $EDITOR. rev - версия файла, против которой править;
// orig - текст в копии на момент запуска редактора.
type editSession struct {
	id, path, rev, orig string
	conflicted          bool
}

type (
	editReady  struct{ s *editSession } // копия готова, можно запускать редактор
	editorDone struct {                 // редактор закрыт
		s   *editSession
		err error
	}
)

const conflictMarker = "<<<<<<<"

// startEdit читает задачу с её rev и кладёт копию тела в .tracker/tmp.
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

// writeCopy создаёт .tracker/tmp/ed-<id>-<n>.md (0600) с текстом; n первый свободный.
func (m *Model) writeCopy(id, text string) (string, error) {
	dir := filepath.Join(m.d.Root, ".tracker", "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("editor copy: %w", err)
	}
	for n := 1; ; n++ {
		path := filepath.Join(dir, fmt.Sprintf("ed-%s-%d.md", id, n))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // путь строит TUI
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

// runEditor запускает $VISUAL, $EDITOR или vi на копии: без оболочки, делится по пробелам.
func runEditor(s *editSession) tea.Cmd {
	args := []string{"vi"}
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if f := strings.Fields(os.Getenv(key)); len(f) > 0 {
			args = f
			break
		}
	}
	cmd := exec.Command(args[0], append(args[1:], s.path)...) //nolint:gosec,noctx // редактор задаёт владелец окружения; жизнью процесса правит Bubble Tea
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDone{s: s, err: err} })
}

// editFinished сверяет копию с тем, что было, и ставит правку в очередь.
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
	switch {
	case strings.Contains(text, conflictMarker):
		m.keepCopy(s, "conflict markers left")
		return nil
	case !s.conflicted && same(text, s.orig):
		_ = os.Remove(s.path)
		m.setStatus(s.id+" no changes", false)
		return nil
	}
	return m.submit(s.id, "edit saved", s, func() (core.Result, error) {
		return m.d.Store.Edit(s.id, s.rev, text, m.d.Actor)
	})
}

// editResult разбирает итог записи правки: успех, конфликт (редактор снова) или отказ.
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

// keepCopy оставляет копию и называет её путь в строке ошибки.
func (m *Model) keepCopy(s *editSession, reason string) {
	m.setStatus(fmt.Sprintf("%s: %s, copy kept: %s", s.id, reason, s.path), true)
}

func same(a, b string) bool { return strings.TrimRight(a, "\n") == strings.TrimRight(b, "\n") }
