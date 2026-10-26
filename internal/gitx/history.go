package gitx

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// logLimit - the ceiling on the number of commits of one task: without -n git reads the whole history,
// at 100 thousand commits that takes seconds (docs/api/git.md, section 5).
const logLimit = 500

// logRecordFields - the number of log record fields: hash, author time, subject, the value of the Actor trailer.
const logRecordFields = 4

// noteLimit - how many latest commits Notes scans: the board pulse reads them with one git call.
const noteLimit = 300

// logIDRe - a task id for --grep: without regular expression metacharacters.
var logIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+$`)

// noteSubjectRe - the subject of a note commit: "<id>: note <text>".
var noteSubjectRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*-[0-9]+): note (.+)$`)

// Note - a note of a task: author time, text and, for TaskNotes, the Actor trailer.
type Note struct {
	At    time.Time
	Text  string
	Actor string
}

// Commit - a history record of one task: hash, author time, subject and the value of the Actor trailer.
type Commit struct {
	Hash    string
	At      time.Time
	Subject string
	Actor   string
}

// Log returns up to logLimit commits of task id from new to old; no commits - empty.
// One git process: selection by the line "<id>: " at the start of a message line (an external edit commit
// carries several ids), the Actor trailer is read by git itself.
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
		// An empty repository: the branch has no commits, this is not a failure.
		if strings.Contains(string(res.errOut), "does not have any commits") {
			return nil, nil
		}
		return nil, &Error{Op: "log", Code: res.code, Stderr: string(res.errOut)}
	}
	return parseLog(res.out)
}

// parseLog parses the -z output: records separated by NUL, fields by 0x1f.
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

// firstLine takes the first trailer value: git prints one line per value.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}

// TaskNotes returns up to logLimit newest notes of task id, from old to new; none - empty.
// The limit counts notes only: other commits of the task do not push old notes out.
// A non-zero since stops the history walk at that time (--since); zero reads the whole history.
func (r Repo) TaskNotes(id string, since time.Time) ([]Note, error) {
	if !logIDRe.MatchString(id) {
		return nil, fmt.Errorf("git log: bad id %q", id)
	}
	prefix := id + ": note "
	args := []string{"log", "-n", fmt.Sprint(logLimit), "-z", "--fixed-strings", "--grep=" + prefix,
		"--format=%H%x1f%aI%x1f%s%x1f%(trailers:key=Actor,valueonly)"}
	if !since.IsZero() {
		args = append(args, "--since="+since.Format(time.RFC3339))
	}
	res, err := r.run(nil, true, args...)
	if err != nil {
		return nil, err
	}
	if res.code != 0 {
		if strings.Contains(string(res.errOut), "does not have any commits") {
			return nil, nil
		}
		return nil, &Error{Op: "log", Code: res.code, Stderr: string(res.errOut)}
	}
	commits, err := parseLog(res.out)
	if err != nil {
		return nil, err
	}
	var notes []Note
	for i := len(commits) - 1; i >= 0; i-- { // git prints the new ones first
		// The match may come from the message body (an external edit commit): only the subject counts.
		if text, ok := strings.CutPrefix(commits[i].Subject, prefix); ok {
			notes = append(notes, Note{At: commits[i].At, Actor: commits[i].Actor, Text: text})
		}
	}
	return notes, nil
}

// Notes returns, by task id, the newest note among the latest noteLimit note commits.
// One git process for all tasks; no commits - an empty result.
func (r Repo) Notes() (map[string]Note, error) {
	res, err := r.run(nil, true, "log", "-n", fmt.Sprint(noteLimit), "-z", "--extended-regexp",
		"--grep=^[A-Za-z][A-Za-z0-9]*-[0-9]+: note ",
		"--format=%aI%x1f%s")
	if err != nil {
		return nil, err
	}
	if res.code != 0 {
		if strings.Contains(string(res.errOut), "does not have any commits") {
			return map[string]Note{}, nil
		}
		return nil, &Error{Op: "log", Code: res.code, Stderr: string(res.errOut)}
	}
	return parseNotes(res.out)
}

// parseNotes parses the -z output from new to old: a task keeps the first note it meets.
// A commit selected by another message line (an external edit) does not match by subject and is skipped.
func parseNotes(out []byte) (map[string]Note, error) {
	notes := map[string]Note{}
	for _, rec := range strings.Split(string(out), "\x00") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		stamp, subject, ok := strings.Cut(rec, "\x1f")
		if !ok {
			return nil, &Error{Op: "log", Stderr: "unexpected record " + fmt.Sprintf("%q", rec)}
		}
		at, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			return nil, &Error{Op: "log", Stderr: err.Error()}
		}
		m := noteSubjectRe.FindStringSubmatch(subject)
		if m == nil {
			continue
		}
		text := strings.TrimSpace(m[2])
		if _, seen := notes[m[1]]; seen || text == "" {
			continue
		}
		notes[m[1]] = Note{At: at.Local(), Text: text}
	}
	return notes, nil
}
