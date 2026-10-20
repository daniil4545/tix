// Package core - единственный путь записи в ~/tasks: лок, временный файл, rename, коммит.
//
// Алгоритм записи - docs/spec.md, 2.1: лок, коммит чужих правок (external edit), чтение,
// слияние для edit, запись во временный файл, сверка, rename, коммит путей операции.
package core

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"tracker/internal/config"
	"tracker/internal/gitx"
	"tracker/internal/task"
)

// Ошибки ядра. Сравнивать через errors.Is.
var (
	ErrConflict = errors.New("edit conflict")
	ErrLocked   = errors.New("store is busy")
	ErrNotFound = errors.New("task not found")
)

// errGitBusy - свежий lock-файл git: ответ "git is busy", errors.Is(err, ErrLocked).
type errGitBusy struct{}

func (errGitBusy) Error() string        { return "git is busy" }
func (errGitBusy) Is(target error) bool { return target == ErrLocked }

const (
	defaultLockTimeout = 5 * time.Second
	maxAttempts        = 3
	defaultProject     = "personal"
	unknownActor       = "unknown"
	warnCommit         = "commit not recorded"

	trackerDir = ".tracker"
	nextIDFile = "next_id"
	configFile = "config.toml"
	lockFile   = "lock"
	tmpDir     = "tmp"
	tmpPrefix  = "w-"
)

var (
	idRe     = regexp.MustCompile(`^T-[0-9]+$`)
	revRe    = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	fileIDRe = regexp.MustCompile(`^(T-[0-9]+)(?:[ -].*)?\.md$`)
)

// Options - настройки Open. LockTimeout <= 0 - 5 с (2.1). GitBin - сырое значение
// TRACKER_GIT, путь к git находит gitx.Resolve при первом вызове git.
type Options struct {
	LockTimeout time.Duration
	GitBin      string
	Log         *slog.Logger
	Now         func() time.Time // часы; nil - time.Now
}

// Result - ответ операции записи. Warning: коммит упал, правка в файле.
// Info - хвост строки ok ("todo > doing  @claude until 20:40").
type Result struct{ ID, Rev, Commit, Warning, Info string }

// Store - открытая база задач.
type Store struct {
	cfg  *config.Config
	root string
	wait time.Duration
	bin  string
	log  *slog.Logger
	now  func() time.Time

	// afterRead - шов для тестов: вызывается между чтением файла и сверкой перед rename
	// (шаг 4 из 2.1). В работе nil.
	afterRead func(path string, attempt int)

	gitOnce sync.Once
	repo    gitx.Repo
	gitErr  error
}

// Open открывает базу в root и читает конфиг. Ничего не создаёт: пустой или
// несуществующий root - пустая база.
func Open(root string, o Options) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("store path %q: %w", root, err)
	}
	cfg, err := config.Load(filepath.Join(abs, trackerDir, configFile))
	if err != nil {
		return nil, err
	}
	s := &Store{cfg: cfg, root: abs, wait: o.LockTimeout, bin: o.GitBin, log: o.Log, now: o.Now}
	if s.now == nil {
		s.now = time.Now
	}
	if s.wait <= 0 {
		s.wait = defaultLockTimeout
	}
	if s.log == nil {
		s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return s, nil
}

// Config возвращает конфиг базы.
func (s *Store) Config() *config.Config { return s.cfg }

// git находит бинарь git один раз на Store.
func (s *Store) git() (gitx.Repo, error) {
	s.gitOnce.Do(func() {
		bin, err := gitx.Resolve(s.bin)
		if err != nil {
			s.gitErr = fmt.Errorf("git not found: %w", err)
			return
		}
		s.repo = gitx.Repo{Dir: s.root, Bin: bin}
	})
	return s.repo, s.gitErr
}

// Create создаёт задачу в колонке project/column от имени actor.
func (s *Store) Create(title, project, column, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	title = strings.TrimSpace(title)
	project = strings.TrimSpace(project)
	if title == "" || strings.ContainsAny(title+project, "\r\n") {
		return Result{}, errors.New("title is empty or has several lines")
	}
	if project == "" {
		project = defaultProject
	}
	var res Result
	err = s.withLock(true, func(tx *txEnv) error {
		if err := s.syncExternal(tx); err != nil {
			return err
		}
		r, err := s.create(tx, title, project, column, actor)
		res = r
		return err
	})
	return res, err
}

func (s *Store) create(tx *txEnv, title, project, column, actor string) (Result, error) {
	cfg, err := config.Load(filepath.Join(s.root, trackerDir, configFile))
	if err != nil {
		return Result{}, err
	}
	column, err = pickColumn(cfg, column)
	if err != nil {
		return Result{}, err
	}
	files, err := s.taskFiles()
	if err != nil {
		return Result{}, err
	}
	num, err := s.nextNum(files)
	if err != nil {
		return Result{}, err
	}
	rank := 1.0
	tasks, _ := s.List() // битые файлы на ранг не влияют
	for _, t := range tasks {
		if t.Rank+1 > rank {
			rank = t.Rank + 1
		}
	}
	id := fmt.Sprintf("T-%d", num)
	t := &task.Task{
		ID: id, Title: title, Project: project, Status: column, Rank: rank,
		Created: time.Now().Truncate(time.Second),
	}
	name := task.FileName(id, title)
	path := filepath.Join(s.root, name)
	if _, err := os.Stat(path); err == nil {
		return Result{}, fmt.Errorf("file %s already exists", path)
	}
	data := t.Render()
	idPath := filepath.Join(s.root, trackerDir, nextIDFile)
	nextID := []byte(fmt.Sprintf("%d\n", num+1))
	if err := s.writeNew(idPath, nextID); err != nil {
		return Result{}, err
	}
	if err := s.writeNew(path, data); err != nil {
		return Result{}, err
	}
	res := Result{ID: id, Rev: blobID(data)}
	written := append([]gitx.File{
		{Path: name, Data: data},
		{Path: filepath.ToSlash(filepath.Join(trackerDir, nextIDFile)), Data: nextID},
	}, tx.extra...)
	s.commit(tx.repo, &res, written, id+": new", actor)
	return res, nil
}

// Edit заменяет тело; baseRev - версия, которую читал вызывающий.
func (s *Store) Edit(id, baseRev, body, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Result{}, errors.New("body is empty")
	}
	if baseRev == "" {
		return Result{}, errors.New("base required: rev from tk show")
	}
	return s.modify(id, actor, "edit", func(t *task.Task, curRev string) error {
		merged, err := s.mergeBody(t, curRev, baseRev, body)
		if err != nil {
			return err
		}
		return t.SetBody(merged)
	})
}

// Comment дописывает комментарий.
func (s *Store) Comment(id, text, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Result{}, errors.New("comment text is empty")
	}
	return s.modify(id, actor, "comment", func(t *task.Task, _ string) error {
		t.AddComment(time.Now(), actor, text)
		return nil
	})
}

// Note пишет пустой коммит T-N: note <текст>.
func (s *Store) Note(id, text, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return Result{}, errors.New("note text is empty")
	}
	var res Result
	err = s.withLock(false, func(tx *txEnv) error {
		if err := s.syncExternal(tx); err != nil {
			return err
		}
		path, err := s.findPath(id)
		if err != nil {
			return err
		}
		res = Result{ID: id}
		if data, err := os.ReadFile(path); err == nil { //nolint:gosec // путь строит ядро
			res.Rev = blobID(data)
		}
		s.commit(tx.repo, &res, nil, id+": note "+text, actor)
		return nil
	})
	return res, err
}

// Remove удаляет задачу.
func (s *Store) Remove(id, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	var res Result
	err = s.withLock(false, func(tx *txEnv) error {
		if err := s.syncExternal(tx); err != nil {
			return err
		}
		path, err := s.findPath(id)
		if err != nil {
			return err
		}
		// os.Remove, а не gitx.Remove: удаление из индекса пишет коммит операции.
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		res = Result{ID: id}
		files := append([]gitx.File{{Path: filepath.Base(path)}}, tx.extra...)
		s.commit(tx.repo, &res, files, id+": rm", actor)
		return nil
	})
	return res, err
}

// Get читает задачу и её rev, пишет blob в git (hash-object -w). Отказ git - warn в лог,
// rev тогда считается в Go (тот же blob id).
func (s *Store) Get(id string) (*task.Task, string, error) {
	t, rev, _, err := s.Show(id)
	return t, rev, err
}

// Show - Get плюс файл как прочитан: rev посчитан по этим же байтам.
func (s *Store) Show(id string) (*task.Task, string, []byte, error) {
	path, err := s.findPath(id)
	if err != nil {
		return nil, "", nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // путь строит ядро
	if err != nil {
		return nil, "", nil, fmt.Errorf("read %s: %w", path, err)
	}
	t, err := task.Parse(path, data)
	if err != nil {
		return nil, "", nil, err
	}
	rev := blobID(data)
	if repo, err := s.git(); err != nil {
		s.log.Warn("hash-object skipped", "op", "hash_object", "id", id, "err", err)
	} else if h, err := repo.HashObject(data); err != nil {
		s.log.Warn("hash-object failed", "op", "hash_object", "id", id, "err", gitx.Detail(err))
	} else {
		rev = h
	}
	t.Rev, t.Path = rev, path
	return t, rev, data, nil
}

// pickColumn проверяет колонку; пустое имя - колонка роли inbox, иначе первая.
func pickColumn(cfg *config.Config, column string) (string, error) {
	if column != "" {
		for _, c := range cfg.Columns {
			if c.Name == column {
				return column, nil
			}
		}
		return "", fmt.Errorf("unknown column %q", column)
	}
	for _, c := range cfg.Columns {
		if c.Role == "inbox" {
			return c.Name, nil
		}
	}
	if len(cfg.Columns) == 0 {
		return "", errors.New("config has no columns")
	}
	return cfg.Columns[0].Name, nil
}

// checkActor подставляет unknown и отклоняет символы, ломающие маркер комментария.
func checkActor(actor string) (string, error) {
	if actor == "" {
		return unknownActor, nil
	}
	if strings.ContainsAny(actor, "|\r\n") || strings.Contains(actor, "-->") {
		return "", fmt.Errorf("bad actor %q: must not contain |, --> or a line break", actor)
	}
	return actor, nil
}
