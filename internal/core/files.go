package core

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // git blob id, not protection
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniil4545/tix/internal/task"
)

// taskRef - a task file in the store root.
type taskRef struct {
	id   string
	num  int
	path string
}

// blobID computes the git blob id (sha1 of "blob <length>\0" and the content).
func blobID(data []byte) string {
	h := sha1.New() //nolint:gosec // git blob id, not protection
	_, _ = fmt.Fprintf(h, "blob %d\x00", len(data))
	_, _ = h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// IDNum - the task number from an id like T-12.
func IDNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "T-"))
	return n
}

// taskFiles lists T-*.md in the root, in ascending number order. No folder - empty.
func (s *Store) taskFiles() ([]taskRef, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", s.root, err)
	}
	var refs []taskRef
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "T-") {
			continue
		}
		if m := fileIDRe.FindStringSubmatch(e.Name()); m != nil {
			refs = append(refs, taskRef{id: m[1], num: IDNum(m[1]), path: filepath.Join(s.root, e.Name())})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].num < refs[j].num })
	return refs, nil
}

// findPath looks for a task file by id.
func (s *Store) findPath(id string) (string, error) {
	if !idRe.MatchString(id) {
		return "", ErrNotFound
	}
	refs, err := s.taskFiles()
	if err != nil {
		return "", err
	}
	for _, r := range refs {
		if r.id == id {
			return r.path, nil
		}
	}
	return "", ErrNotFound
}

// nextNum: max(next_id, max file number + 1). A broken next_id is an error, the file stays intact.
func (s *Store) nextNum(refs []taskRef) (int, error) {
	num := 1
	if n := len(refs); n > 0 {
		num = refs[n-1].num + 1
	}
	path := filepath.Join(s.root, tixDir, nextIDFile)
	data, err := os.ReadFile(path) //nolint:gosec // the path is built by the core
	switch {
	case err == nil:
		n, perr := strconv.Atoi(strings.TrimSpace(string(data)))
		if perr != nil || n < 1 {
			return 0, fmt.Errorf("%s:1: not a number", path)
		}
		num = max(num, n)
	case !os.IsNotExist(err):
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	return num, nil
}

// listWorkers - the number of parse goroutines in List (stage 0 measurement: 4 goroutines 11 ms, sequential 24 ms).
const listWorkers = 4

// List reads all tasks without git. Broken files go into the error list.
func (s *Store) List() ([]*task.Task, []error) {
	refs, err := s.taskFiles()
	if err != nil {
		return nil, []error{err}
	}
	tasks := make([]*task.Task, len(refs))
	errs := make([]error, len(refs))
	// Files are split into contiguous chunks, one per goroutine: per-item distribution
	// through a channel over 1000 files eats the gain with thread wakeups.
	workers := min(listWorkers, runtime.NumCPU(), max(len(refs), 1))
	chunk := (len(refs) + workers - 1) / workers
	var wg sync.WaitGroup
	for from := 0; from < len(refs); from += chunk {
		wg.Add(1)
		go func(from, to int) {
			defer wg.Done()
			for i := from; i < to; i++ {
				tasks[i], errs[i] = readTask(refs[i].path)
			}
		}(from, min(from+chunk, len(refs)))
	}
	wg.Wait()
	var okTasks []*task.Task
	var bad []error
	for i := range refs {
		if errs[i] != nil {
			bad = append(bad, errs[i])
		} else {
			okTasks = append(okTasks, tasks[i])
		}
	}
	return okTasks, bad
}

func readTask(path string) (*task.Task, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is built by the core
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	t, err := task.Parse(path, data)
	if err != nil {
		return nil, err
	}
	if t.Rev == "" {
		t.Rev = blobID(data)
	}
	t.Path = path // absolute: the root is normalized in Open
	return t, nil
}

// stage writes data to the temp file .tix/tmp and flushes to disk.
func (s *Store) stage(data []byte, mode os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Join(s.root, tixDir, tmpDir), tmpPrefix+"*.tmp")
	if err != nil {
		return "", fmt.Errorf("temp file: %w", err)
	}
	name := f.Name()
	fail := func(err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(name)
		return "", fmt.Errorf("write %s: %w", name, err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("write %s: %w", name, err)
	}
	return name, nil
}

// publish renames temp into place; repeats on a temporary error.
func publish(tmp, dst string) error {
	var err error
	for i := range maxAttempts {
		if err = os.Rename(tmp, dst); err == nil { //nolint:gosec // paths are built by the core
			return nil
		}
		time.Sleep(time.Duration(i+1) * 5 * time.Millisecond)
	}
	_ = os.Remove(tmp) //nolint:gosec // the path is built by the core
	return fmt.Errorf("rename to %s: %w", dst, err)
}

// writeNew writes a file that does not exist yet (or is not readable as a task): temp and rename.
func (s *Store) writeNew(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := s.stage(data, mode)
	if err != nil {
		return err
	}
	return publish(tmp, path)
}

// replace replaces the file if it still equals what was read (step 4 of 2.1). false - the file
// changed since the read, a repeat is needed.
func (s *Store) replace(path string, old, data []byte) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	tmp, err := s.stage(data, info.Mode().Perm())
	if err != nil {
		return false, err
	}
	cur, err := os.ReadFile(path) //nolint:gosec // the path is built by the core
	if err != nil || !bytes.Equal(cur, old) {
		_ = os.Remove(tmp) //nolint:gosec // the path is built by the core
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("read %s: %w", path, err)
		}
		return false, nil
	}
	return true, publish(tmp, path)
}
