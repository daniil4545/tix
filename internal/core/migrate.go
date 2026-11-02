package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/task"
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

var reColumnTable = regexp.MustCompile(`(?m)^[ \t]*\[\[column\]\]`)

// stripColumns drops the [[column]] tables of a config text; other sections and comments stay.
func stripColumns(text string) string {
	var out []string
	skip := false
	for _, l := range strings.Split(text, "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "[") {
			skip = strings.HasPrefix(t, "[[column]]")
		}
		if !skip {
			out = append(out, l)
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

// migrateStatuses renames task statuses by column role and drops [[column]], in one commit.
// No config.toml: the default names. A status in no column stays. Costs one file read when done.
func (s *Store) migrateStatuses() error {
	cfgPath := filepath.Join(s.root, tixDir, configFile)
	text, err := os.ReadFile(cfgPath) //nolint:gosec // path inside the store, set by the owner
	switch {
	case err == nil && !reColumnTable.Match(text):
		return nil
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("migrate %s: %w", cfgPath, err)
	case err != nil:
		if refs, _ := s.taskFiles(); len(refs) == 0 {
			return nil
		}
	}
	return s.withLock(true, func(tx *txEnv) error { return s.migrateLocked(tx, cfgPath) })
}

// migrateLocked is the migration under the lock: rename the statuses, drop [[column]], commit once.
func (s *Store) migrateLocked(tx *txEnv, cfgPath string) error {
	if err := s.syncExternal(tx); err != nil {
		return err
	}
	old, err := os.ReadFile(cfgPath) //nolint:gosec // see migrateStatuses
	// prepare may have just written config.toml: then there was none, and it is already in tx.extra
	exists := err == nil && !tx.skip[filepath.ToSlash(filepath.Join(tixDir, configFile))]
	if exists && !reColumnTable.Match(old) {
		return nil // another process migrated while we waited for the lock
	}
	if !exists {
		old = nil
	}
	names, _, err := config.LegacyMap(string(old))
	if err != nil {
		return fmt.Errorf("migrate %s: %w", cfgPath, err)
	}
	files, err := s.renameStatuses(names)
	if err != nil {
		return err
	}
	if exists {
		out := []byte(stripColumns(string(old)))
		if ok, err := s.replace(cfgPath, old, out); err != nil || !ok {
			return errors.Join(err, errors.New("config.toml changed during the migration, retry the command"))
		}
		files = append(files, gitx.File{Path: filepath.ToSlash(filepath.Join(tixDir, configFile)), Data: out})
	}
	var res Result
	s.commit(tx, &res, append(files, tx.extra...), "tix: migrate statuses to idea open in_progress blocked closed", "tix")
	if res.Warning != "" {
		return errors.New("migration commit failed: " + res.Warning)
	}
	return nil
}

// renameStatuses rewrites the status of every task file by the map and returns the written files.
func (s *Store) renameStatuses(names map[string]string) ([]gitx.File, error) {
	refs, err := s.taskFiles()
	if err != nil {
		return nil, err
	}
	var files []gitx.File
	for _, ref := range refs {
		data, err := os.ReadFile(ref.path) //nolint:gosec // the path is built by the core
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", ref.path, err)
		}
		t, err := task.Parse(ref.path, data)
		if err != nil {
			s.log.Warn("migrate: task skipped", "op", "migrate", "id", ref.id, "err", err)
			continue
		}
		to, ok := names[t.Status]
		if !ok {
			s.log.Warn("migrate: status in no column kept", "op", "migrate", "id", ref.id, "status", t.Status)
			continue
		}
		if to == t.Status {
			continue
		}
		t.Status = to
		out := t.Render()
		if ok, err := s.replace(ref.path, data, out); err != nil || !ok {
			return nil, errors.Join(err, fmt.Errorf("%s changed during the migration, retry the command", ref.id))
		}
		files = append(files, gitx.File{Path: filepath.Base(ref.path), Data: out})
	}
	return files, nil
}
