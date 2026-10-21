package core

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // git blob id, не защита
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

// taskRef - файл задачи в корне базы.
type taskRef struct {
	id   string
	num  int
	path string
}

// blobID считает git blob id (sha1 от "blob <длина>\0" и содержимого).
func blobID(data []byte) string {
	h := sha1.New() //nolint:gosec // git blob id, не защита
	_, _ = fmt.Fprintf(h, "blob %d\x00", len(data))
	_, _ = h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// IDNum - номер задачи из id вида T-12.
func IDNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "T-"))
	return n
}

// taskFiles перечисляет T-*.md в корне, по возрастанию номера. Нет папки - пусто.
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

// findPath ищет файл задачи по id.
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

// nextNum: max(next_id, максимальный номер файла + 1). Битый next_id - ошибка, файл цел.
func (s *Store) nextNum(refs []taskRef) (int, error) {
	num := 1
	if n := len(refs); n > 0 {
		num = refs[n-1].num + 1
	}
	path := filepath.Join(s.root, tixDir, nextIDFile)
	data, err := os.ReadFile(path) //nolint:gosec // путь строит ядро
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

// listWorkers - число горутин разбора в List (замер этапа 0: 4 горутины 11 мс, последовательно 24 мс).
const listWorkers = 4

// List читает все задачи без git. Битые файлы - в списке ошибок.
func (s *Store) List() ([]*task.Task, []error) {
	refs, err := s.taskFiles()
	if err != nil {
		return nil, []error{err}
	}
	tasks := make([]*task.Task, len(refs))
	errs := make([]error, len(refs))
	// Файлы делятся на непрерывные куски по одному на горутину: поштучная раздача
	// через канал на 1000 файлов съедает выигрыш пробуждениями потоков.
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
	data, err := os.ReadFile(path) //nolint:gosec // путь строит ядро
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
	t.Path = path // абсолютный: корень приведён в Open
	return t, nil
}

// stage пишет данные во временный файл .tix/tmp и сбрасывает на диск.
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

// publish делает rename temp на место; повтор при временной ошибке.
func publish(tmp, dst string) error {
	var err error
	for i := range maxAttempts {
		if err = os.Rename(tmp, dst); err == nil { //nolint:gosec // пути строит ядро
			return nil
		}
		time.Sleep(time.Duration(i+1) * 5 * time.Millisecond)
	}
	_ = os.Remove(tmp) //nolint:gosec // путь строит ядро
	return fmt.Errorf("rename to %s: %w", dst, err)
}

// writeNew пишет файл, которого ещё нет (или не читается задачей): temp и rename.
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

// replace заменяет файл, если он ещё равен прочитанному (шаг 4 из 2.1). false - файл
// изменился с чтения, нужен повтор.
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
	cur, err := os.ReadFile(path) //nolint:gosec // путь строит ядро
	if err != nil || !bytes.Equal(cur, old) {
		_ = os.Remove(tmp) //nolint:gosec // путь строит ядро
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("read %s: %w", path, err)
		}
		return false, nil
	}
	return true, publish(tmp, path)
}
