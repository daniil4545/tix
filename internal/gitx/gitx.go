// Package gitx - the only place with os/exec: git calls.
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
	gitTimeout    = 30 * time.Second // a hung git does not hold the lock forever
	xcrunTimeout  = 10 * time.Second
	cacheDirName  = "tix"
	cacheFileName = "git-path"
)

// revRe - an allowed blob id: protection against options instead of an id in cat-file.
var revRe = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

// ErrBusy - a fresh lock file lies in .git: git of another process is still working.
var ErrBusy = errors.New("git is busy")

// staleLockAge - a lock file older than this age counts as dangling (a trace of a killed git).
const staleLockAge = 10 * time.Second

// Error - a git refusal. Error() gives a short phrase without git text: it goes into the answer
// to the user. The full stderr text is in Stderr and Detail, it goes to the log only.
type Error struct {
	Op     string // git subcommand
	Code   int    // exit code, 0 - git did not start
	Stderr string // git output or the start error text
}

func (e *Error) Error() string { return "git " + e.Op + " failed" }

// Detail - the full refusal text for the log.
func (e *Error) Detail() string {
	return fmt.Sprintf("git %s: exit %d: %s", e.Op, e.Code, strings.TrimSpace(e.Stderr))
}

// Detail returns the error text for the log: with git output, if it is in the chain.
func Detail(err error) string {
	var ge *Error
	if errors.As(err, &ge) {
		return err.Error() + " [" + ge.Detail() + "]"
	}
	return err.Error()
}

// Repo - the task repository and the path to the git binary.
type Repo struct{ Dir, Bin string }

// Change - a git status line: the path and the XY state code (two characters).
// A rename from -z comes as two paths: the target carries the write code,
// the source carries the code "D ". With --no-renames, R and C entries do not appear.
type Change struct{ Path, Code string }

// result - the outcome of a git run: output and exit code.
type result struct {
	out, errOut []byte
	code        int
}

// Resolve finds the path to git: override (TIX_GIT), cache, xcrun on darwin.
// On darwin /usr/bin/git is an xcode-select stub (+5.5 ms per call), so
// the path to the real git is looked up once and cached in os.UserCacheDir().
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

// findDarwinGit asks xcrun; no xcrun or it failed - git from PATH.
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

// readCache returns the path from the cache if the git file is still there (ENOENT - look up again).
func readCache(cache string) string {
	if cache == "" {
		return ""
	}
	data, err := os.ReadFile(cache) //nolint:gosec // the cache path is built from UserCacheDir
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
	fi, err := os.Stat(p) //nolint:gosec // path from the cache or xcrun, check only
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// writeCache writes the cache through temp and rename; an error is not critical: the path is already found.
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

// env returns the git environment: without the variables that redirect the repository,
// and with a ceiling for the .git search at Dir (without it a write would go to a foreign repository above).
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

// command builds a git call with the flags of 2.1; reader adds --no-optional-locks.
func (r Repo) command(ctx context.Context, reader bool, args ...string) *exec.Cmd {
	full := []string{
		"-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "user.name=tix",
		"-c", "user.email=tix@localhost",
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
	cmd := exec.CommandContext(ctx, bin, full...) //nolint:gosec // the git binary comes from Resolve or TIX_GIT
	cmd.Dir = r.Dir
	cmd.Env = r.env()
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// run runs git and waits up to gitTimeout.
func (r Repo) run(stdin []byte, reader bool, args ...string) (result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := r.command(ctx, reader, args...)
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

// must runs git and treats any non-zero code as an error.
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

// Init creates a repository (and the folder, if missing).
func (r Repo) Init() error {
	if err := os.MkdirAll(r.Dir, 0o750); err != nil {
		return &Error{Op: "init", Stderr: err.Error()}
	}
	_, err := r.must(nil, false, "init", "-q", "-b", "main", "--template=")
	return err
}

// Status reads changes: --porcelain -z --untracked-files=all.
func (r Repo) Status() ([]Change, error) {
	out, err := r.must(nil, true, "status", "--porcelain", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

// parseStatus parses the -z output: "XY path\0"; R and C are followed by the source path.
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

// Trailer - a "Key: Value" line at the end of a commit message (git commit --trailer).
// An empty value - the trailer is not written.
type Trailer struct{ Key, Value string }

// trailerKeyRe - a trailer key: a token without spaces and colons.
var trailerKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

// messageArgs builds the message arguments: subject, then trailers. Actor goes first.
func messageArgs(subject, actor string, trailers []Trailer) ([]string, error) {
	args := []string{"-m", subject}
	all := trailers
	if actor != "" {
		all = append([]Trailer{{Key: "Actor", Value: actor}}, trailers...)
	}
	for _, t := range all {
		if t.Value == "" {
			continue
		}
		if !trailerKeyRe.MatchString(t.Key) || strings.ContainsAny(t.Value, "\n\r\x00") {
			return nil, fmt.Errorf("git commit: bad trailer %q", t.Key)
		}
		args = append(args, "--trailer", t.Key+": "+t.Value)
	}
	return args, nil
}

// Commit does add -A -- paths and commit --only -- paths, returns the hash.
// An empty path list - commit --allow-empty --only (the index is not taken).
// A path that is neither in the tree nor in the index is an error.
// Fast path: commit --only indexes an edit of a tracked file itself, without
// a separate add (-5 ms). git does not take a new file this way: then add -A and retry.
func (r Repo) Commit(paths []string, subject, actor string, trailers ...Trailer) (string, error) {
	msg, err := messageArgs(subject, actor, trailers)
	if err != nil {
		return "", err
	}
	args := append([]string{"commit", "-q", "--only"}, msg...)
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

// File - an operation path and the bytes the core wrote. Data nil - the path is deleted.
type File struct {
	Path string
	Data []byte
}

// zeroID - the id for a delete line in update-index --index-info: only the format matters.
const zeroID = "0000000000000000000000000000000000000000"

// CommitFiles commits exactly the core bytes: hash-object -w per file, update-index
// --index-info, commit of the index. A file from the tree does not go into the commit: a foreign write after
// rename stays a tree edit and goes with the next external edit commit.
// An empty list - an empty commit, like Commit(nil, ...).
func (r Repo) CommitFiles(files []File, subject, actor string, trailers ...Trailer) (string, error) {
	if len(files) == 0 {
		return r.Commit(nil, subject, actor, trailers...)
	}
	msg, err := messageArgs(subject, actor, trailers)
	if err != nil {
		return "", err
	}
	var info bytes.Buffer
	for _, f := range files {
		if f.Data == nil {
			fmt.Fprintf(&info, "0 %s\t%s\x00", zeroID, f.Path)
			continue
		}
		id, err := r.HashObject(f.Data)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&info, "%s %s\t%s\x00", r.fileMode(f.Path), id, f.Path)
	}
	if _, err := r.must(info.Bytes(), false, "update-index", "-z", "--index-info"); err != nil {
		return "", err
	}
	if _, err := r.must(nil, false, append([]string{"commit", "-q"}, msg...)...); err != nil {
		return "", err
	}
	return r.head()
}

// fileMode - the index entry mode by the execute bit of the file in the tree.
func (r Repo) fileMode(path string) string {
	if fi, err := os.Lstat(filepath.Join(r.Dir, filepath.FromSlash(path))); err == nil && fi.Mode()&0o111 != 0 {
		return "100755"
	}
	return "100644"
}

// head reads the HEAD hash from the refs files (loose ref); otherwise rev-parse.
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
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD")) //nolint:gosec // path inside the repository
	if err != nil {
		return ""
	}
	ref, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "ref: ")
	if !ok || strings.Contains(ref, "..") {
		return ""
	}
	h, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref))) //nolint:gosec // see above
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(h))
	if len(id) != 40 || !revRe.MatchString(id) {
		return ""
	}
	return id
}

// stage indexes the operation paths. In the tree - add -A; not there (deleted) -
// rm --cached: add -A on a path that is neither in the tree nor in the index fails.
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

// HashObject writes a blob to git (hash-object -w) and returns its id.
func (r Repo) HashObject(data []byte) (string, error) {
	out, err := r.must(data, false, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Blob reads a blob by id.
func (r Repo) Blob(rev string) ([]byte, error) {
	if !revRe.MatchString(rev) {
		return nil, fmt.Errorf("git cat-file: bad id %q", rev)
	}
	return r.must(nil, true, "cat-file", "blob", rev)
}

// MergeFile merges three versions through git merge-file. conflict: markers remain in out.
func (r Repo) MergeFile(ours, base, theirs []byte) (out []byte, conflict bool, err error) {
	dir, err := os.MkdirTemp("", "tixmerge-")
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

// DropGitLocks removes dangling git lock files after a killed process: .git/*.lock
// (index.lock, HEAD.lock, packed-refs.lock) and .git/refs/**/*.lock. Dangling - with mtime
// older than staleLockAge. A fresh lock may be held by a live git (Obsidian Git, a terminal):
// we do not touch it, and with it present we delete nothing and answer ErrBusy.
// Call under our lock. Returns the paths (relative to .git) of the removed files.
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
			continue // git released the lock itself
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
