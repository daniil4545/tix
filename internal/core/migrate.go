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

// migrateLegacyDir переименовывает .tracker в .tix, если .tix ещё нет, и переводит
// .gitignore базы на .tix. .gitignore правится первым: оборванный шаг не оставит файлы
// .tix/lock и .tix/log неигнорируемыми. Гонка двух процессов безопасна: проигравший
// видит .tix. Обе папки сразу (старый бинарь после миграции) - .tracker не трогается,
// .gitignore получает недостающие строки .tix. Правки коммитит следующая запись ядра.
func migrateLegacyDir(root string) error {
	oldDir := filepath.Join(root, legacyDir)
	newDir := filepath.Join(root, tixDir)
	if _, err := os.Lstat(oldDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("migrate %s: %w", oldDir, err)
	}
	_, err := os.Lstat(newDir)
	both := err == nil
	if err := migrateGitignore(root, !both); err != nil {
		return err
	}
	if both {
		return nil
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		if _, statErr := os.Lstat(newDir); statErr == nil {
			return nil
		}
		return fmt.Errorf("migrate %s to %s: %w", oldDir, newDir, err)
	}
	return nil
}

// migrateGitignore дописывает в .gitignore базы недостающие служебные строки .tix/ из
// gitignore ядра; replace ещё меняет строки .tracker/... и /.tracker/... на .tix. Нет файла -
// ничего не делает: файл создаст подготовка базы. Временный файл лежит в .tracker/tmp:
// после переноса это .tix/tmp, его чистит ядро.
func migrateGitignore(root string, replace bool) error {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path) //nolint:gosec // путь внутри базы, заданной владельцем
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	text := string(data)
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	changed := false
	have := map[string]bool{}
	for i, l := range lines {
		for _, pre := range []string{legacyDir + "/", "/" + legacyDir + "/"} {
			if replace && strings.HasPrefix(l, pre) {
				lines[i] = strings.Replace(l, legacyDir, tixDir, 1)
				changed = true
			}
		}
		have[strings.TrimPrefix(lines[i], "/")] = true
	}
	for _, l := range strings.Split(gitignore, "\n") {
		if strings.HasPrefix(l, tixDir+"/") && !have[l] {
			lines = append(lines, l)
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
	dir := filepath.Join(root, legacyDir, tmpDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, tmpPrefix+"gitignore-*")
	if err != nil {
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	_, werr := tmp.WriteString(strings.Join(lines, "\n") + "\n")
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), mode)); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("migrate %s: %w", path, err)
	}
	_ = os.Remove(dir) // пустая папка следа не оставляет; занятую ядро создаст и почистит само
	return nil
}
