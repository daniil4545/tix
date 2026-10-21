package actor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lockWait - how long a hook waits for the marks lock: a parallel hook of the same session holds it for milliseconds.
const lockWait = 3 * time.Second

// Marks - delivery marks: a task id and the number of comments the agent has already seen.
type Marks map[string]int

// InboxDir - the directory of delivery marks: machine data, not the store. TIX_INBOX_DIR overrides
// the path (tests), otherwise os.UserCacheDir()/tix/inbox. Empty if the cache directory is unknown.
func InboxDir(getenv func(string) string) string {
	if dir := getenv("TIX_INBOX_DIR"); dir != "" {
		return dir
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cache, "tix", "inbox")
}

// HookActor - the agent identity in a hook: the environment (Resolve), otherwise the hook input session_id
// with the agent prefix. Codex is told apart by turn_id in the input: CODEX_* variable names are not enough for that.
// Empty if no identity is found or the name fails Valid.
func HookActor(getenv func(string) string, sessionID string, codex bool) string {
	name, known := Resolve("", getenv)
	if !known {
		if sessionID == "" {
			return ""
		}
		kind := "claude"
		if codex {
			kind = "codex"
		}
		name = kind + ":" + sessionID
	}
	if Valid(name) != nil {
		return ""
	}
	return name
}

// ReadMarks reads the agent marks from file name in directory dir. No file or unreadable - ok=false:
// there are no marks, they are set again.
func ReadMarks(dir, name string) (marks Marks, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // dir comes from the user cache, name passed FocusFile
	if err != nil {
		return Marks{}, false
	}
	if json.Unmarshal(data, &marks) != nil || marks == nil {
		return Marks{}, false
	}
	return marks, true
}

// WriteMarks writes marks through a temporary file and rename: a reader never sees a half-written file.
func WriteMarks(dir, name string, marks Marks) error {
	data, err := json.Marshal(marks)
	if err != nil {
		return fmt.Errorf("inbox marks: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("inbox dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("inbox temp: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("inbox write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("inbox close: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("inbox rename: %w", err)
	}
	return nil
}

// LockMarks takes the agent marks lock (flock on .<name>.lock in dir) and returns the release function.
// The lock covers reading, computing and writing marks: two parallel session hooks do not deliver a comment twice.
// It is released when the process dies, too.
func LockMarks(dir, name string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("inbox dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "."+name+".lock"), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // dir comes from the user cache, name passed FocusFile
	if err != nil {
		return nil, fmt.Errorf("inbox lock: %w", err)
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // a file descriptor fits in int
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("inbox lock: %w", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
