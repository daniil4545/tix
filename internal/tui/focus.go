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

// focusReadLimit - сколько байт файла фокуса читается: адрес короче.
const focusReadLimit = 256

// openURL открывает адрес вкладки терминала командой `open` без ожидания её конца.
func openURL(url string) error {
	cmd := exec.Command("open", url) //nolint:gosec,noctx // адрес проверен actor.ValidFocusURL; процесс не ждём
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // забрать код выхода, чтобы не копить зомби
	return nil
}

// startFocus переключает терминал на вкладку агента, который держит задачу (спека 4a):
// живой захват, файл фокуса владельца с проверенным адресом. Иначе строка состояния.
func (m *Model) startFocus() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	url := m.focusURL(t)
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

// focusURL - проверенный адрес вкладки владельца захвата; пусто, если захвата нет, он истёк,
// файла нет или адрес не того вида.
func (m *Model) focusURL(t *task.Task) string {
	c, ok := t.ClaimInfo()
	if !ok || !c.Until.After(m.now()) {
		return ""
	}
	name, dir := actor.FocusFile(c.Actor), actor.FocusDir(os.Getenv)
	if name == "" || dir == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, name)) //nolint:gosec // имя очищено actor.FocusFile
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
