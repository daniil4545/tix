package tui

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/task"
)

// focusReadLimit - how many bytes of the focus file are read: an address is shorter.
const focusReadLimit = 256

// openURL opens the terminal tab address with the `open` command without waiting for it to end.
func openURL(url string) error {
	cmd := exec.Command("open", url) //nolint:gosec,noctx // the address is checked by actor.ValidFocusURL; the process is not awaited
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // collect the exit code to avoid piling up zombies
	return nil
}

// startFocus switches the terminal to the tab of the agent that holds the task:
// a live claim, the owner focus file with a checked address. Otherwise the status line.
func (m *Model) startFocus() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	url := m.focusURL(t)
	if url == "" && strings.TrimSpace(t.Ask) != "" && !m.liveClaim(t) {
		m.setStatus(t.ID+": session ended; the answer stays in the task", false)
		return nil
	}
	if url == "" {
		m.setStatus(t.ID+": no session to open", false)
		return nil
	}
	open := m.acts.open
	if open == nil {
		open = openURL
	}
	if err := open(url); err != nil {
		m.log.Warn("open session failed", "op", "focus", "id", t.ID, "err", err)
		m.setStatus(t.ID+": cannot open session: "+firstLine(err.Error()), true)
	}
	return nil
}

// liveClaim reports whether the task has a claim that is not expired by the model clock.
func (m *Model) liveClaim(t *task.Task) bool {
	c, ok := t.ClaimInfo()
	return ok && c.Until.After(m.now())
}

// focusURL - the checked tab address of the claim owner; empty if there is no claim, it expired,
// there is no file or the address is of the wrong form.
func (m *Model) focusURL(t *task.Task) string {
	if !m.liveClaim(t) {
		return ""
	}
	c, _ := t.ClaimInfo()
	name, dir := actor.FocusFile(c.Actor), actor.FocusDir(os.Getenv)
	if name == "" || dir == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, name)) //nolint:gosec // the name is cleaned by actor.FocusFile
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, focusReadLimit))
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(data))
	if !actor.ValidFocusURL(url) {
		return ""
	}
	return url
}
