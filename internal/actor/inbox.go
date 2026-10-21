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

// lockWait - сколько хук ждёт замок отметок: параллельный хук той же сессии держит его миллисекунды.
const lockWait = 3 * time.Second

// Marks - отметки доставки: id задачи и число комментариев, уже учтённых агентом.
type Marks map[string]int

// InboxDir - каталог отметок доставки: данные машины, не базы. TRACKER_INBOX_DIR перекрывает
// путь (тесты), иначе os.UserCacheDir()/tk/inbox. Пусто, если каталог кэша неизвестен.
func InboxDir(getenv func(string) string) string {
	if dir := getenv("TRACKER_INBOX_DIR"); dir != "" {
		return dir
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cache, "tk", "inbox")
}

// HookActor - личность агента в хуке: окружение (Resolve), иначе session_id входа хука
// с префиксом агента. Codex отличает turn_id во входе: имён переменных CODEX_* для этого мало.
// Пусто, если личность не найдена или имя не проходит Valid.
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

// ReadMarks читает отметки агента из файла name каталога dir. Нет файла или он нечитаем - ok=false:
// отметок нет, их ставят заново.
func ReadMarks(dir, name string) (marks Marks, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // dir из кэша пользователя, name прошёл FocusFile
	if err != nil {
		return Marks{}, false
	}
	if json.Unmarshal(data, &marks) != nil || marks == nil {
		return Marks{}, false
	}
	return marks, true
}

// WriteMarks пишет отметки через временный файл и rename: читатель не видит полузаписанный файл.
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

// LockMarks берёт замок отметок агента (flock на .<name>.lock в dir) и отдаёт функцию снятия.
// Замок держит чтение, расчёт и запись отметок: два параллельных хука сессии не доставят комментарий дважды.
// Снимается и при смерти процесса.
func LockMarks(dir, name string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("inbox dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "."+name+".lock"), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // dir из кэша пользователя, name прошёл FocusFile
	if err != nil {
		return nil, fmt.Errorf("inbox lock: %w", err)
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // дескриптор файла помещается в int
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
