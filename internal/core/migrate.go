package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Служебная папка прежнего имени проекта. Имя остаётся только здесь: миграция базы.
const legacyDir = ".tracker"

// migrateLegacyDir переименовывает .tracker в .tix, если .tix ещё нет, и заменяет
// записи .tracker/ в .gitignore базы. .gitignore правится первым: оборванный шаг не
// оставит файлы .tix/lock и .tix/log неигнорируемыми. Гонка двух процессов безопасна:
// проигравший видит .tix. Правки коммитит следующая запись ядра как внешние.
func migrateLegacyDir(root string) error {
	oldDir := filepath.Join(root, legacyDir)
	newDir := filepath.Join(root, tixDir)
	if _, err := os.Lstat(oldDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("migrate %s: %w", oldDir, err)
	}
	if _, err := os.Lstat(newDir); err == nil {
		return nil
	}
	if err := migrateGitignore(filepath.Join(root, ".gitignore")); err != nil {
		return err
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		if _, statErr := os.Lstat(newDir); statErr == nil {
			return nil
		}
		return fmt.Errorf("migrate %s to %s: %w", oldDir, newDir, err)
	}
	return nil
}

// migrateGitignore заменяет строки .tracker/... на .tix/... одним rename. Нет файла
// или нет старых строк - ничего не меняет.
func migrateGitignore(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // путь внутри базы, заданной владельцем
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	lines := strings.Split(string(data), "\n")
	changed := false
	for i, l := range lines {
		if strings.HasPrefix(l, legacyDir+"/") {
			lines[i] = tixDir + strings.TrimPrefix(l, legacyDir)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gitignore-*.tmp")
	if err != nil {
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	_, werr := tmp.WriteString(strings.Join(lines, "\n"))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), mode)); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	return nil
}
