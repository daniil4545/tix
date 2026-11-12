package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/task"
)

// formatParent is the store format with the field parent instead of epic lines (spec 0.9.0, 4).
const formatParent = 2

var reFormatKey = regexp.MustCompile(`(?m)^format\s*=.*$`)

// withFormat sets the top-level `format = 2` in a config text; comments and tables stay.
func withFormat(text string) string {
	head, rest := text, ""
	if loc := regexp.MustCompile(`(?m)^\s*\[`).FindStringIndex(text); loc != nil {
		head, rest = text[:loc[0]], text[loc[0]:]
	}
	if reFormatKey.MatchString(head) {
		return reFormatKey.ReplaceAllString(head, "format = 2") + rest
	}
	return "format = 2\n" + head + rest
}

type snapTask struct {
	ref  taskRef
	data []byte
	t    *task.Task
}

// parentPlan - the result of the rules 1-3 on the snapshot.
type parentPlan struct {
	epics   []*snapTask       // the tagged tasks, ascending
	parent  map[string]string // linked T-N -> chosen epic
	several []string          // T-N listed in several epics
	kept    []string          // T-N of rule 3
}

// readSnapshot reads every task file before the first write; a file that does not parse is skipped.
func (s *Store) readSnapshot() (tasks []*snapTask, bad []string, err error) {
	refs, err := s.taskFiles()
	if err != nil {
		return nil, nil, err
	}
	for _, ref := range refs {
		data, err := os.ReadFile(ref.path) //nolint:gosec // the path is built by the core
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", ref.path, err)
		}
		t, err := task.Parse(ref.path, data)
		if err != nil {
			s.log.Warn("migrate: task skipped", "op", "migrate", "id", ref.id, "err", err)
			bad = append(bad, ref.id)
			continue
		}
		tasks = append(tasks, &snapTask{ref: ref, data: data, t: t})
	}
	return tasks, bad, nil
}

// epicLines - the distinct tasks an epic lists, other than itself.
func epicLines(st *snapTask) []string {
	var out []string
	for _, c := range st.t.Criteria() {
		if n := task.LineTask(c.Text); n != "" && n != st.ref.id && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// planLinks applies the rules 1-3 of the spec to the snapshot.
func planLinks(tasks []*snapTask) parentPlan {
	snap := map[string]*snapTask{}
	children := map[string]bool{} // tasks that already have a child
	ownLines := map[string]bool{} // epics with task lines
	listers := map[string][]string{}
	p := parentPlan{parent: map[string]string{}}
	for _, st := range tasks {
		snap[st.ref.id] = st
		if st.t.Parent != "" {
			children[st.t.Parent] = true
		}
	}
	for _, st := range tasks {
		if !slices.Contains(st.t.Tags, "epic") { // the old epic sign
			continue
		}
		p.epics = append(p.epics, st)
		own := epicLines(st)
		ownLines[st.ref.id] = len(own) > 0
		for _, n := range own {
			listers[n] = append(listers[n], st.ref.id)
		}
	}
	for n, ls := range listers {
		if len(ls) > 1 {
			p.several = append(p.several, n)
		}
		chosen := ls[len(ls)-1]
		target := snap[n]
		if target != nil && slices.Contains(ls, target.t.Parent) {
			chosen = target.t.Parent
		}
		if target == nil || ownLines[n] || children[n] || (target.t.Parent != "" && target.t.Parent != chosen) {
			p.kept = append(p.kept, n) // rule 3
			continue
		}
		p.parent[n] = chosen
	}
	sortIDs(p.several)
	sortIDs(p.kept)
	return p
}

// report - the one line about the migration.
func (p parentPlan) report(bad []string) string {
	msg := fmt.Sprintf("migrate: epic lines to parent: %d epics, %d children", len(p.epics), len(p.parent))
	for _, part := range []struct {
		name string
		ids  []string
	}{{"in several epics", p.several}, {"kept as criteria", p.kept}, {"skipped files", bad}} {
		if len(part.ids) > 0 {
			msg += "; " + part.name + ": " + strings.Join(part.ids, " ")
		}
	}
	return msg
}

// migrateParent turns the epic lines `- [ ] T-N` into the field parent. It runs under the lock before
// the first write of a store without `format = 2`; one commit, config.toml is written last.
func (s *Store) migrateParent(tx *txEnv) error {
	if err := s.syncExternal(tx); err != nil {
		return err
	}
	tasks, bad, err := s.readSnapshot()
	if err != nil {
		return err
	}
	p := planLinks(tasks)
	snap := map[string]*snapTask{}
	for _, st := range tasks {
		snap[st.ref.id] = st
	}
	var files []gitx.File
	write := func(st *snapTask) error {
		out := st.t.Render()
		if string(out) == string(st.data) {
			return nil
		}
		if ok, err := s.replace(st.ref.path, st.data, out); err != nil || !ok {
			return errors.Join(err, fmt.Errorf("%s changed during the migration, retry the command", st.ref.id))
		}
		st.data = out // a task that is both a child and an epic is written once
		files = append(files, gitx.File{Path: filepath.Base(st.ref.path), Data: out})
		return nil
	}
	linked := map[string]bool{}
	for n, parent := range p.parent {
		linked[n] = true
		snap[n].t.Parent = parent
	}
	for _, st := range p.epics {
		st.t.DropLines(linked)
		st.t.Tags = slices.DeleteFunc(st.t.Tags, func(tag string) bool { return tag == "epic" })
	}
	for _, n := range sortedIDsOf(p.parent) { // the children first
		if err := write(snap[n]); err != nil {
			return err
		}
	}
	for _, st := range p.epics { // ascending: the chosen (largest) epic is written last
		if err := write(st); err != nil {
			return err
		}
	}
	if err := s.writeFormat(tx, files, len(tasks)+len(bad) == 0); err != nil {
		return err
	}
	if len(p.epics) > 0 || len(bad) > 0 {
		sortIDs(bad)
		s.log.Warn(p.report(bad), "op", "migrate")
	}
	return nil
}

// writeFormat writes config.toml with `format = 2` last. A store with no task files has nothing to
// migrate: the config rides along with the next operation commit; otherwise one migration commit.
func (s *Store) writeFormat(tx *txEnv, files []gitx.File, empty bool) error {
	cfgPath := filepath.Join(s.root, tixDir, configFile)
	cfgRel := filepath.ToSlash(filepath.Join(tixDir, configFile))
	old, err := os.ReadFile(cfgPath) //nolint:gosec // path inside the store, set by the owner
	if err != nil {
		return fmt.Errorf("read %s: %w", cfgPath, err)
	}
	out := []byte(withFormat(string(old)))
	if ok, err := s.replace(cfgPath, old, out); err != nil || !ok {
		return errors.Join(err, errors.New("config.toml changed during the migration, retry the command"))
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	s.cfg.Store(cfg)
	cfgFile := gitx.File{Path: cfgRel, Data: out}
	if empty {
		tx.skip[cfgRel] = true
		tx.extra = append(slices.DeleteFunc(tx.extra, func(f gitx.File) bool { return f.Path == cfgRel }), cfgFile)
		return nil
	}
	extra := slices.DeleteFunc(slices.Clone(tx.extra), func(f gitx.File) bool { return f.Path == cfgRel })
	var res Result
	s.commit(tx, &res, append(append(files, extra...), cfgFile), "migrate: epic lines to parent", "tix")
	if res.Warning != "" {
		return errors.New("migration commit failed: " + res.Warning)
	}
	tx.extra = nil
	return nil
}

func sortedIDsOf(m map[string]string) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sortIDs(ids)
	return ids
}

func sortIDs(ids []string) {
	sort.Slice(ids, func(i, j int) bool { return IDNum(ids[i]) < IDNum(ids[j]) })
}
