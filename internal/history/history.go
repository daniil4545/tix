// Package history собирает ленту карточки задачи: коммиты git и комментарии из файла.
package history

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"tracker/internal/gitx"
	"tracker/internal/task"
)

const (
	opComment  = "comment"
	opEdits    = "edits"
	opExternal = "external edit"
	// actorExternal - автор правок мимо ядра (трейлер Actor).
	actorExternal = "external"
)

// foldable - мелкие правки: от двух подряд одного Actor они сливаются в строку "N edits" (Р60).
// Переносы, заметки, взятие и комментарии остаются отдельными строками.
var foldable = map[string]bool{"set": true, "check": true, "edit": true, "release": true, opExternal: true}

// subjectRe - начало темы коммита ядра: "T-12: ".
var subjectRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+: `)

// Entry - строка ленты. Folded - свёрнутая серия правок ("N edits"); Op comment - из файла.
type Entry struct {
	At              time.Time
	Actor, Op, Text string
	Folded          []Entry
}

// Load собирает ленту задачи от старых записей к новым и сворачивает её (Fold). Коммиты git
// дают переносы, заметки и правки; тексты комментариев берутся из t.Comments. Коммит comment
// сам в ленту не идёт: на его месте встаёт комментарий из файла, иначе он задвоился бы.
func Load(r gitx.Repo, t *task.Task) ([]Entry, error) {
	commits, err := r.Log(t.ID)
	if err != nil {
		return nil, fmt.Errorf("history of %s: %w", t.ID, err)
	}
	left := append([]task.Comment(nil), t.Comments...)
	es := make([]Entry, 0, len(commits)+len(left))
	for i := len(commits) - 1; i >= 0; i-- { // Log отдаёт новые первыми
		e := commitEntry(commits[i])
		if e.Op != opComment {
			es = append(es, e)
			continue
		}
		// Маркер в файле хранит минуты, коммит - секунды: пара по автору и минуте.
		if c, ok := takeComment(&left, e); ok {
			es = append(es, Entry{At: e.At, Actor: c.Actor, Op: opComment, Text: c.Text})
		}
	}
	// Оставшиеся комментарии пришли без своего коммита comment: `done -m` пишет их коммитом
	// done. Они встают перед ним; без пары - по времени из файла.
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

// takeComment вынимает из left первый комментарий того же автора и минуты, что у коммита.
func takeComment(left *[]task.Comment, commit Entry) (task.Comment, bool) {
	for i, c := range *left {
		if c.Actor == commit.Actor && sameMinute(c.At, commit.At) {
			*left = append((*left)[:i], (*left)[i+1:]...)
			return c, true
		}
	}
	return task.Comment{}, false
}

// findDone ищет коммит done того же автора и минуты, что у комментария; нет - -1.
func findDone(es []Entry, c task.Comment) int {
	for i, e := range es {
		if e.Op == "done" && e.Actor == c.Actor && sameMinute(e.At, c.At) {
			return i
		}
	}
	return -1
}

// commitEntry переводит коммит в строку ленты. Тип - слово после "T-12: "; правка мимо ядра
// (Actor external) - external edit: в таком коммите несколько id, тема несёт только первый.
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

// Fold сворачивает серии мелких правок одного Actor от двух подряд в одну строку
// Op edits, Text "N edits", Folded - исходные записи. Порядок входа сохраняется.
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
