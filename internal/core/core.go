// Package core - the only write path into ~/tasks: lock, temp file, rename, commit.
//
// The write algorithm is in docs/spec.md, 2.1: lock, commit of foreign edits (external edit), read,
// merge for edit, write to a temp file, check, rename, commit of the operation paths.
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

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/task"
)

// Core errors. Compare with errors.Is.
var (
	ErrConflict = errors.New("edit conflict")
	ErrLocked   = errors.New("store is busy")
	ErrNotFound = errors.New("task not found")
	// ErrNotRecorded - the commit failed and the operation lives only in the commit: a note.
	ErrNotRecorded = errors.New("note not recorded: the commit failed, see .tix/log; run the note again")
)

// errGitBusy - a fresh git lock file: the reply "git is busy", errors.Is(err, ErrLocked).
type errGitBusy struct{}

func (errGitBusy) Error() string        { return "git is busy" }
func (errGitBusy) Is(target error) bool { return target == ErrLocked }

const (
	defaultLockTimeout = 5 * time.Second
	maxAttempts        = 3
	claimTTL           = 3 * time.Hour
	defaultProject     = "personal"
	unknownActor       = "unknown"
	warnCommit         = "commit not recorded"

	tixDir     = ".tix"
	nextIDFile = "next_id"
	configFile = "config.toml"
	rulesFile  = "RULES.md"
	lockFile   = "lock"
	tmpDir     = "tmp"
	tmpPrefix  = "w-"
)

var (
	idRe     = regexp.MustCompile(`^T-[0-9]+$`)
	revRe    = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	fileIDRe = regexp.MustCompile(`^(T-[0-9]+)(?:[ -].*)?\.md$`)
)

// Options - settings of Open. LockTimeout <= 0 - 5 s (2.1). GitBin - the raw value of
// TIX_GIT, gitx.Resolve finds the path to git on the first git call.
type Options struct {
	LockTimeout time.Duration
	GitBin      string
	Log         *slog.Logger
	Now         func() time.Time // clock; nil - time.Now
}

// Result - the reply of a write operation. Warning: the commit failed, the edit is in the file.
// Info - the tail of the ok line ("todo > doing  @claude until 20:40").
type Result struct{ ID, Rev, Commit, Warning, Info string }

// Store - an open task store.
type Store struct {
	cfg  *config.Config
	root string
	wait time.Duration
	bin  string
	log  *slog.Logger
	now  func() time.Time

	// afterRead - a test seam: called between the file read and the check before rename
	// (step 4 of 2.1). nil in production.
	afterRead func(path string, attempt int)

	gitOnce sync.Once
	repo    gitx.Repo
	gitErr  error
}

// Open opens the store at root and reads the config. Creates nothing: an empty or
// nonexistent root is an empty store. The only edit is the migration of .tracker to .tix.
func Open(root string, o Options) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("store path %q: %w", root, err)
	}
	if err := migrateLegacyDir(abs); err != nil {
		return nil, err
	}
	cfg, err := config.Load(filepath.Join(abs, tixDir, configFile))
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

// Config returns the store config.
func (s *Store) Config() *config.Config { return s.cfg }

// git finds the git binary once per Store.
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

// Create creates a task in column project/column on behalf of actor.
func (s *Store) Create(title, project, column, actor string) (Result, error) {
	return s.CreateWithBody(title, project, column, "", actor)
}

// CreateWithBody creates a task with its body and checks the destination before writing.
func (s *Store) CreateWithBody(title, project, column, body, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	title = strings.TrimSpace(title)
	project = strings.ToLower(strings.TrimSpace(project))
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
		r, err := s.create(tx, title, project, column, body, actor)
		res = r
		return err
	})
	return res, err
}

func (s *Store) create(tx *txEnv, title, project, column, body, actor string) (Result, error) {
	cfg, err := config.Load(filepath.Join(s.root, tixDir, configFile))
	if err != nil {
		return Result{}, err
	}
	column, err = pickColumn(cfg, column)
	if err != nil {
		return Result{}, err
	}
	t := &task.Task{Title: title, Project: project, Status: column}
	if err := t.SetBody(body); err != nil {
		return Result{}, err
	}
	if err := s.checkNewEntry(cfg, t); err != nil {
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
	tasks, _ := s.List() // broken files do not affect the rank
	for _, t := range tasks {
		if t.Rank+1 > rank {
			rank = t.Rank + 1
		}
	}
	id := fmt.Sprintf("T-%d", num)
	t.ID, t.Rank = id, rank
	t.Created = time.Now().Truncate(time.Second)
	name := task.FileName(id, title)
	path := filepath.Join(s.root, name)
	if _, err := os.Stat(path); err == nil {
		return Result{}, fmt.Errorf("file %s already exists", path)
	}
	data := t.Render()
	idPath := filepath.Join(s.root, tixDir, nextIDFile)
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
		{Path: filepath.ToSlash(filepath.Join(tixDir, nextIDFile)), Data: nextID},
	}, tx.extra...)
	s.commit(tx, &res, written, id+": new", actor)
	return res, nil
}

// checkNewEntry applies the destination entry check before the task is written.
func (s *Store) checkNewEntry(cfg *config.Config, t *task.Task) error {
	column := t.Status
	role := cfg.RoleOf(column)
	if role != roleClosed {
		if _, err := s.enter(t, role, "", MoveOpts{}, ""); err == nil {
			return nil
		}
	}
	switch role {
	case roleWaiting:
		return rule("a new task has no waiting reason; create it, then run: tix move <id> %s -m <reason>", column)
	case roleClosed:
		return rule("a new task cannot start closed; create it, then run: tix done <id> -m <result>")
	}
	return rule("a new task has no criteria; pass a body with `- [ ]` lines under ## Details: tix new <title> --to %s --body-file body.md", column)
}

// Edit replaces the body; baseRev - the version the caller read.
func (s *Store) Edit(id, baseRev, body, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Result{}, errors.New("body is empty")
	}
	if baseRev == "" {
		return Result{}, errors.New("base required: rev from tix show")
	}
	return s.modify(id, actor, "edit", func(t *task.Task, curRev string) error {
		merged, err := s.mergeBody(t, curRev, baseRev, body)
		if err != nil {
			return err
		}
		return t.SetBody(merged)
	})
}

// Comment appends a comment.
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

// Note writes an empty commit T-N: note <text>.
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
		if data, err := os.ReadFile(path); err == nil { //nolint:gosec // the path is built by the core
			res.Rev = blobID(data)
		}
		s.commit(tx, &res, tx.extra, id+": note "+text, actor)
		if res.Warning != "" {
			return ErrNotRecorded
		}
		return nil
	})
	return res, err
}

// Remove deletes a task.
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
		// os.Remove, not gitx.Remove: removal from the index writes the operation commit.
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		res = Result{ID: id}
		files := append([]gitx.File{{Path: filepath.Base(path)}}, tx.extra...)
		s.commit(tx, &res, files, id+": rm", actor)
		return nil
	})
	return res, err
}

// Get reads a task and its rev, writes the blob to git (hash-object -w). A git refusal is a warn in the log,
// then rev is computed in Go (the same blob id).
func (s *Store) Get(id string) (*task.Task, string, error) {
	t, rev, _, err := s.Show(id)
	return t, rev, err
}

// Show - Get plus the file as read: rev is computed over the same bytes.
func (s *Store) Show(id string) (*task.Task, string, []byte, error) {
	return s.show(id, nil)
}

// show reads the task; parsed, if set, is called once the file is parsed, before the slow rev step.
func (s *Store) show(id string, parsed func(*task.Task)) (*task.Task, string, []byte, error) {
	path, err := s.findPath(id)
	if err != nil {
		return nil, "", nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is built by the core
	if err != nil {
		return nil, "", nil, fmt.Errorf("read %s: %w", path, err)
	}
	t, err := task.Parse(path, data)
	if err != nil {
		return nil, "", nil, err
	}
	if parsed != nil {
		parsed(t)
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

// pickColumn checks the column; an empty name - the inbox role column, else the first.
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

// checkActor substitutes unknown and rejects characters that break the comment marker.
func checkActor(actor string) (string, error) {
	if actor == "" {
		return unknownActor, nil
	}
	if strings.ContainsAny(actor, "|\r\n") || strings.Contains(actor, "-->") {
		return "", fmt.Errorf("bad actor %q: must not contain |, --> or a line break", actor)
	}
	return actor, nil
}
