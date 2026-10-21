package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Service folder of the former project name. The name stays only here: store migration.
const legacyDir = ".tracker"

// migrateLegacyDir renames .tracker to .tix if .tix does not exist yet, and moves the store
// .gitignore to .tix. .gitignore is edited first: an interrupted step will not leave the files
// .tix/lock and .tix/log unignored. A race of two processes is safe: the loser
// sees .tix. Both folders at once (an old binary after migration) - .tracker is not touched,
// .gitignore gets the missing .tix lines. The edits are committed by the next core write.
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

// migrateGitignore appends to the store .gitignore the missing service lines .tix/ from the
// core gitignore; replace also changes the lines .tracker/... and /.tracker/... to .tix. No file -
// does nothing: the file will be created by store preparation. The temp file lies in .tracker/tmp:
// after the move it is .tix/tmp, the core cleans it.
func migrateGitignore(root string, replace bool) error {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path) //nolint:gosec // path inside the store, set by the owner
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
	_ = os.Remove(dir) // an empty folder leaves no trace; a busy one will be created and cleaned by the core itself
	return nil
}
