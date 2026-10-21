package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/daniil4545/tix/internal/actor"
)

const focusEnv = "WARP_FOCUS_URL"

// recordFocus после успешной записи агента кладёт адрес его вкладки в actor.FocusDir (кэш пользователя, не база).
// Ошибка записи - Warn в лог, команда не падает.
func (a *app) recordFocus() {
	name := actor.FocusFile(a.who)
	url := a.env.Getenv(focusEnv)
	dir := actor.FocusDir(a.env.Getenv)
	if name == "" || !actor.ValidFocusURL(url) || dir == "" {
		return
	}
	if err := writeFocus(dir, name, url); err != nil {
		a.log.Warn("focus file not written", "op", "focus", "actor", a.who, "err", err)
	}
}

func writeFocus(dir, name, url string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("focus dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("focus temp: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(url + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("focus write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("focus close: %w", err)
	}
	// CreateTemp создаёт файл с 0600; chmod фиксирует это независимо от umask.
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("focus chmod: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("focus rename: %w", err)
	}
	return nil
}
