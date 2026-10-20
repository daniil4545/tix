package gitx

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// logLimit - потолок числа коммитов одной задачи: без -n git читает всю историю,
// на 100 тыс. коммитов это секунды (docs/api/git.md, раздел 5).
const logLimit = 500

// logRecordFields - число полей записи log: хеш, время автора, тема, значение трейлера Actor.
const logRecordFields = 4

// logIDRe - id задачи для --grep: без метасимволов регулярного выражения.
var logIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+$`)

// Commit - запись истории одной задачи: хеш, время автора, тема и значение трейлера Actor.
type Commit struct {
	Hash    string
	At      time.Time
	Subject string
	Actor   string
}

// Log возвращает до logLimit коммитов задачи id от новых к старым; нет коммитов - пусто.
// Один процесс git: выбор по строке "<id>: " в начале строки сообщения (коммит external edit
// несёт несколько id), трейлер Actor читает сам git.
func (r Repo) Log(id string) ([]Commit, error) {
	if !logIDRe.MatchString(id) {
		return nil, fmt.Errorf("git log: bad id %q", id)
	}
	res, err := r.run(nil, true, "log", "-n", fmt.Sprint(logLimit), "-z",
		"--grep=^"+id+":",
		"--format=%H%x1f%aI%x1f%s%x1f%(trailers:key=Actor,valueonly)")
	if err != nil {
		return nil, err
	}
	if res.code != 0 {
		// Пустой репозиторий: у ветки нет коммитов, это не отказ.
		if strings.Contains(string(res.errOut), "does not have any commits") {
			return nil, nil
		}
		return nil, &Error{Op: "log", Code: res.code, Stderr: string(res.errOut)}
	}
	return parseLog(res.out)
}

// parseLog разбирает вывод -z: записи через NUL, поля через 0x1f.
func parseLog(out []byte) ([]Commit, error) {
	var commits []Commit
	for _, rec := range strings.Split(string(out), "\x00") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", logRecordFields)
		if len(f) != logRecordFields {
			return nil, &Error{Op: "log", Stderr: "unexpected record " + fmt.Sprintf("%q", rec)}
		}
		at, err := time.Parse(time.RFC3339, f[1])
		if err != nil {
			return nil, &Error{Op: "log", Stderr: err.Error()}
		}
		commits = append(commits, Commit{Hash: f[0], At: at.Local(), Subject: f[2], Actor: firstLine(f[3])})
	}
	return commits, nil
}

// firstLine берёт первое значение трейлера: git печатает по строке на значение.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}
