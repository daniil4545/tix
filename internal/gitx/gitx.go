// Package gitx - единственное место с os/exec: вызовы git.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	gitTimeout    = 30 * time.Second // зависший git не держит лок вечно
	xcrunTimeout  = 10 * time.Second
	cacheDirName  = "tracker"
	cacheFileName = "git-path"
)

// revRe - допустимый blob id: защита от опций вместо id в cat-file.
var revRe = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

// ErrBusy - в .git лежит свежий lock-файл: git другого процесса ещё работает.
var ErrBusy = errors.New("git is busy")

// staleLockAge - lock-файл старше этого возраста считается висящим (след убитого git).
const staleLockAge = 10 * time.Second

// Error - отказ git. Error() даёт короткую фразу без текста git: она идёт в ответ
// пользователю. Полный текст stderr - в Stderr и Detail, он идёт только в лог.
type Error struct {
	Op     string // подкоманда git
	Code   int    // код выхода, 0 - git не запустился
	Stderr string // вывод git или текст ошибки запуска
}

func (e *Error) Error() string { return "git " + e.Op + " failed" }

// Detail - полный текст отказа для лога.
func (e *Error) Detail() string {
	return fmt.Sprintf("git %s: exit %d: %s", e.Op, e.Code, strings.TrimSpace(e.Stderr))
}

// Detail возвращает текст ошибки для лога: с выводом git, если он в цепочке.
func Detail(err error) string {
	var ge *Error
	if errors.As(err, &ge) {
		return err.Error() + " [" + ge.Detail() + "]"
	}
	return err.Error()
}

// Repo - репозиторий задач и путь к бинарю git.
type Repo struct{ Dir, Bin string }

// Change - строка git status: путь и код состояния XY (два знака).
// Переименование из -z приходит двумя путями: целевой несёт код записи,
// исходный - код "D ". С --no-renames записи R и C не возникают.
type Change struct{ Path, Code string }

// result - итог запуска git: вывод и код выхода.
type result struct {
	out, errOut []byte
	code        int
}

// Resolve находит путь к git: override (TRACKER_GIT), кеш, xcrun на darwin.
// На darwin /usr/bin/git - заглушка xcode-select (+5,5 мс на вызов), поэтому
// путь к настоящему git ищется один раз и кешируется в os.UserCacheDir().
func Resolve(override string) (string, error) {
	if override != "" {
		p, err := exec.LookPath(override)
		if err != nil {
			return "", fmt.Errorf("git %q: %w", override, err)
		}
		return absIfPath(p), nil
	}
	if runtime.GOOS != "darwin" {
		return lookGit()
	}
	cache := cachePath()
	if p := readCache(cache); p != "" {
		return p, nil
	}
	p, err := findDarwinGit()
	if err != nil {
		return "", err
	}
	writeCache(cache, p)
	return p, nil
}

func lookGit() (string, error) {
	p, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git not found: %w", err)
	}
	return absIfPath(p), nil
}

func absIfPath(p string) string {
	if !strings.ContainsRune(p, filepath.Separator) {
		return p
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// findDarwinGit спрашивает xcrun; нет xcrun или он упал - git из PATH.
func findDarwinGit() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), xcrunTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "--find", "git").Output()
	if err == nil {
		p := strings.TrimSpace(string(out))
		if usable(p) {
			return p, nil
		}
	}
	return lookGit()
}

func cachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, cacheDirName, cacheFileName)
}

// readCache возвращает путь из кеша, если файл git ещё на месте (ENOENT - заново).
func readCache(cache string) string {
	if cache == "" {
		return ""
	}
	data, err := os.ReadFile(cache) //nolint:gosec // путь кеша строится из UserCacheDir
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(data))
	if !usable(p) {
		return ""
	}
	return p
}

func usable(p string) bool {
	if p == "" || !filepath.IsAbs(p) {
		return false
	}
	fi, err := os.Stat(p) //nolint:gosec // путь из кеша или xcrun, только проверка
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// writeCache пишет кеш через temp и rename; ошибка не критична: путь уже найден.
func writeCache(cache, p string) {
	if cache == "" {
		return
	}
	dir := filepath.Dir(cache)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, cacheFileName+".*")
	if err != nil {
		return
	}
	_, werr := tmp.WriteString(p + "\n")
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), cache) != nil {
		_ = os.Remove(tmp.Name())
	}
}

// env возвращает окружение git: без переменных, что перенаправляют репозиторий,
// и с потолком поиска .git на Dir (без него запись ушла бы в чужой репозиторий выше).
func (r Repo) env() []string {
	skip := []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_OBJECT_DIRECTORY=", "GIT_COMMON_DIR=", "GIT_CEILING_DIRECTORIES="}
	base := os.Environ()
	env := make([]string, 0, len(base)+2)
	for _, kv := range base {
		drop := false
		for _, p := range skip {
			if strings.HasPrefix(kv, p) {
				drop = true
				break
			}
		}
		if !drop {
			env = append(env, kv)
		}
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	if abs, err := filepath.Abs(r.Dir); err == nil {
		env = append(env, "GIT_CEILING_DIRECTORIES="+filepath.Dir(abs))
	}
	return env
}

// run запускает git с флагами 2.1; reader добавляет --no-optional-locks.
func (r Repo) run(stdin []byte, reader bool, args ...string) (result, error) {
	full := []string{
		"-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "user.name=tracker",
		"-c", "user.email=tracker@localhost",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
		"--literal-pathspecs",
	}
	if reader {
		full = append(full, "--no-optional-locks")
	}
	full = append(full, args...)
	bin := r.Bin
	if bin == "" {
		bin = "git"
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, full...) //nolint:gosec // бинарь git задаёт Resolve или TRACKER_GIT
	cmd.Dir = r.Dir
	cmd.Env = r.env()
	cmd.WaitDelay = 2 * time.Second
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	res := result{out: out.Bytes(), errOut: errOut.Bytes()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.code = ee.ExitCode()
		return res, nil
	}
	if err != nil {
		return res, &Error{Op: args[0], Stderr: err.Error()}
	}
	return res, nil
}

// must запускает git и считает любой ненулевой код ошибкой.
func (r Repo) must(stdin []byte, reader bool, args ...string) ([]byte, error) {
	res, err := r.run(stdin, reader, args...)
	if err != nil {
		return nil, err
	}
	if res.code != 0 {
		return nil, &Error{Op: args[0], Code: res.code, Stderr: string(res.errOut)}
	}
	return res.out, nil
}

// Init создаёт репозиторий (и папку, если её нет).
func (r Repo) Init() error {
	if err := os.MkdirAll(r.Dir, 0o750); err != nil {
		return &Error{Op: "init", Stderr: err.Error()}
	}
	_, err := r.must(nil, false, "init", "-q", "-b", "main", "--template=")
	return err
}

// Status читает изменения: --porcelain -z --untracked-files=all.
func (r Repo) Status() ([]Change, error) {
	out, err := r.must(nil, true, "status", "--porcelain", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

// parseStatus разбирает вывод -z: "XY путь\0"; у R и C следом идёт исходный путь.
func parseStatus(out []byte) []Change {
	var changes []Change
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		code, path := e[:2], e[3:]
		changes = append(changes, Change{Path: path, Code: code})
		if code[0] == 'R' || code[0] == 'C' || code[1] == 'R' || code[1] == 'C' {
			i++
			if i < len(parts) && parts[i] != "" {
				changes = append(changes, Change{Path: parts[i], Code: "D "})
			}
		}
	}
	return changes
}

// Commit делает add -A -- paths и commit --only -- paths, возвращает хеш.
// Пустой список путей - commit --allow-empty --only (индекс не забирается).
// Путь, которого нет ни в дереве, ни в индексе, - ошибка.
// Быстрый путь: commit --only сам индексирует правку отслеживаемого файла, без
// отдельного add (-5 мс). Новый файл git так не берёт: тогда add -A и повтор.
func (r Repo) Commit(paths []string, subject, actor string) (string, error) {
	args := []string{"commit", "-q", "--only", "-m", subject}
	if actor != "" {
		args = append(args, "-m", "Actor: "+actor)
	}
	if len(paths) == 0 {
		args = append(args, "--allow-empty")
	} else {
		args = append(args, "--")
		args = append(args, paths...)
	}
	res, err := r.run(nil, false, args...)
	if err != nil {
		return "", err
	}
	if res.code == 1 && len(paths) > 0 {
		if err := r.stage(paths); err != nil {
			return "", err
		}
		if res, err = r.run(nil, false, args...); err != nil {
			return "", err
		}
	}
	if res.code != 0 {
		return "", &Error{Op: "commit", Code: res.code, Stderr: string(res.errOut)}
	}
	return r.head()
}

// head читает хеш HEAD из файлов refs (loose ref); иначе rev-parse.
func (r Repo) head() (string, error) {
	if id := r.looseHead(); id != "" {
		return id, nil
	}
	out, err := r.must(nil, true, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (r Repo) looseHead() string {
	gitDir := filepath.Join(r.Dir, ".git")
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD")) //nolint:gosec // путь внутри репозитория
	if err != nil {
		return ""
	}
	ref, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "ref: ")
	if !ok || strings.Contains(ref, "..") {
		return ""
	}
	h, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref))) //nolint:gosec // см. выше
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(h))
	if len(id) != 40 || !revRe.MatchString(id) {
		return ""
	}
	return id
}

// stage индексирует пути операции. Есть в дереве - add -A; нет (удалён) -
// rm --cached: add -A на путь, которого нет ни в дереве, ни в индексе, падает.
func (r Repo) stage(paths []string) error {
	var present, gone []string
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(r.Dir, p)
		}
		if _, err := os.Lstat(abs); err != nil {
			gone = append(gone, p)
		} else {
			present = append(present, p)
		}
	}
	if len(present) > 0 {
		if _, err := r.must(nil, false, append([]string{"add", "-A", "--"}, present...)...); err != nil {
			return err
		}
	}
	if len(gone) > 0 {
		if _, err := r.must(nil, false, append([]string{"rm", "-q", "--cached", "--ignore-unmatch", "--"}, gone...)...); err != nil {
			return err
		}
	}
	return nil
}

// HashObject пишет blob в git (hash-object -w) и возвращает его id.
func (r Repo) HashObject(data []byte) (string, error) {
	out, err := r.must(data, false, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Blob читает blob по id.
func (r Repo) Blob(rev string) ([]byte, error) {
	if !revRe.MatchString(rev) {
		return nil, fmt.Errorf("git cat-file: bad id %q", rev)
	}
	return r.must(nil, true, "cat-file", "blob", rev)
}

// MergeFile сливает три версии через git merge-file. conflict: в out остались маркеры.
func (r Repo) MergeFile(ours, base, theirs []byte) (out []byte, conflict bool, err error) {
	dir, err := os.MkdirTemp("", "tkmerge-")
	if err != nil {
		return nil, false, &Error{Op: "merge-file", Stderr: err.Error()}
	}
	defer func() { _ = os.RemoveAll(dir) }()
	names := []string{"ours", "base", "theirs"}
	for i, data := range [][]byte{ours, base, theirs} {
		if err := os.WriteFile(filepath.Join(dir, names[i]), data, 0o600); err != nil {
			return nil, false, &Error{Op: "merge-file", Stderr: err.Error()}
		}
	}
	res, err := r.run(nil, true, "merge-file", "-p",
		"-L", "ours", "-L", "base", "-L", "theirs",
		filepath.Join(dir, "ours"), filepath.Join(dir, "base"), filepath.Join(dir, "theirs"))
	if err != nil {
		return nil, false, err
	}
	switch {
	case res.code == 0:
		return res.out, false, nil
	case res.code > 0 && res.code < 128:
		return res.out, true, nil
	default:
		return nil, false, &Error{Op: "merge-file", Code: res.code, Stderr: string(res.errOut)}
	}
}

// DropGitLocks удаляет висящие lock-файлы git после убитого процесса: .git/*.lock
// (index.lock, HEAD.lock, packed-refs.lock) и .git/refs/**/*.lock. Висящий - с mtime
// старше staleLockAge. Свежий lock может держать живой git (Obsidian Git, терминал):
// его не трогаем, а при нём ничего не удаляем и отвечаем ErrBusy.
// Вызывать под нашим локом. Возвращает пути (относительно .git) удалённых файлов.
func (r Repo) DropGitLocks() ([]string, error) {
	gitDir := filepath.Join(r.Dir, ".git")
	top, err := filepath.Glob(filepath.Join(gitDir, "*.lock"))
	if err != nil {
		return nil, fmt.Errorf("glob git locks: %w", err)
	}
	locks := top
	err = filepath.WalkDir(filepath.Join(gitDir, "refs"), func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil && errors.Is(err, os.ErrNotExist):
			return nil
		case err != nil:
			return err
		case !d.IsDir() && strings.HasSuffix(p, ".lock"):
			locks = append(locks, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk refs: %w", err)
	}
	var stale []string
	for _, p := range locks {
		info, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue // git снял lock сам
		}
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}
		if time.Since(info.ModTime()) < staleLockAge {
			rel, _ := filepath.Rel(gitDir, p)
			return nil, fmt.Errorf("%w: %s is fresh", ErrBusy, filepath.ToSlash(rel))
		}
		stale = append(stale, p)
	}
	var dropped []string
	for _, p := range stale {
		err := os.Remove(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return dropped, fmt.Errorf("remove %s: %w", p, err)
		}
		rel, _ := filepath.Rel(gitDir, p)
		dropped = append(dropped, filepath.ToSlash(rel))
	}
	return dropped, nil
}
