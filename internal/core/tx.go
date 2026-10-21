package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daniil4545/tix/internal/assets"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/task"
)

// txEnv - the operation state under the lock.
type txEnv struct {
	repo  gitx.Repo
	extra []gitx.File     // files created by store preparation: they go into the operation commit
	skip  map[string]bool // the same: not an external edit

	pending []pendingWrite // foreign files of the operation (renumbering), written after the main one
	commits int            // commits during the lock: feeds the gc counter
}

const gitignore = `.tix/tmp/
.tix/lock
.tix/log
.tix/connect-state.json
.obsidian/workspace*
.trash/
*.swp
`

// withLock takes flock, prepares the store (git, index.lock, config.toml, .gitignore) and calls fn.
// create: the first write creates the folder; otherwise no folder - ErrNotFound.
func (s *Store) withLock(create bool, fn func(tx *txEnv) error) error {
	if !create {
		if _, err := os.Stat(s.root); err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return fmt.Errorf("store %s: %w", s.root, err)
		}
	}
	repo, err := s.git()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.root, tixDir, tmpDir), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", s.root, err)
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	var once sync.Once
	release := func() { once.Do(unlock) }
	defer release()
	tx, err := s.prepare(repo)
	if err != nil {
		return err
	}
	err = fn(tx)
	due := s.countCommits(tx.commits)
	release()
	if due {
		if gcErr := repo.SpawnGC(); gcErr != nil {
			s.log.Warn("gc not started", "op", "spawn_gc", "err", gitx.Detail(gcErr))
		}
	}
	return err
}

// lock takes flock .tix/lock; waits up to s.wait. Released when the process dies.
func (s *Store) lock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(s.root, tixDir, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	fd := int(f.Fd()) //nolint:gosec // the descriptor fits into int
	deadline := time.Now().Add(s.wait)
	delay := time.Millisecond
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("lock: %w", err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%w: waited %s", ErrLocked, s.wait)
		}
		time.Sleep(delay)
		delay = min(delay*2, 10*time.Millisecond)
	}
}

// prepare under the lock: git init, removal of index.lock, cleanup of own temp files, .gitignore, config.toml and
// RULES.md. Existing files are not touched, even if the text differs.
func (s *Store) prepare(repo gitx.Repo) (*txEnv, error) {
	tx := &txEnv{repo: repo, skip: map[string]bool{}}
	if _, err := os.Stat(filepath.Join(s.root, ".git")); os.IsNotExist(err) {
		if err := repo.Init(); err != nil {
			return nil, err
		}
	}
	dropped, err := repo.DropGitLocks()
	if errors.Is(err, gitx.ErrBusy) {
		s.log.Warn("git lock is fresh", "op", "git_busy", "err", err)
		return nil, errGitBusy{}
	}
	if err != nil {
		return nil, fmt.Errorf("git locks: %w", err)
	}
	for _, p := range dropped {
		s.log.Warn("stale git lock removed", "op", "drop_index_lock", "path", p)
	}
	s.cleanTmp()
	files := []struct {
		rel  string
		data []byte
	}{
		{".gitignore", []byte(gitignore)},
		{filepath.ToSlash(filepath.Join(tixDir, configFile)), s.cfg.Render()},
		{rulesFile, []byte(assets.Rules)},
	}
	for _, f := range files {
		path := filepath.Join(s.root, filepath.FromSlash(f.rel))
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := s.writeNew(path, f.data); err != nil {
			return nil, err
		}
		tx.extra = append(tx.extra, gitx.File{Path: f.rel, Data: f.data})
		tx.skip[f.rel] = true
	}
	return tx, nil
}

// cleanTmp removes core temp files left by a killed process: under the lock they belong to no one.
func (s *Store) cleanTmp() {
	dir := filepath.Join(s.root, tixDir, tmpDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// syncExternal commits edits made around the core (step 2 of 2.1): one commit, one line per id.
// A failed commit does not stop the operation: the edits go with the next one.
func (s *Store) syncExternal(tx *txEnv) error {
	changes, err := tx.repo.Status()
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	var others, paths []string
	for _, c := range changes {
		if tx.skip[c.Path] {
			continue
		}
		paths = append(paths, c.Path)
		if m := fileIDRe.FindStringSubmatch(c.Path); m != nil {
			ids[m[1]] = true
		} else {
			others = append(others, c.Path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	sort.Strings(others)
	var lines []string
	for _, id := range sortedIDs(ids) {
		lines = append(lines, id+": external edit")
	}
	for _, p := range others {
		lines = append(lines, "external edit: "+p)
	}
	if _, err := tx.repo.Commit(paths, strings.Join(lines, "\n\n"), "external"); err != nil {
		s.log.Warn("external edit not committed", "op", "external_edit", "paths", len(paths), "err", gitx.Detail(err))
		return nil
	}
	tx.commits++
	return nil
}

func sortedIDs(ids map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return IDNum(out[i]) < IDNum(out[j]) })
	return out
}

// commit commits the bytes of the operation, not the tree files: a foreign write after rename does not go
// under actor. A git refusal is a Warning in the reply, the edit is already in the file.
func (s *Store) commit(tx *txEnv, res *Result, files []gitx.File, subject, actor string) {
	h, err := tx.repo.CommitFiles(files, subject, actor, s.codeTrailer(), modelTrailer())
	if err != nil {
		s.log.Warn("commit not recorded", "op", "commit", "id", res.ID, "err", gitx.Detail(err))
		res.Warning = warnCommit
		return
	}
	tx.commits++
	res.Commit = h
}

// opInfo - the operation name. kind tells note from the rest for the claim extension; subject goes into
// the commit subject after "T-N: ". mutate may refine subject (for example, where the move came from).
type opInfo struct{ kind, subject string }

// pendingWrite - an edit of a foreign file within the operation: the new rank of a neighbor on renumbering.
type pendingWrite struct {
	path     string
	old, new []byte
}

// modify - the common path to edit an existing task: read, mutate, temp, check, rename,
// commit. The file changed since the read - repeat with external edit, up to maxAttempts.
func (s *Store) modify(id, actor, op string, mutate func(t *task.Task, curRev string) error) (Result, error) {
	return s.modifyTx(id, actor, &opInfo{kind: op, subject: op}, func(_ *txEnv, t *task.Task, rev string) error {
		return mutate(t, rev)
	})
}

// modifyTx - modify where mutate sees txEnv: it may order writes of neighboring files.
func (s *Store) modifyTx(id, actor string, op *opInfo,
	mutate func(tx *txEnv, t *task.Task, curRev string) error) (Result, error) {
	var res Result
	err := s.withLock(false, func(tx *txEnv) error {
		for attempt := range maxAttempts {
			if err := s.syncExternal(tx); err != nil {
				return err
			}
			tx.pending = nil
			r, done, err := s.modifyOnce(tx, id, actor, op, attempt, mutate)
			if err != nil || done {
				res = r
				return err
			}
		}
		return errors.New("file changes during write, retry the command")
	})
	return res, err
}

// modifyOnce - one attempt of modify. done=false: the file changed since the read, a repeat is needed.
func (s *Store) modifyOnce(tx *txEnv, id, actor string, op *opInfo, attempt int,
	mutate func(tx *txEnv, t *task.Task, curRev string) error) (Result, bool, error) {
	path, err := s.findPath(id)
	if err != nil {
		return Result{}, false, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is built by the core
	if err != nil {
		return Result{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	t, err := task.Parse(path, data)
	if err != nil {
		return Result{}, false, err
	}
	rev := blobID(data)
	if err := mutate(tx, t, rev); err != nil {
		return Result{}, false, err
	}
	out := t.Render()
	if bytes.Equal(out, data) {
		return Result{ID: id, Rev: rev}, true, nil
	}
	// Claim extension: mutate changed the file, set now+3 h in the same commit.
	if s.extendClaim(t, actor, op.kind) {
		out = t.Render()
	}
	if s.afterRead != nil {
		s.afterRead(path, attempt)
	}
	ok, err := s.replace(path, data, out)
	if err != nil || !ok {
		return Result{}, false, err
	}
	res := Result{ID: id, Rev: blobID(out)}
	files := append([]gitx.File{{Path: filepath.Base(path), Data: out}}, s.writePending(tx)...)
	files = append(files, tx.extra...)
	s.commit(tx, &res, files, id+": "+op.subject, actor)
	return res, true, nil
}

// writePending writes the neighboring files of the operation after the main one. A neighbor changed since the read -
// its rank stays as it was (the column order does not break: renumbering preserves it).
func (s *Store) writePending(tx *txEnv) []gitx.File {
	var files []gitx.File
	for _, p := range tx.pending {
		ok, err := s.replace(p.path, p.old, p.new)
		if err != nil || !ok {
			s.log.Warn("rank of neighbour not renumbered", "op", "renumber", "path", p.path, "err", err)
			continue
		}
		files = append(files, gitx.File{Path: filepath.Base(p.path), Data: p.new})
	}
	return files
}

// mergeBody returns the body for SetBody. The base matches the file - the new body as is;
// otherwise a three-way merge of bodies via git merge-file. A conflict - *ConflictError.
func (s *Store) mergeBody(cur *task.Task, curRev, baseRev, body string) (string, error) {
	if baseRev == curRev {
		return body, nil
	}
	stale := fmt.Errorf("base %s not found in git, run tix show again", baseRev)
	if !revRe.MatchString(baseRev) {
		return "", stale
	}
	repo, err := s.git()
	if err != nil {
		return "", err
	}
	baseData, err := repo.Blob(baseRev)
	if err != nil {
		s.log.Warn("base blob not read", "op", "cat_file", "rev", baseRev, "err", gitx.Detail(err))
		return "", stale
	}
	bt, err := task.Parse(cur.Path+" (base "+baseRev[:7]+")", baseData)
	if err != nil {
		return "", stale
	}
	switch {
	case same(body, bt.Body):
		return cur.Body, nil // the user did not change the body: the foreign edit stays
	case same(cur.Body, bt.Body):
		return body, nil // the body in the file did not change: the user's edit in full
	}
	// Bodies are reduced to one form (no trailing empty lines): the comments section
	// and its indent do not take part in the merge.
	out, conflict, err := repo.MergeFile([]byte(nl(cur.Body)), []byte(nl(bt.Body)), []byte(nl(body)))
	if err != nil {
		return "", err
	}
	if conflict {
		return "", &ConflictError{Rev: curRev, Merged: string(out)}
	}
	return string(out), nil
}

func same(a, b string) bool { return strings.TrimRight(a, "\n") == strings.TrimRight(b, "\n") }

// nl brings the body to one trailing newline; SetBody restores the indent before the comments.
func nl(s string) string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

// extendClaim extends the owner's claim: the actor matched, the expiry does not matter; op "note" does not extend.
// true: the task changed, a new Render is needed.
func (s *Store) extendClaim(t *task.Task, actor, op string) bool {
	if op == "note" {
		return false
	}
	c, ok := t.ClaimInfo()
	if !ok || c.Actor != actor {
		return false
	}
	t.SetClaim(task.Claim{Actor: actor, Until: s.now().Add(claimTTL)})
	return true
}
