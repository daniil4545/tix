package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"tracker/internal/gitx"
	"tracker/internal/task"
)

// txEnv - состояние операции под локом.
type txEnv struct {
	repo  gitx.Repo
	extra []string        // файлы, созданные подготовкой базы: идут в коммит операции
	skip  map[string]bool // они же: не external edit
}

const gitignore = `.tracker/tmp/
.tracker/lock
.tracker/log
.obsidian/workspace*
.trash/
*.swp
`

// withLock берёт flock, готовит базу (git, index.lock, config.toml, .gitignore) и зовёт fn.
// create: первая запись создаёт папку; иначе нет папки - ErrNotFound.
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
	if err := os.MkdirAll(filepath.Join(s.root, trackerDir, tmpDir), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", s.root, err)
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	tx, err := s.prepare(repo)
	if err != nil {
		return err
	}
	return fn(tx)
}

// lock берёт flock .tracker/lock; ждёт до s.wait. Снимается при смерти процесса.
func (s *Store) lock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(s.root, trackerDir, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	fd := int(f.Fd()) //nolint:gosec // дескриптор влезает в int
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

// prepare под локом: git init, снятие index.lock, чистка своих temp, .gitignore и config.toml.
func (s *Store) prepare(repo gitx.Repo) (*txEnv, error) {
	tx := &txEnv{repo: repo, skip: map[string]bool{}}
	if _, err := os.Stat(filepath.Join(s.root, ".git")); os.IsNotExist(err) {
		if err := repo.Init(); err != nil {
			return nil, fmt.Errorf("git init: %w", err)
		}
	}
	dropped, err := repo.DropGitLocks()
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
		{filepath.ToSlash(filepath.Join(trackerDir, configFile)), s.cfg.Render()},
	}
	for _, f := range files {
		path := filepath.Join(s.root, filepath.FromSlash(f.rel))
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := s.writeNew(path, f.data); err != nil {
			return nil, err
		}
		tx.extra = append(tx.extra, f.rel)
		tx.skip[f.rel] = true
	}
	return tx, nil
}

// cleanTmp удаляет temp-файлы ядра, оставшиеся от убитого процесса: под локом они ничьи.
func (s *Store) cleanTmp() {
	dir := filepath.Join(s.root, trackerDir, tmpDir)
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

// syncExternal коммитит правки мимо ядра (шаг 2 из 2.1): одним коммитом, строка на id.
// Упавший коммит не останавливает операцию: правки уйдут со следующей.
func (s *Store) syncExternal(tx *txEnv) error {
	changes, err := tx.repo.Status()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
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
		s.log.Warn("external edit not committed", "op", "external_edit", "paths", len(paths), "err", err)
	}
	return nil
}

func sortedIDs(ids map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return idNum(out[i]) < idNum(out[j]) })
	return out
}

// commit коммитит пути операции. Отказ git - Warning в ответе, правка уже в файле.
func (s *Store) commit(repo gitx.Repo, res *Result, paths []string, subject, actor string) {
	h, err := repo.Commit(paths, subject, actor)
	if err != nil {
		s.log.Warn("commit not recorded", "op", "commit", "id", res.ID, "err", err)
		res.Warning = warnCommit
		return
	}
	res.Commit = h
}

// modify - общий путь правки существующей задачи: чтение, mutate, temp, сверка, rename,
// коммит. Файл изменился с чтения - повтор с external edit, до maxAttempts.
func (s *Store) modify(id, actor, op string, mutate func(t *task.Task, curRev string) error) (Result, error) {
	var res Result
	err := s.withLock(false, func(tx *txEnv) error {
		for range maxAttempts {
			if err := s.syncExternal(tx); err != nil {
				return err
			}
			r, done, err := s.modifyOnce(tx, id, actor, op, mutate)
			if err != nil || done {
				res = r
				return err
			}
		}
		return errors.New("file changes during write, retry the command")
	})
	return res, err
}

// modifyOnce - одна попытка modify. done=false: файл изменился с чтения, нужен повтор.
func (s *Store) modifyOnce(tx *txEnv, id, actor, op string,
	mutate func(t *task.Task, curRev string) error) (Result, bool, error) {
	path, err := s.findPath(id)
	if err != nil {
		return Result{}, false, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // путь строит ядро
	if err != nil {
		return Result{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	t, err := task.Parse(path, data)
	if err != nil {
		return Result{}, false, err
	}
	rev := blobID(data)
	if err := mutate(t, rev); err != nil {
		return Result{}, false, err
	}
	out := t.Render()
	if bytes.Equal(out, data) {
		return Result{ID: id, Rev: rev}, true, nil
	}
	ok, err := s.replace(path, data, out)
	if err != nil || !ok {
		return Result{}, false, err
	}
	res := Result{ID: id, Rev: blobID(out)}
	paths := append([]string{filepath.Base(path)}, tx.extra...)
	s.commit(tx.repo, &res, paths, id+": "+op, actor)
	return res, true, nil
}

// mergeBody возвращает тело для SetBody. База совпала с файлом - новое тело как есть;
// иначе трёхстороннее слияние тел через git merge-file. Конфликт - ErrConflict.
func (s *Store) mergeBody(cur *task.Task, curRev, baseRev, body string) (string, error) {
	if baseRev == curRev {
		return body, nil
	}
	stale := fmt.Errorf("base %s not found in git, run tk show again", baseRev)
	if !revRe.MatchString(baseRev) {
		return "", stale
	}
	repo, err := s.git()
	if err != nil {
		return "", err
	}
	baseData, err := repo.Blob(baseRev)
	if err != nil {
		return "", stale
	}
	bt, err := task.Parse(cur.Path+" (base "+baseRev[:7]+")", baseData)
	if err != nil {
		return "", stale
	}
	switch {
	case same(body, bt.Body):
		return cur.Body, nil // пользователь тело не менял: чужая правка остаётся
	case same(cur.Body, bt.Body):
		return body, nil // тело в файле не менялось: правка пользователя целиком
	}
	// Тела сводятся к одному виду (без хвостовых пустых строк): раздел комментариев
	// и его отступ в слиянии не участвуют.
	out, conflict, err := repo.MergeFile([]byte(nl(cur.Body)), []byte(nl(bt.Body)), []byte(nl(body)))
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	if conflict {
		return "", fmt.Errorf("%w: body changed after tk show, reread and retry", ErrConflict)
	}
	return string(out), nil
}

func same(a, b string) bool { return strings.TrimRight(a, "\n") == strings.TrimRight(b, "\n") }

// nl приводит тело к одной завершающей новой строке; SetBody вернёт отступ перед комментариями.
func nl(s string) string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}
