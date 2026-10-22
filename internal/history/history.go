// Package history builds the task card timeline: git commits and comments from the file.
package history

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/task"
)

const (
	opComment  = "comment"
	opEdits    = "edits"
	opExternal = "external edit"
	// actorExternal - the author of external edits (the Actor trailer).
	actorExternal = "external"
)

// foldable - minor edits: two or more in a row by one Actor merge into an "N edits" line (D60).
// Moves, notes, takes, releases and comments stay separate lines: a release can move the task.
var foldable = map[string]bool{"set": true, "check": true, "edit": true, opExternal: true}

// subjectRe - the start of a core commit subject: "T-12: ".
var subjectRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+: `)

// Entry - a timeline line. Folded - a folded series of edits ("N edits"); Op comment - from the file.
type Entry struct {
	At              time.Time
	Actor, Op, Text string
	Folded          []Entry
}

// Load builds the task timeline from old records to new and folds it (Fold). Git commits
// give moves, notes and edits; comment texts come from t.Comments. A comment commit
// does not go into the timeline itself: the comment from the file takes its place, otherwise it would double.
func Load(r gitx.Repo, t *task.Task) ([]Entry, error) {
	commits, err := r.Log(t.ID)
	if err != nil {
		return nil, fmt.Errorf("history of %s: %w", t.ID, err)
	}
	left := append([]task.Comment(nil), t.Comments...)
	es := make([]Entry, 0, len(commits)+len(left))
	for i := len(commits) - 1; i >= 0; i-- { // Log returns the new ones first
		e := commitEntry(commits[i])
		if e.Op != opComment {
			es = append(es, e)
			continue
		}
		// The marker in the file keeps minutes, the commit keeps seconds: a pair by author and minute.
		if c, ok := takeComment(&left, e); ok {
			es = append(es, Entry{At: e.At, Actor: c.Actor, Op: opComment, Text: c.Text})
		}
	}
	// The remaining comments came without their own comment commit: `done -m` writes them in the done
	// commit. They go before it; without a pair - by the time from the file.
	for _, c := range left {
		e := Entry{At: c.At, Actor: c.Actor, Op: opComment, Text: c.Text}
		if i := findDone(es, c); i >= 0 {
			e.At = es[i].At
			es = append(es[:i+1], es[i:]...)
			es[i] = e
			continue
		}
		es = append(es, e)
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].At.Before(es[j].At) })
	return Fold(es), nil
}

func sameMinute(a, b time.Time) bool { return a.Truncate(time.Minute).Equal(b.Truncate(time.Minute)) }

// takeComment pulls out of left the first comment with the same author and minute as the commit.
func takeComment(left *[]task.Comment, commit Entry) (task.Comment, bool) {
	for i, c := range *left {
		if c.Actor == commit.Actor && sameMinute(c.At, commit.At) {
			*left = append((*left)[:i], (*left)[i+1:]...)
			return c, true
		}
	}
	return task.Comment{}, false
}

// findDone looks for a done commit with the same author and minute as the comment; none - -1.
func findDone(es []Entry, c task.Comment) int {
	for i, e := range es {
		if e.Op == "done" && e.Actor == c.Actor && sameMinute(e.At, c.At) {
			return i
		}
	}
	return -1
}

// commitEntry turns a commit into a timeline line. The kind is the word after "T-12: "; an edit bypassing the core
// (Actor external) is an external edit: such a commit has several ids, the subject carries only the first.
func commitEntry(c gitx.Commit) Entry {
	e := Entry{At: c.At, Actor: c.Actor}
	rest := subjectRe.ReplaceAllString(c.Subject, "")
	if c.Actor == actorExternal || strings.HasPrefix(rest, opExternal) {
		e.Op = opExternal
		return e
	}
	e.Op, e.Text, _ = strings.Cut(rest, " ")
	return e
}

// Fold folds series of minor edits by one Actor, two or more in a row, into one line
// Op edits, Text "N edits", Folded - the source records. The input order is kept.
func Fold(es []Entry) []Entry {
	out := make([]Entry, 0, len(es))
	for i := 0; i < len(es); {
		j := i + 1
		if foldable[es[i].Op] {
			for j < len(es) && foldable[es[j].Op] && es[j].Actor == es[i].Actor {
				j++
			}
		}
		if j-i < 2 {
			out = append(out, es[i])
		} else {
			out = append(out, Entry{
				At: es[j-1].At, Actor: es[i].Actor, Op: opEdits,
				Text: fmt.Sprintf("%d edits", j-i), Folded: append([]Entry(nil), es[i:j]...),
			})
		}
		i = j
	}
	return out
}
