package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/project"
	"github.com/daniil4545/tix/internal/task"
)

// gcEvery - the number of commits between gc runs (docs/api/git.md, section 5: 200-500).
const gcEvery = 300

// codeTrailer returns the trailer Code: <project>@<HEAD sha> for the process working folder.
// A folder outside a project (inside the store, outside git) or no HEAD - an empty trailer, it is not written.
func (s *Store) codeTrailer() gitx.Trailer {
	cwd, err := os.Getwd()
	if err != nil {
		return gitx.Trailer{}
	}
	name, ok := project.Detect(cwd, s.root, s.cfg)
	if !ok {
		return gitx.Trailer{}
	}
	dir := repoDir(cwd)
	if dir == "" {
		return gitx.Trailer{}
	}
	sha, ok := project.Head(dir)
	if !ok {
		return gitx.Trailer{}
	}
	return gitx.Trailer{Key: "Code", Value: name + "@" + sha}
}

// modelTrailer returns the trailer Model: <TIX_MODEL>. A variable that is empty or fails
// validation gives an empty trailer, it is not written, the write does not fail.
func modelTrailer() gitx.Trailer {
	model := os.Getenv("TIX_MODEL")
	if !actor.ValidModel(model) {
		return gitx.Trailer{}
	}
	return gitx.Trailer{Key: "Model", Value: model}
}

// repoDir climbs from dir to the folder with .git (a directory or a worktree file).
func repoDir(dir string) string {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// countCommits adds n to the counter under the lock. true: gcEvery reached, time to run gc
// (after the lock is released). The counter lives in the lock file: it is already in .gitignore, and nobody
// reads the lock content. Not critical: a write failure only shifts gc, so it is written without temp and rename.
func (s *Store) countCommits(n int) bool {
	if n == 0 {
		return false
	}
	path := filepath.Join(s.root, tixDir, lockFile)
	cur := 0
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // the path is built by the core
		cur, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	total := cur + n
	due := total >= gcEvery
	if due {
		total %= gcEvery
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(total)+"\n"), 0o600); err != nil {
		s.log.Warn("gc counter not written", "op", "gc_counter", "err", err)
	}
	return due
}

// noteSinceMargin - how far before `created` the note search starts.
const noteSinceMargin = 24 * time.Hour

// TaskNotes returns the notes of task id from old to new. They live only in commit subjects,
// so the store format stays as is.
func (s *Store) TaskNotes(id string, created time.Time) ([]gitx.Note, error) {
	repo, err := s.git()
	if err != nil {
		return nil, err
	}
	// A note cannot predate its task: git stops the walk there. The margin covers clock skew.
	var since time.Time
	if !created.IsZero() {
		since = created.Add(-noteSinceMargin)
	}
	notes, err := repo.TaskNotes(id, since)
	if err != nil {
		return nil, fmt.Errorf("notes of %s: %w", id, err)
	}
	return notes, nil
}

// Note - a task note from the history, as gitx reads it.
type Note = gitx.Note

// Shown - the result of ShowNotes: Show output plus the notes, or the error of reading them.
type Shown struct {
	Task     *task.Task
	Rev      string
	Data     []byte
	Notes    []Note
	NotesErr error
}

// ShowNotes is Show plus TaskNotes. The notes query starts as soon as the file is parsed (it needs
// `created`) and runs beside the rev step: both spawn git, together they fit the 15 ms budget of show.
func (s *Store) ShowNotes(id string) (Shown, error) {
	var sh Shown
	done := make(chan struct{})
	t, rev, data, err := s.show(id, func(t *task.Task) {
		go func() {
			defer close(done)
			sh.Notes, sh.NotesErr = s.TaskNotes(id, t.Created)
		}()
	})
	if err != nil {
		return Shown{}, err // no goroutine started: parsed is called only after a successful parse
	}
	<-done
	sh.Task, sh.Rev, sh.Data = t, rev, data
	return sh, nil
}
