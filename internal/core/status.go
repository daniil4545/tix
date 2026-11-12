package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/task"
)

// StatusInfo - one status of the config with the number of its tasks.
type StatusInfo struct {
	Name, Kind string
	Tasks      int
}

// Statuses lists the statuses in board order with their task counts. A broken file is not counted.
func (s *Store) Statuses() []StatusInfo {
	counts, _ := s.statusCounts()
	var out []StatusInfo
	for _, st := range s.conf().StatusList() {
		out = append(out, StatusInfo{st.Name, st.Kind, counts[st.Name]})
	}
	return out
}

// statusCounts counts tasks per status name and names the files that do not parse.
func (s *Store) statusCounts() (map[string]int, []string) {
	counts := map[string]int{}
	var bad []string
	refs, _ := s.taskFiles()
	for _, ref := range refs {
		data, err := os.ReadFile(ref.path) //nolint:gosec // the path is built by the core
		var t *task.Task
		if err == nil {
			t, err = task.Parse(ref.path, data)
		}
		if err != nil {
			bad = append(bad, filepath.Base(ref.path))
			continue
		}
		counts[t.Status]++
	}
	return counts, bad
}

// AddStatus adds a status after the last one of its kind.
func (s *Store) AddStatus(name, kind, actor string) (Result, error) {
	return s.statusOp(actor, "add "+name, func(cfg *config.Config) ([]gitx.File, error) {
		return nil, asRule(cfg.AddStatus(name, kind))
	})
}

// RenameStatus rewrites the task files of OLD first and renames the status in the config last, in one
// commit. After an interrupted run the same call finishes the job.
func (s *Store) RenameStatus(oldName, newName, actor string) (Result, error) {
	return s.statusOp(actor, "rename "+oldName+" "+newName, func(cfg *config.Config) ([]gitx.File, error) {
		if err := cfg.RenameStatus(oldName, newName); err != nil {
			return nil, rule("%s", err)
		}
		if _, bad := s.statusCounts(); len(bad) > 0 {
			return nil, unparsed(bad)
		}
		return s.renameStatuses(map[string]string{oldName: newName})
	})
}

// RemoveStatus drops a status that has no tasks and is not the last of its kind.
func (s *Store) RemoveStatus(name, actor string) (Result, error) {
	return s.statusOp(actor, "rm "+name, func(cfg *config.Config) ([]gitx.File, error) {
		if err := cfg.CheckStatus(name); err != nil {
			return nil, rule("%s", err)
		}
		counts, bad := s.statusCounts()
		if len(bad) > 0 {
			return nil, unparsed(bad)
		}
		if n := counts[name]; n > 0 {
			return nil, rule("status %s has %d tasks; tix status rename %s NEW, or tix move ID STATUS", name, n, name)
		}
		return nil, asRule(cfg.RemoveStatus(name))
	})
}

func asRule(err error) error {
	if err == nil {
		return nil
	}
	return rule("%s", err)
}

func unparsed(bad []string) error {
	return rule("task files do not parse, their status is unknown: %s", strings.Join(bad, " "))
}

// statusOp runs edit on the config under the lock and commits the config with the task files it wrote.
// edit returns rule errors for refusals. Another write to config.toml in the meantime: retry the command.
func (s *Store) statusOp(actor, what string, edit func(*config.Config) ([]gitx.File, error)) (Result, error) {
	var res Result
	err := s.withLock(true, func(tx *txEnv) error {
		if err := s.syncExternal(tx); err != nil {
			return err
		}
		cfgRel := filepath.ToSlash(filepath.Join(tixDir, configFile))
		cfgPath := filepath.Join(s.root, tixDir, configFile)
		old, err := os.ReadFile(cfgPath) //nolint:gosec // path inside the store, set by the owner
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s: %w", cfgPath, err)
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		files, err := edit(cfg)
		if err != nil {
			return err
		}
		out := cfg.Render()
		if ok, err := s.replace(cfgPath, old, out); err != nil || !ok {
			return errors.Join(err, errors.New("config.toml changes during write, retry the command"))
		}
		s.cfg.Store(cfg)
		files = append(files, gitx.File{Path: cfgRel, Data: out})
		for _, f := range tx.extra { // prepare's own config.toml is older than ours
			if f.Path != cfgRel {
				files = append(files, f)
			}
		}
		s.commit(tx, &res, files, "tix: status "+what, actor)
		return nil
	})
	return res, err
}
