package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/gitx"
)

// CatalogEntry - one project of the catalog. Path is empty for a project with no folder;
// Known is false for a project that only tasks mention, not [paths].
type CatalogEntry struct {
	Name, Path string
	Tasks      int
	Known      bool
}

// Catalog lists the projects: the names registered in [paths] and the projects of the tasks, by name.
func (s *Store) Catalog() []CatalogEntry {
	count := map[string]int{}
	tasks, _ := s.List()
	for _, t := range tasks {
		if t.Project == "" {
			continue
		}
		if s.kindOf(t.Status) != config.Closed {
			count[t.Project]++
		} else {
			count[t.Project] += 0
		}
	}
	for n := range s.conf().Paths {
		count[n] += 0
	}
	out := make([]CatalogEntry, 0, len(count))
	for n, c := range count {
		p, known := s.conf().Paths[n]
		out = append(out, CatalogEntry{Name: n, Path: p, Tasks: c, Known: known})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SetProjectPath stores name (lower case) and an absolute path in [paths] of config.toml, in one commit.
// Empty path: a project with no folder. Same path or known name with empty path: ok, no commit. Other path: ErrRule.
func (s *Store) SetProjectPath(name, path, actor string) (Result, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Result{}, rule("project name is empty")
	}
	if path != "" {
		if !filepath.IsAbs(path) {
			return Result{}, rule("project path %q is not absolute", path)
		}
		path = filepath.Clean(path)
	}
	var res Result
	err := s.withLock(true, func(tx *txEnv) error {
		if err := s.syncExternal(tx); err != nil {
			return err
		}
		for range maxAttempts {
			done, err := s.setPathOnce(tx, &res, name, path, actor)
			if err != nil || done {
				return err
			}
		}
		return errors.New("config.toml changes during write, retry the command")
	})
	return res, err
}

// setPathOnce is one attempt under the lock. done=false: config.toml changed since the read.
func (s *Store) setPathOnce(tx *txEnv, res *Result, name, path, actor string) (bool, error) {
	cfgRel := filepath.ToSlash(filepath.Join(tixDir, configFile))
	cfgPath := filepath.Join(s.root, tixDir, configFile)
	// bytes first, parse second: an edit between the two is caught by replace and retried
	old, err := os.ReadFile(cfgPath) //nolint:gosec // path inside the store, set by the owner
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read %s: %w", cfgPath, err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return false, err
	}
	cur, known := cfg.Paths[name]
	if known && (path == "" || cur == path) {
		s.cfg.Store(cfg)
		return true, nil
	}
	if known && cur != "" {
		return false, rule("project %s is already at %s, not %s", name, cur, path)
	}
	cfg.Paths[name] = path
	out := cfg.Render()
	if ok, err := s.replace(cfgPath, old, out); err != nil || !ok {
		return false, err
	}
	s.cfg.Store(cfg)
	files := []gitx.File{{Path: cfgRel, Data: out}}
	for _, f := range tx.extra { // prepare's own config.toml is older than ours
		if f.Path != cfgRel {
			files = append(files, f)
		}
	}
	s.commit(tx, res, files, "tix: project "+name+" at "+path, actor)
	return true, nil
}

// checkKnownProject refuses a project outside [paths] and not "personal". An empty catalog: no check.
func (s *Store) checkKnownProject(project string) error {
	if len(s.conf().Paths) == 0 || project == defaultProject {
		return nil
	}
	if _, ok := s.conf().Paths[strings.ToLower(project)]; ok {
		return nil
	}
	known := []string{defaultProject}
	for n := range s.conf().Paths {
		if n != defaultProject { // personal is always allowed: one row, not two
			known = append(known, n)
		}
	}
	sort.Strings(known)
	return rule("unknown project %q; known: %s; register it: tix projects add %s PATH",
		project, strings.Join(known, ", "), project)
}
